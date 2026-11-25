package httpx

import (
	"fmt"
	"math/big"
	"regexp"
)

var budgetAmountPattern = regexp.MustCompile(`^(?:\d{1,12}(?:\.\d{1,8})?|\.\d{1,8})$`)

// parseBudgetAmount 解析字符串金额为大数;失败返回错误。
func parseBudgetAmount(raw string) (*big.Rat, error) {
	if !budgetAmountPattern.MatchString(raw) {
		return nil, fmt.Errorf("invalid amount %q", raw)
	}
	v, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("invalid amount %q", raw)
	}
	return v, nil
}

// amountCompare 比较两个字符串金额:a<b 返回 -1,a==b 返回 0,a>b 返回 1;非法值按 0 处理。
func amountCompare(a, b string) int {
	av, errA := parseBudgetAmount(a)
	bv, errB := parseBudgetAmount(b)
	if errA != nil {
		av = new(big.Rat)
	}
	if errB != nil {
		bv = new(big.Rat)
	}
	return av.Cmp(bv)
}
