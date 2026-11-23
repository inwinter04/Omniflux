package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryZhipuPlanBalanceUsesQuotaSemantics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/monitor/usage/quota/limit" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"limits": []any{
				map[string]any{"type": "TOKENS_LIMIT", "percentage": 35.5, "nextResetTime": 1795368000000},
				map[string]any{"type": "CREDIT_LIMIT", "percentage": 70, "nextResetTime": 1795454400000},
			}},
		})
	}))
	defer server.Close()
	entries, err := queryZhipuPlanBalance(t.Context(), "zhipu-glm-coding", server.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	first := entries[0]
	if first.Kind != "quota" || first.Window != "5 小时" || first.Used == nil || *first.Used != 35.5 || first.Remaining == nil || *first.Remaining != 64.5 {
		t.Fatalf("quota semantics = %#v", first)
	}
	if first.NextResetTime == nil || *first.NextResetTime != 1795368000000 {
		t.Fatalf("reset time = %#v", first.NextResetTime)
	}
	if entries[1].Label != "每周限额" || entries[1].Remaining == nil || *entries[1].Remaining != 30 {
		t.Fatalf("weekly quota = %#v", entries[1])
	}
}
