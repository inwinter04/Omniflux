package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultPrimaryURL  = "https://api.frankfurter.dev/v1/latest?base=USD"
	defaultFallbackURL = "https://open.er-api.com/v6/latest/USD"
)

var defaultBuiltinRates = map[string]float64{
	"USD": 1.0,
	"CNY": 7.2,
	"EUR": 0.92,
	"GBP": 0.79,
	"JPY": 154.0,
	"HKD": 7.8,
	"TWD": 32.5,
	"KRW": 1380.0,
}

type Settings struct {
	AutoSyncEnabled   bool       `json:"auto_sync_enabled"`
	SyncIntervalHours int        `json:"sync_interval_hours"`
	PrimarySource     string     `json:"primary_source"`
	FallbackSource    string     `json:"fallback_source"`
	LastSyncedAt      *time.Time `json:"last_synced_at"`
	LastSyncStatus    string     `json:"last_sync_status"`
	LastSyncSource    *string    `json:"last_sync_source"`
	LastSyncError     *string    `json:"last_sync_error"`
	UpdatedBy         *string    `json:"updated_by,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

type RateEntry struct {
	Currency       string    `json:"currency"`
	RatePerUSD     float64   `json:"rate_per_usd"`
	Source         string    `json:"source"`
	EffectiveDate  *string   `json:"effective_date"`
	ManualOverride bool      `json:"manual_override"`
	UpdatedBy      *string   `json:"updated_by,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Service manages authoritative FX rates with PostgreSQL persistence and an in-memory cache.
type Service struct {
	pool        *pgxpool.Pool
	logger      *slog.Logger
	client      *http.Client
	primaryURL  string
	fallbackURL string

	mu     sync.RWMutex
	rates  map[string]RateEntry
	syncMu sync.Mutex
}

func NewService(pool *pgxpool.Pool, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	rates := make(map[string]RateEntry, len(defaultBuiltinRates))
	now := time.Now()
	for cur, val := range defaultBuiltinRates {
		rates[cur] = RateEntry{
			Currency:   cur,
			RatePerUSD: val,
			Source:     "builtin",
			UpdatedAt:  now,
		}
	}
	return &Service{
		pool:        pool,
		logger:      logger,
		client:      &http.Client{Timeout: 12 * time.Second},
		primaryURL:  defaultPrimaryURL,
		fallbackURL: defaultFallbackURL,
		rates:       rates,
	}
}

// Start loads cached rates from PostgreSQL and schedules periodic authoritative syncs.
func (s *Service) Start(ctx context.Context) {
	if s == nil || s.pool == nil {
		return
	}
	if err := s.reloadCache(ctx); err != nil {
		s.logger.Warn("load initial fx rates failed", "error", err)
	}
	go s.runLoop(ctx)
}

func (s *Service) runLoop(ctx context.Context) {
	s.maybeSync(ctx)
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.maybeSync(ctx)
		}
	}
}

func (s *Service) maybeSync(ctx context.Context) {
	settings, err := s.loadSettings(ctx)
	if err != nil || !settings.AutoSyncEnabled {
		return
	}
	interval := time.Duration(settings.SyncIntervalHours) * time.Hour
	if interval <= 0 {
		interval = 12 * time.Hour
	}
	if settings.LastSyncedAt != nil && time.Since(*settings.LastSyncedAt) < interval {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, _, err := s.SyncNow(syncCtx); err != nil {
		s.logger.Warn("scheduled fx rate sync failed", "error", err)
	}
}

func (s *Service) reloadCache(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, entries, err := s.List(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]RateEntry, len(entries)+1)
	for _, entry := range entries {
		next[entry.Currency] = entry
	}
	if _, ok := next["USD"]; !ok {
		next["USD"] = RateEntry{Currency: "USD", RatePerUSD: 1.0, Source: "builtin"}
	}
	s.mu.Lock()
	s.rates = next
	s.mu.Unlock()
	return nil
}

// Snapshot returns a copy of current currency -> rate_per_usd mappings.
func (s *Service) Snapshot() map[string]float64 {
	out := make(map[string]float64, len(defaultBuiltinRates))
	for k, v := range defaultBuiltinRates {
		out[k] = v
	}
	if s == nil {
		return out
	}
	s.mu.RLock()
	for k, entry := range s.rates {
		if entry.RatePerUSD > 0 {
			out[k] = entry.RatePerUSD
		}
	}
	s.mu.RUnlock()
	out["USD"] = 1.0
	return out
}

// Convert converts an amount from one 3-letter currency to another using live cached rates.
func (s *Service) Convert(amount float64, fromCurrency, toCurrency string) (float64, bool) {
	return ConvertWithMap(amount, fromCurrency, toCurrency, s.Snapshot())
}

// ConvertWithMap performs pure deterministic FX conversion given rate_per_usd values.
func ConvertWithMap(amount float64, fromCurrency, toCurrency string, ratePerUSD map[string]float64) (float64, bool) {
	from := strings.ToUpper(strings.TrimSpace(fromCurrency))
	to := strings.ToUpper(strings.TrimSpace(toCurrency))
	if from == "" {
		from = "USD"
	}
	if to == "" {
		to = "USD"
	}
	if from == to {
		return amount, true
	}
	fromRate, okFrom := ratePerUSD[from]
	if from == "USD" {
		fromRate = 1.0
		okFrom = true
	}
	toRate, okTo := ratePerUSD[to]
	if to == "USD" {
		toRate = 1.0
		okTo = true
	}
	if !okFrom || !okTo || fromRate <= 0 || toRate <= 0 {
		return 0, false
	}
	return amount * (toRate / fromRate), true
}

func (s *Service) List(ctx context.Context) (Settings, []RateEntry, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return Settings{}, nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT currency, rate_per_usd::float8, source, to_char(effective_date, 'YYYY-MM-DD'),
		       manual_override, updated_by::text, updated_at
		FROM fx_rates
		ORDER BY CASE currency WHEN 'USD' THEN 0 WHEN 'CNY' THEN 1 WHEN 'EUR' THEN 2 ELSE 3 END, currency ASC
	`)
	if err != nil {
		return Settings{}, nil, err
	}
	defer rows.Close()
	list := make([]RateEntry, 0, 16)
	for rows.Next() {
		var entry RateEntry
		if err := rows.Scan(&entry.Currency, &entry.RatePerUSD, &entry.Source, &entry.EffectiveDate, &entry.ManualOverride, &entry.UpdatedBy, &entry.UpdatedAt); err != nil {
			return Settings{}, nil, err
		}
		list = append(list, entry)
	}
	return settings, list, rows.Err()
}

func (s *Service) loadSettings(ctx context.Context) (Settings, error) {
	var st Settings
	err := s.pool.QueryRow(ctx, `
		SELECT auto_sync_enabled, sync_interval_hours, primary_source, fallback_source,
		       last_synced_at, last_sync_status, last_sync_source, last_sync_error,
		       updated_by::text, updated_at
		FROM fx_rate_settings WHERE singleton
	`).Scan(&st.AutoSyncEnabled, &st.SyncIntervalHours, &st.PrimarySource, &st.FallbackSource,
		&st.LastSyncedAt, &st.LastSyncStatus, &st.LastSyncSource, &st.LastSyncError,
		&st.UpdatedBy, &st.UpdatedAt)
	return st, err
}

func (s *Service) UpdateSettings(ctx context.Context, autoSyncEnabled bool, syncIntervalHours int, userID uuid.UUID) (Settings, error) {
	if syncIntervalHours < 1 || syncIntervalHours > 168 {
		return Settings{}, errors.New("sync_interval_hours must be between 1 and 168")
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE fx_rate_settings SET
			auto_sync_enabled = $1,
			sync_interval_hours = $2,
			updated_by = $3,
			updated_at = now()
		WHERE singleton
	`, autoSyncEnabled, syncIntervalHours, userID); err != nil {
		return Settings{}, err
	}
	return s.loadSettings(ctx)
}

func (s *Service) SetManualRate(ctx context.Context, currency string, ratePerUSD float64, manualOverride bool, userID uuid.UUID) (RateEntry, error) {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if len(cur) != 3 {
		return RateEntry{}, errors.New("currency must be a 3-letter ISO code")
	}
	if cur == "USD" {
		return RateEntry{}, errors.New("USD is the base currency fixed at 1.0")
	}
	if ratePerUSD <= 0 {
		return RateEntry{}, errors.New("rate_per_usd must be greater than zero")
	}
	source := "manual"
	if !manualOverride {
		source = "builtin"
	}
	var entry RateEntry
	err := s.pool.QueryRow(ctx, `
		INSERT INTO fx_rates(currency, rate_per_usd, source, effective_date, manual_override, updated_by, updated_at)
		VALUES ($1, $2, $3, current_date, $4, $5, now())
		ON CONFLICT (currency) DO UPDATE SET
			rate_per_usd = EXCLUDED.rate_per_usd,
			source = EXCLUDED.source,
			effective_date = CASE WHEN EXCLUDED.manual_override THEN current_date ELSE fx_rates.effective_date END,
			manual_override = EXCLUDED.manual_override,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
		RETURNING currency, rate_per_usd::float8, source, to_char(effective_date, 'YYYY-MM-DD'),
		          manual_override, updated_by::text, updated_at
	`, cur, ratePerUSD, source, manualOverride, userID).Scan(
		&entry.Currency, &entry.RatePerUSD, &entry.Source, &entry.EffectiveDate, &entry.ManualOverride, &entry.UpdatedBy, &entry.UpdatedAt,
	)
	if err != nil {
		return RateEntry{}, err
	}
	_ = s.reloadCache(ctx)
	return entry, nil
}

type fetchedRate struct {
	rate   float64
	source string
	date   string
}

// SyncNow fetches authoritative exchange rates (Frankfurter ECB primary, Open ER-API supplement/fallback)
// and updates non-manual rows in PostgreSQL + memory cache.
func (s *Service) SyncNow(ctx context.Context) (Settings, []RateEntry, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	_, existing, err := s.List(ctx)
	if err != nil {
		return Settings{}, nil, err
	}
	tracked := make([]string, 0, len(existing))
	manualSet := make(map[string]bool)
	for _, item := range existing {
		if item.Currency == "USD" {
			continue
		}
		tracked = append(tracked, item.Currency)
		if item.ManualOverride {
			manualSet[item.Currency] = true
		}
	}

	merged := make(map[string]fetchedRate)
	var errs []string
	var usedSources []string

	ecbRates, ecbDate, ecbErr := s.fetchFrankfurter(ctx)
	if ecbErr != nil {
		errs = append(errs, fmt.Sprintf("frankfurter_ecb: %v", ecbErr))
	} else {
		usedSources = append(usedSources, "frankfurter_ecb")
		for cur, val := range ecbRates {
			if val > 0 {
				merged[strings.ToUpper(cur)] = fetchedRate{rate: val, source: "frankfurter_ecb", date: ecbDate}
			}
		}
	}

	needFallback := ecbErr != nil
	if !needFallback {
		for _, cur := range tracked {
			if !manualSet[cur] {
				if _, ok := merged[cur]; !ok {
					needFallback = true
					break
				}
			}
		}
	}
	if needFallback {
		erRates, erDate, erErr := s.fetchOpenERAPI(ctx)
		if erErr != nil {
			errs = append(errs, fmt.Sprintf("open_er_api: %v", erErr))
		} else {
			usedSources = append(usedSources, "open_er_api")
			for cur, val := range erRates {
				up := strings.ToUpper(cur)
				if val <= 0 {
					continue
				}
				if _, exists := merged[up]; !exists {
					merged[up] = fetchedRate{rate: val, source: "open_er_api", date: erDate}
				}
			}
		}
	}

	if len(merged) == 0 {
		errText := strings.Join(errs, "; ")
		_, _ = s.pool.Exec(ctx, `
			UPDATE fx_rate_settings SET
				last_synced_at = now(),
				last_sync_status = 'error',
				last_sync_error = $1,
				updated_at = now()
			WHERE singleton
		`, errText)
		settings, list, listErr := s.List(ctx)
		if listErr != nil {
			return Settings{}, nil, errors.New(errText)
		}
		return settings, list, errors.New(errText)
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, cur := range tracked {
			if manualSet[cur] {
				continue
			}
			item, ok := merged[cur]
			if !ok || item.rate <= 0 {
				continue
			}
			var effDate any
			if item.date != "" {
				effDate = item.date
			}
			if _, err := tx.Exec(ctx, `
				UPDATE fx_rates SET
					rate_per_usd = $2,
					source = $3,
					effective_date = COALESCE($4::date, current_date),
					updated_at = now()
				WHERE currency = $1 AND manual_override = false
			`, cur, item.rate, item.source, effDate); err != nil {
				return err
			}
		}
		status := "ok"
		var syncErr any
		if len(errs) > 0 {
			status = "partial"
			syncErr = strings.Join(errs, "; ")
		}
		sourceText := strings.Join(usedSources, "+")
		_, err := tx.Exec(ctx, `
			UPDATE fx_rate_settings SET
				last_synced_at = now(),
				last_sync_status = $1,
				last_sync_source = $2,
				last_sync_error = $3,
				updated_at = now()
			WHERE singleton
		`, status, sourceText, syncErr)
		return err
	})
	if err != nil {
		return Settings{}, nil, err
	}
	_ = s.reloadCache(ctx)
	return s.List(ctx)
}

func (s *Service) fetchFrankfurter(ctx context.Context) (map[string]float64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.primaryURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("http %d", resp.StatusCode)
	}
	var payload struct {
		Base  string             `json:"base"`
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, "", err
	}
	if len(payload.Rates) == 0 {
		return nil, "", errors.New("empty frankfurter rates")
	}
	return payload.Rates, payload.Date, nil
}

func (s *Service) fetchOpenERAPI(ctx context.Context) (map[string]float64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.fallbackURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("http %d", resp.StatusCode)
	}
	var payload struct {
		Result         string             `json:"result"`
		TimeLastUnix   int64              `json:"time_last_update_unix"`
		Rates          map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, "", err
	}
	if payload.Result != "" && payload.Result != "success" {
		return nil, "", fmt.Errorf("er-api result %s", payload.Result)
	}
	if len(payload.Rates) == 0 {
		return nil, "", errors.New("empty er-api rates")
	}
	dateStr := time.Now().UTC().Format("2006-01-02")
	if payload.TimeLastUnix > 0 {
		dateStr = time.Unix(payload.TimeLastUnix, 0).UTC().Format("2006-01-02")
	}
	return payload.Rates, dateStr, nil
}
