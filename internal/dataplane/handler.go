package dataplane

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// Deps 是数据面处理器的依赖集合。
type Deps struct {
	Logger       *slog.Logger
	Pool         *pgxpool.Pool
	Engine       *bifrostx.Engine
	APIKeyPepper string
}

// apiError 输出 OpenAI 风格的错误 JSON。
func apiError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    errType,
			"message": message,
			"code":    status,
		},
	})
}
