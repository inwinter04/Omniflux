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
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func adminOK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, AdminEnvelope{Code: "0000", Msg: "ok", Data: data})
}

func adminError(w http.ResponseWriter, status int, code, message string, data any) {
	JSON(w, status, AdminEnvelope{Code: code, Msg: message, Data: data})
}

func adminFail(w http.ResponseWriter, status int, message string) {
	adminError(w, status, fmt.Sprintf("%d", status), message, nil)
}
