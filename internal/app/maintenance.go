package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// maintenanceInterval 是维护循环周期;pendingTTL 是判定请求被中断遗弃的时限。
	// 服务端最长写超时 300s、上游超时默认 60s,1 小时远大于正常请求生命周期。
	maintenanceInterval = 10 * time.Minute
	pendingTTL          = "1 hour"
)

// providerAccountSuccessRate 按 provider 账号输出近 24 小时成功率(0~1)。
// 无流量的账号不产出样本,避免空账号刷屏。
var providerAccountSuccessRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "omniflux_provider_account_success_rate_24h",
	Help: "Success rate of each provider account over the last 24 hours (passive, from usage records)",
}, []string{"account_id", "provider"})

// StartMaintenance 启动后台维护循环:对账被中断遗弃的 pending 用量、刷新账号健康指标。
// 循环随 ctx 取消退出;单轮失败记日志,下一轮自动重试。
func StartMaintenance(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) {
	knownAccounts := map[string]bool{}
	runOnce(ctx, logger, pool, knownAccounts)
	go func() {
		ticker := time.NewTicker(maintenanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce(ctx, logger, pool, knownAccounts)
			}
		}
	}()
}

func runOnce(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, knownAccounts map[string]bool) {
	reconcilePending(ctx, logger, pool)
	refreshProviderHealth(ctx, logger, pool, knownAccounts)
}

// reconcilePending 把长时间停留在 pending 的用量定稿为 failed。
// 正常路径一定经过 finalize;只有进程崩溃/重启才会遗留 pending。
func reconcilePending(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) {
	var count int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
	        UPDATE usage_records
	        SET state = 'failed', status_code = 504,
	            error_type = 'abandoned',
	            error_message = 'server interrupted before completion; usage reconciled as failed',
	            finalized_at = now()
	        WHERE state = 'pending' AND created_at < now() - interval '`+pendingTTL+`'
	    `)
		if err != nil {
			return err
		}
		count = tag.RowsAffected()
		if count == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `
            UPDATE policy_decisions d
            SET decision = 'failed',
                reason = d.reason || '; abandoned: server interrupted before completion'
            WHERE d.decision = 'allowed'
              AND EXISTS (
                SELECT 1 FROM usage_records u
                WHERE u.request_id = d.request_id AND u.state = 'failed'
                      AND u.error_type = 'abandoned'
              )
        `)
		return err
	})
	if err != nil {
		logger.Warn("reconcile pending usage failed", "error", err)
		return
	}
	if count > 0 {
		logger.Info("reconciled abandoned pending usage", "count", count)
	}
}

// refreshProviderHealth 从用量记录计算各账号近 24h 成功率并写入指标;
// 已消失账号的旧标签由调用方清理,避免指标残留。
func refreshProviderHealth(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, knownAccounts map[string]bool) {
	rows, err := pool.Query(ctx, `
        SELECT a.id, a.provider,
               count(u.id),
               avg(CASE WHEN u.state = 'success' THEN 1.0 ELSE 0.0 END) FILTER (WHERE u.id IS NOT NULL)
        FROM provider_accounts a
        LEFT JOIN model_candidates mc ON mc.provider_account_id = a.id
        LEFT JOIN usage_records u ON u.model_candidate_id = mc.id
             AND u.created_at >= now() - interval '24 hours'
             AND u.state <> 'pending'
        GROUP BY a.id, a.provider
    `)
	if err != nil {
		logger.Warn("refresh provider health failed", "error", err)
		return
	}
	defer rows.Close()
	current := map[string]bool{}
	for rows.Next() {
		var id, provider string
		var requests *int64
		var rate *float64
		if err := rows.Scan(&id, &provider, &requests, &rate); err != nil {
			logger.Warn("scan provider health failed", "error", err)
			return
		}
		key := id + "|" + provider
		if requests != nil && *requests > 0 && rate != nil {
			current[key] = true
			knownAccounts[key] = true
			providerAccountSuccessRate.WithLabelValues(id, provider).Set(*rate)
		}
	}
	if err := rows.Err(); err != nil {
		logger.Warn("iterate provider health failed", "error", err)
		return
	}
	for key := range knownAccounts {
		if !current[key] {
			parts := strings.SplitN(key, "|", 2)
			providerAccountSuccessRate.DeleteLabelValues(parts[0], parts[1])
			delete(knownAccounts, key)
		}
	}
}
