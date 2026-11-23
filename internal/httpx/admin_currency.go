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
		return "", errors.New("结算币种必须是三个字母的货币代码")
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return "", errors.New("结算币种必须是三个字母的货币代码")
		}
	}
	return value, nil
}

func normalizeCandidateCurrencies(ctx context.Context, q rowQuerier, candidates []createCandidateRequest) (string, error) {
	currency := ""
	for index := range candidates {
		candidateCurrency, err := normalizeCurrency(candidates[index].CostCurrency)
		if err != nil {
			return "", err
		}
		if candidates[index].CostCurrency == "" {
			var accountCurrency string
			if err := q.QueryRow(ctx, `SELECT billing_currency FROM provider_accounts WHERE id = $1`, candidates[index].ProviderAccountID).Scan(&accountCurrency); err != nil {
				return "", err
			}
			candidateCurrency = accountCurrency
		}
		if currency == "" {
			currency = candidateCurrency
		}
		if candidateCurrency != currency {
			return "", errors.New("同一模型服务的候选必须使用相同结算币种")
		}
		candidates[index].CostCurrency = candidateCurrency
	}
	if currency == "" {
		currency = "USD"
	}
	return currency, nil
}
