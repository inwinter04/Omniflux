package dataplane

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pendingUsage 是请求开始时写入 usage_records 与 policy_decisions 的最小事实。
type pendingUsage struct {
	RequestID     uuid.UUID
	OrganizationID uuid.UUID
	ProjectID     uuid.UUID
	ApplicationID uuid.UUID
	APIKeyID      uuid.UUID
	ModelServiceID uuid.UUID
	RoutePolicyID *uuid.UUID
	Reason        string
	Candidates    []byte // jsonb: 配置的候选列表
	SelectedID    *uuid.UUID
	FallbackIDs   []uuid.UUID
}

// finalUsage 是请求结束后对 usage_records 与 policy_decisions 的定稿更新。
type finalUsage struct {
	State           string // success / failed / interrupted
	StatusCode      *int
	CandidateID     *uuid.UUID
	Provider        *string
	UpstreamModel   *string
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
	Cost            float64
	FirstTokenMS    *int
	TotalMS         int
	UsageSource     *string
	ErrorType       *string
	ErrorMessage    *string
	AttemptTrail    []byte
}

type usageStore struct {
	pool *pgxpool.Pool
}

// insertPending 在一个事务里写入 pending 用量与 allowed 路由决策。
func (s usageStore) insertPending(ctx context.Context, p pendingUsage) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO usage_records(
				request_id, organization_id, project_id, application_id, api_key_id,
				model_service_id, route_policy_id, state
			) VALUES ($1,$2,$3,$4,$5,$6,$7,'pending')
		`, p.RequestID, p.OrganizationID, p.ProjectID, p.ApplicationID, p.APIKeyID,
			p.ModelServiceID, nullableUUID(p.RoutePolicyID)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO policy_decisions(
				request_id, organization_id, api_key_id, route_policy_id,
				decision, reason, candidates, selected_candidate_id, fallback_candidate_ids
			) VALUES ($1,$2,$3,$4,'allowed',$5,$6,$7,$8)
		`, p.RequestID, p.OrganizationID, p.APIKeyID, nullableUUID(p.RoutePolicyID),
			p.Reason, p.Candidates, nullableUUID(p.SelectedID), p.FallbackIDs)
		return err
	})
}

// finalize 定稿用量与决策。路由决策的 attempt_trail 一并更新。
func (s usageStore) finalize(ctx context.Context, requestID uuid.UUID, f finalUsage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE usage_records SET
			state = $2, status_code = $3, model_candidate_id = $4,
			provider = $5, upstream_model = $6,
			input_tokens = $7, output_tokens = $8, cached_tokens = $9, reasoning_tokens = $10,
			cost = $11, first_token_ms = $12, total_ms = $13, usage_source = $14,
			error_type = $15, error_message = $16, finalized_at = now()
		WHERE request_id = $1 AND state = 'pending'
	`, requestID, f.State, nullableInt(f.StatusCode), nullableUUID(f.CandidateID),
		nullableString(f.Provider), nullableString(f.UpstreamModel),
		f.InputTokens, f.OutputTokens, f.CachedTokens, f.ReasoningTokens,
		f.Cost, nullableInt(f.FirstTokenMS), f.TotalMS, nullableString(f.UsageSource),
		nullableString(f.ErrorType), nullableString(f.ErrorMessage))
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE policy_decisions SET attempt_trail = $2
		WHERE request_id = $1
	`, requestID, f.AttemptTrail)
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

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

