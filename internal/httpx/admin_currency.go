package httpx

import (
	"context"
	"errors"
	"strings"
)

func normalizeCurrency(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "USD", nil
	}
	if len(value) != 3 {
		return "", errors.New("settlement currency must be a three-letter currency code")
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return "", errors.New("settlement currency must be a three-letter currency code")
		}
	}
	return value, nil
}

var errCandidateCurrencyMismatch = errors.New("candidate currency must match provider account settlement currency")

func normalizeCandidateCurrencies(ctx context.Context, q rowQuerier, candidates []createCandidateRequest) (string, error) {
	currency := ""
	accountCurrencies := map[string]string{}
	for index := range candidates {
		accountCurrency, ok := accountCurrencies[candidates[index].ProviderAccountID]
		if !ok {
			if err := q.QueryRow(ctx, `SELECT billing_currency FROM provider_accounts WHERE id = $1`, candidates[index].ProviderAccountID).Scan(&accountCurrency); err != nil {
				return "", err
			}
			accountCurrencies[candidates[index].ProviderAccountID] = accountCurrency
		}
		candidateCurrency := accountCurrency
		if candidates[index].CostCurrency != "" {
			normalized, err := normalizeCurrency(candidates[index].CostCurrency)
			if err != nil {
				return "", err
			}
			if normalized != accountCurrency {
				return "", errCandidateCurrencyMismatch
			}
			candidateCurrency = normalized
		}
		if currency == "" {
			currency = candidateCurrency
		}
		if candidateCurrency != currency {
			return "", errors.New("candidates for the same model service must use the same settlement currency")
		}
		candidates[index].CostCurrency = candidateCurrency
	}
	if currency == "" {
		currency = "USD"
	}
	return currency, nil
}
