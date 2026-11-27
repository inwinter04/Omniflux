package dataplane

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/fx"
)

type quotaUsage30d struct {
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	Currency     string  `json:"currency"`
}

type quotaItemResponse struct {
	LabelKey      string   `json:"label_key"`
	Kind          string   `json:"kind"`
	PlanName      string   `json:"plan_name"`
	Remaining     *float64 `json:"remaining"`
	Total         *float64 `json:"total"`
	Used          *float64 `json:"used"`
	Unit          string   `json:"unit"`
	NextResetTime *int64   `json:"next_reset_time,omitempty"`
}

type clientQuotaResponse struct {
	Valid     bool                `json:"valid"`
	Source    string              `json:"source"` // budget | upstream_balance | usage
	PlanName  string              `json:"plan_name"`
	Unlimited bool                `json:"unlimited"`
	Remaining *float64            `json:"remaining"`
	Total     *float64            `json:"total"`
	Used      *float64            `json:"used"`
	Unit      string              `json:"unit"`
	ResetAt   *string             `json:"reset_at,omitempty"`
	Items     []quotaItemResponse `json:"items"`
	Usage30d  quotaUsage30d       `json:"usage_30d"`
}

func roundQuotaValue(v float64) float64 {
	return math.Round(v*1_000_000) / 1_000_000
}

func budgetScopePrecedence(scopeType string) int {
	switch scopeType {
	case "api_key":
		return 4
	case "application":
		return 3
	case "project":
		return 2
	case "organization":
		return 1
	default:
		return 0
	}
}

func buildBudgetQuotaResponse(windows []budgetScopeRow, usage quotaUsage30d) (clientQuotaResponse, bool) {
	if len(windows) == 0 {
		return clientQuotaResponse{}, false
	}

	type parsedWindow struct {
		row            budgetScopeRow
		limit          float64
		spent          float64
		remaining      float64
		remainingRatio float64
		precedence     int
	}

	parsed := make([]parsedWindow, 0, len(windows))
	for _, w := range windows {
		limit, errL := strconv.ParseFloat(strings.TrimSpace(w.LimitAmount), 64)
		spent, errS := strconv.ParseFloat(strings.TrimSpace(w.SpentAmount), 64)
		if errL != nil || errS != nil || limit <= 0 {
			continue
		}
		rem := math.Max(0, limit-spent)
		parsed = append(parsed, parsedWindow{
			row:            w,
			limit:          limit,
			spent:          spent,
			remaining:      rem,
			remainingRatio: rem / limit,
			precedence:     budgetScopePrecedence(w.ScopeType),
		})
	}
	if len(parsed) == 0 {
		return clientQuotaResponse{}, false
	}

	primary := parsed[0]
	for _, cur := range parsed[1:] {
		if cur.precedence > primary.precedence ||
			(cur.precedence == primary.precedence && cur.remainingRatio < primary.remainingRatio) {
			primary = cur
		}
	}

	items := make([]quotaItemResponse, 0, len(parsed))
	for _, item := range parsed {
		rem := roundQuotaValue(item.remaining)
		tot := roundQuotaValue(item.limit)
		used := roundQuotaValue(item.spent)
		unit := item.row.Currency
		if unit == "" {
			unit = "USD"
		}
		var resetMS *int64
		if !item.row.WindowEnd.IsZero() {
			ms := item.row.WindowEnd.UTC().UnixMilli()
			resetMS = &ms
		}
		items = append(items, quotaItemResponse{
			LabelKey:      item.row.ScopeType + "_budget",
			Kind:          "budget",
			PlanName:      item.row.ScopeType + "_budget",
			Remaining:     &rem,
			Total:         &tot,
			Used:          &used,
			Unit:          unit,
			NextResetTime: resetMS,
		})
	}

	rem := roundQuotaValue(primary.remaining)
	tot := roundQuotaValue(primary.limit)
	used := roundQuotaValue(primary.spent)
	unit := primary.row.Currency
	if unit == "" {
		unit = "USD"
	}
	var resetAt *string
	if !primary.row.WindowEnd.IsZero() {
		formatted := primary.row.WindowEnd.UTC().Format(time.RFC3339)
		resetAt = &formatted
	}
	return clientQuotaResponse{
		Valid:     true,
		Source:    "budget",
		PlanName:  primary.row.ScopeType + "_budget",
		Unlimited: false,
		Remaining: &rem,
		Total:     &tot,
		Used:      &used,
		Unit:      unit,
		ResetAt:   resetAt,
		Items:     items,
		Usage30d:  usage,
	}, true
}

func buildUpstreamQuotaResponse(upstream []UpstreamQuotaItem, usage quotaUsage30d) (clientQuotaResponse, bool) {
	if len(upstream) == 0 {
		return clientQuotaResponse{}, false
	}
	items := make([]quotaItemResponse, 0, len(upstream))
	for _, entry := range upstream {
		planName := entry.PlanName
		if planName == "" {
			planName = entry.LabelKey
		}
		if planName == "" {
			planName = entry.Provider
		}
		items = append(items, quotaItemResponse{
			LabelKey:      entry.LabelKey,
			Kind:          entry.Kind,
			PlanName:      planName,
			Remaining:     entry.Remaining,
			Total:         entry.Total,
			Used:          entry.Used,
			Unit:          entry.Unit,
			NextResetTime: entry.NextResetTime,
		})
	}
	first := items[0]
	var resetAt *string
	if first.NextResetTime != nil && *first.NextResetTime > 0 {
		ms := *first.NextResetTime
		// Convert seconds vs milliseconds cleanly if needed; upstream endpoints return Unix ms.
		var ts time.Time
		if ms > 1_000_000_000_000 {
			ts = time.UnixMilli(ms).UTC()
		} else {
			ts = time.Unix(ms, 0).UTC()
		}
		formatted := ts.Format(time.RFC3339)
		resetAt = &formatted
	}
	return clientQuotaResponse{
		Valid:     true,
		Source:    "upstream_balance",
		PlanName:  first.PlanName,
		Unlimited: false,
		Remaining: first.Remaining,
		Total:     first.Total,
		Used:      first.Used,
		Unit:      first.Unit,
		ResetAt:   resetAt,
		Items:     items,
		Usage30d:  usage,
	}, true
}

func buildUsageFallbackQuotaResponse(usage quotaUsage30d) clientQuotaResponse {
	used := roundQuotaValue(usage.Cost)
	return clientQuotaResponse{
		Valid:     true,
		Source:    "usage",
		PlanName:  "pay_as_you_go",
		Unlimited: true,
		Remaining: nil,
		Total:     nil,
		Used:      &used,
		Unit:      "USD",
		Items:     []quotaItemResponse{},
		Usage30d:  usage,
	}
}

func (d Deps) queryKeyUsage30d(ctx context.Context, keyCtx apiKeyContext) (quotaUsage30d, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cost), 0),
		       COALESCE(NULLIF(cost_currency, ''), 'USD')
		FROM usage_records
		WHERE api_key_id = $1 AND state = 'success' AND created_at >= now() - interval '30 days'
		GROUP BY COALESCE(NULLIF(cost_currency, ''), 'USD')
	`, keyCtx.KeyID)
	if err != nil {
		return quotaUsage30d{}, err
	}
	defer rows.Close()

	fxRates := d.fxRatesSnapshot()
	if len(fxRates) == 0 {
		fxRates = map[string]float64{"USD": 1.0, "CNY": 7.2}
	}
	out := quotaUsage30d{Currency: "USD"}
	for rows.Next() {
		var (
			reqs     int64
			inTok    int64
			outTok   int64
			costRaw  float64
			currency string
		)
		if err := rows.Scan(&reqs, &inTok, &outTok, &costRaw, &currency); err != nil {
			return quotaUsage30d{}, err
		}
		out.Requests += reqs
		out.InputTokens += inTok
		out.OutputTokens += outTok
		if converted, ok := fx.ConvertWithMap(costRaw, currency, "USD", fxRates); ok {
			out.Cost += converted
		} else {
			out.Cost += costRaw
		}
	}
	if err := rows.Err(); err != nil {
		return quotaUsage30d{}, err
	}
	out.Cost = roundQuotaValue(out.Cost)
	return out, nil
}

// Quota handles GET /v1/quota so coding clients and CC Switch usage scripts can inspect
// budget limits, optional upstream balances, and 30-day API key consumption.
func (d Deps) Quota(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		writeProtocolError(w, r, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}

	usage, err := d.queryKeyUsage30d(r.Context(), keyCtx)
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to query API key usage.")
		return
	}

	if windows, bErr := d.loadActiveBudgets(r.Context(), keyCtx); bErr == nil {
		if resp, ok := buildBudgetQuotaResponse(windows, usage); ok {
			writeJSON(w, http.StatusOK, resp)
			return
		}
	} else {
		d.Logger.Warn("quota load active budgets failed", "error", bErr)
	}

	var exposeUpstream bool
	err = d.Pool.QueryRow(r.Context(), `SELECT expose_upstream_balance_to_clients FROM system_settings WHERE singleton`).Scan(&exposeUpstream)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		d.Logger.Warn("quota read expose_upstream_balance_to_clients failed", "error", err)
	}
	if exposeUpstream && d.QueryUpstreamBalances != nil {
		upstreamItems, uErr := d.QueryUpstreamBalances(r.Context(), keyCtx.OrganizationID, keyCtx.ProjectID)
		if uErr == nil {
			if resp, ok := buildUpstreamQuotaResponse(upstreamItems, usage); ok {
				writeJSON(w, http.StatusOK, resp)
				return
			}
		} else {
			d.Logger.Debug("quota query upstream balances skipped", "error", uErr)
		}
	}

	writeJSON(w, http.StatusOK, buildUsageFallbackQuotaResponse(usage))
}
