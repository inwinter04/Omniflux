package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type AdminEnvelope struct {
	Code   string         `json:"code"`
	Reason string         `json:"reason,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Detail string         `json:"detail,omitempty"`
	Data   any            `json:"data,omitempty"`
}

func adminOK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, AdminEnvelope{Code: "0000", Data: data})
}

func adminError(w http.ResponseWriter, status int, code, reason string, params map[string]any, detail ...string) {
	envelope := AdminEnvelope{Code: code, Reason: reason, Params: params}
	if len(detail) > 0 {
		envelope.Detail = detail[0]
	}
	JSON(w, status, envelope)
}

func adminFail(w http.ResponseWriter, status int, reason string) {
	adminError(w, status, fmt.Sprintf("%d", status), reason, nil)
}
