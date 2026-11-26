package fx

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConvertWithMap(t *testing.T) {
	rates := map[string]float64{
		"USD": 1.0,
		"CNY": 7.0,
		"EUR": 0.9,
		"JPY": 150.0,
	}
	cnyToUSD, ok := ConvertWithMap(14, "CNY", "USD", rates)
	if !ok || math.Abs(cnyToUSD-2.0) > 1e-9 {
		t.Fatalf("CNY -> USD = (%v, %v), want (2.0, true)", cnyToUSD, ok)
	}
	usdToCNY, ok := ConvertWithMap(2, "USD", "CNY", rates)
	if !ok || math.Abs(usdToCNY-14.0) > 1e-9 {
		t.Fatalf("USD -> CNY = (%v, %v), want (14.0, true)", usdToCNY, ok)
	}
	cnyToJPY, ok := ConvertWithMap(7, "CNY", "JPY", rates)
	if !ok || math.Abs(cnyToJPY-150.0) > 1e-9 {
		t.Fatalf("CNY -> JPY = (%v, %v), want (150.0, true)", cnyToJPY, ok)
	}
	if _, ok := ConvertWithMap(10, "ZZZ", "USD", rates); ok {
		t.Fatal("unknown currency should return ok=false")
	}
}

func TestFetchPrimaryAndFallback(t *testing.T) {
	ecb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base":  "USD",
			"date":  "2026-11-25",
			"rates": map[string]float64{"CNY": 6.68, "EUR": 0.91},
		})
	}))
	defer ecb.Close()

	er := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":                "success",
			"time_last_update_unix": 1795564800,
			"rates":                 map[string]float64{"USD": 1, "CNY": 6.70, "TWD": 32.1},
		})
	}))
	defer er.Close()

	svc := NewService(nil, nil)
	svc.primaryURL = ecb.URL
	svc.fallbackURL = er.URL

	ecbRates, ecbDate, err := svc.fetchFrankfurter(context.Background())
	if err != nil {
		t.Fatalf("fetchFrankfurter failed: %v", err)
	}
	if ecbDate != "2026-11-25" || math.Abs(ecbRates["CNY"]-6.68) > 1e-9 {
		t.Fatalf("unexpected ECB payload: date=%s rates=%v", ecbDate, ecbRates)
	}

	erRates, erDate, err := svc.fetchOpenERAPI(context.Background())
	if err != nil {
		t.Fatalf("fetchOpenERAPI failed: %v", err)
	}
	if erDate == "" || math.Abs(erRates["TWD"]-32.1) > 1e-9 {
		t.Fatalf("unexpected ER-API payload: date=%s rates=%v", erDate, erRates)
	}
}
