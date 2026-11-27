package dataplane

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pendingUsage 是请求开始时写入 usage_records 与 policy_decisions 的最小事实。
type pendingUsage struct {
	RequestID       uuid.UUID
	OrganizationID  uuid.UUID
	ProjectID       *uuid.UUID
	ApplicationID   *uuid.UUID
	APIKeyID        *uuid.UUID
	ActorUserID     *uuid.UUID
	Source          string
	ModelServiceID  uuid.UUID
	RoutePolicyID   *uuid.UUID
	PolicyVersion   *int
	Strategy        string
	MatchedRuleID   string
	MatchedRuleName string
	DecisionDetail  []byte
	Reason          string
	Candidates      []byte // jsonb: 配置的候选列表
	SelectedID      *uuid.UUID
	FallbackIDs     []uuid.UUID
}

// finalUsage 是请求结束后对 usage_records 与 policy_decisions 的定稿更新。
type finalUsage struct {
	State               string // success / failed / interrupted
	StatusCode          *int
	CandidateID         *uuid.UUID
	Provider            *string
	UpstreamModel       *string
	InputTokens         int64
	OutputTokens        int64
	CachedTokens        int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	Protocol            string
	Cost                float64
	Currency            string
	FirstTokenMS        *int
	TotalMS             int
	UsageSource         *string
	ErrorType           *string
	ErrorMessage        *string
	AttemptTrail        []byte
}

type usageStore struct {
	pool    *pgxpool.Pool
	fxRates map[string]float64
}

// insertPending 在一个事务里写入 pending 用量与 allowed 路由决策快照。
func (s usageStore) insertPending(ctx context.Context, p pendingUsage) error {
	source := p.Source
	if source == "" {
		source = "api"
	}
	detail := p.DecisionDetail
	if len(detail) == 0 {
		detail = []byte("{}")
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO usage_records(
				request_id, organization_id, project_id, application_id, api_key_id,
				actor_user_id, source, model_service_id, route_policy_id, state
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending')
		`, p.RequestID, p.OrganizationID, nullableUUID(p.ProjectID), nullableUUID(p.ApplicationID), nullableUUID(p.APIKeyID),
			nullableUUID(p.ActorUserID), source, p.ModelServiceID, nullableUUID(p.RoutePolicyID)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO policy_decisions(
				request_id, organization_id, api_key_id, route_policy_id,
				policy_version, strategy, matched_rule_id, matched_rule_name, decision_detail,
				decision, reason, candidates, selected_candidate_id, fallback_candidate_ids
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'allowed',$10,$11,$12,$13)
		`, p.RequestID, p.OrganizationID, nullableUUID(p.APIKeyID), nullableUUID(p.RoutePolicyID),
			nullableInt(p.PolicyVersion), nullableStringValue(p.Strategy), nullableStringValue(p.MatchedRuleID), nullableStringValue(p.MatchedRuleName), detail,
			p.Reason, p.Candidates, nullableUUID(p.SelectedID), p.FallbackIDs)
		return err
	})
}

// finalize 定稿用量与决策。路由决策的 attempt_trail 一并更新。
func (s usageStore) finalize(ctx context.Context, requestID uuid.UUID, f finalUsage) error {
	currency := f.Currency
	if currency == "" {
		currency = "USD"
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE usage_records SET
				state = $2, status_code = $3, model_candidate_id = $4,
				provider = $5, upstream_model = $6,
				input_tokens = $7, output_tokens = $8, cached_tokens = $9, reasoning_tokens = $10,
				cost = $11, first_token_ms = $12, total_ms = $13, usage_source = $14,
				error_type = $15, error_message = $16, cost_currency = COALESCE($17, cost_currency),
				cache_creation_tokens = $18, protocol = COALESCE($19, protocol), finalized_at = now()
			WHERE request_id = $1 AND state = 'pending'
		`, requestID, f.State, nullableInt(f.StatusCode), nullableUUID(f.CandidateID),
			nullableString(f.Provider), nullableString(f.UpstreamModel),
			f.InputTokens, f.OutputTokens, f.CachedTokens, f.ReasoningTokens,
			f.Cost, nullableInt(f.FirstTokenMS), f.TotalMS, nullableString(f.UsageSource),
			nullableString(f.ErrorType), nullableString(f.ErrorMessage), nullableString(&currency),
			f.CacheCreationTokens, nullableStringValue(f.Protocol)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE policy_decisions SET attempt_trail = $2
			WHERE request_id = $1
		`, requestID, f.AttemptTrail)
		return err
	})
	if err == nil {
		// 定稿是所有请求的唯一出口,业务指标在这里统一采集。
		recordFinalizedMetrics(f, s.fxRates)
	}
	return err
}

func nullableUUID(v *uuid.UUID) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableStringValue(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
