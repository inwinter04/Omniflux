package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/dataplane"
	"github.com/inwinter04/omniflux/internal/fx"
)

type updateFXSettingsRequest struct {
	AutoSyncEnabled   bool `json:"auto_sync_enabled"`
	SyncIntervalHours int  `json:"sync_interval_hours"`
}

type updateFXRateRequest struct {
	RatePerUSD     float64 `json:"rate_per_usd"`
	ManualOverride *bool   `json:"manual_override"`
}

// FXRates returns cached FX rate settings and currency entries for system config and routing views.
func (d ServerDeps) FXRates(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) &&
		!principal.HasPermission(auth.PermRoutePolicyRead) &&
		!principal.HasPermission(auth.PermModelServiceRead) &&
		!principal.HasPermission(auth.PermProviderRead) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission-denied", nil)
		return
	}
	if d.FX == nil {
		adminOK(w, map[string]any{
			"settings": fx.Settings{AutoSyncEnabled: true, SyncIntervalHours: 12, PrimarySource: "frankfurter_ecb", FallbackSource: "open_er_api", LastSyncStatus: "never"},
			"rates":    []fx.RateEntry{},
		})
		return
	}
	settings, rates, err := d.FX.List(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "FX_RATES_READ_FAILED", "failed-to-query-fx-rates", nil, err.Error())
		return
	}
	adminOK(w, map[string]any{
		"settings": settings,
		"rates":    rates,
	})
}

// UpdateFXSettings updates automatic FX synchronization interval and toggle.
func (d ServerDeps) UpdateFXSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	if d.FX == nil {
		adminError(w, http.StatusServiceUnavailable, "FX_SERVICE_UNAVAILABLE", "fx-service-unavailable", nil)
		return
	}
	var body updateFXSettingsRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if body.SyncIntervalHours < 1 || body.SyncIntervalHours > 168 {
		adminError(w, http.StatusBadRequest, "FX_SETTINGS_INVALID", "invalid-fx-settings", nil)
		return
	}
	settings, err := d.FX.UpdateSettings(r.Context(), body.AutoSyncEnabled, body.SyncIntervalHours, principal.UserID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "FX_SETTINGS_UPDATE_FAILED", "failed-to-update-fx-settings", nil, err.Error())
		return
	}
	_ = writeAuditEvent(r.Context(), d.Pool, principal.OrganizationID, principal.UserID,
		"fx_rate.settings_updated", "fx_rate", "settings", nil, map[string]any{
			"auto_sync_enabled":   settings.AutoSyncEnabled,
			"sync_interval_hours": settings.SyncIntervalHours,
		})
	adminOK(w, settings)
}

// UpdateFXRate updates or pins a manual exchange rate for a non-USD currency.
func (d ServerDeps) UpdateFXRate(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	if d.FX == nil {
		adminError(w, http.StatusServiceUnavailable, "FX_SERVICE_UNAVAILABLE", "fx-service-unavailable", nil)
		return
	}
	currency, err := dataplane.NormalizeCurrencyCode(chi.URLParam(r, "currency"))
	if err != nil || currency == "USD" {
		adminError(w, http.StatusBadRequest, "FX_RATE_INVALID", "invalid-fx-rate", nil)
		return
	}
	var body updateFXRateRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if body.RatePerUSD <= 0 {
		adminError(w, http.StatusBadRequest, "FX_RATE_INVALID", "invalid-fx-rate", nil)
		return
	}
	manual := true
	if body.ManualOverride != nil {
		manual = *body.ManualOverride
	}
	entry, err := d.FX.SetManualRate(r.Context(), currency, body.RatePerUSD, manual, principal.UserID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "FX_RATE_UPDATE_FAILED", "failed-to-update-fx-rate", nil, err.Error())
		return
	}
	_ = writeAuditEvent(r.Context(), d.Pool, principal.OrganizationID, principal.UserID,
		"fx_rate.updated", "fx_rate", currency, nil, map[string]any{
			"currency":        currency,
			"rate_per_usd":    entry.RatePerUSD,
			"manual_override": entry.ManualOverride,
		})
	adminOK(w, entry)
}

// SyncFXRates triggers an immediate authoritative FX rate pull (Frankfurter ECB + Open ER-API fallback).
func (d ServerDeps) SyncFXRates(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	if d.FX == nil {
		adminError(w, http.StatusServiceUnavailable, "FX_SERVICE_UNAVAILABLE", "fx-service-unavailable", nil)
		return
	}
	settings, rates, err := d.FX.SyncNow(r.Context())
	if err != nil {
		adminError(w, http.StatusBadGateway, "FX_SYNC_FAILED", "failed-to-sync-fx-rates", nil, err.Error())
		return
	}
	_ = writeAuditEvent(r.Context(), d.Pool, principal.OrganizationID, principal.UserID,
		"fx_rate.synced", "fx_rate", "sync", nil, map[string]any{
			"status": settings.LastSyncStatus,
			"source": settings.LastSyncSource,
		})
	adminOK(w, map[string]any{
		"settings": settings,
		"rates":    rates,
	})
}
