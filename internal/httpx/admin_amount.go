package httpx

import (
	"fmt"
	"math/big"
)

// parseBudgetAmount 解析字符串金额为大数;失败返回错误。
func parseBudgetAmount(raw string) (*big.Float, error) {
	v, ok := new(big.Float).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("invalid amount %q", raw)
	}
	return v, nil
}

// amountCompare 比较两个字符串金额:a<b 返回 -1,a==b 返回 0,a>b 返回 1;非法值按 0 处理。
func amountCompare(a, b string) int {
	av, okA := new(big.Float).SetString(a)
	bv, okB := new(big.Float).SetString(b)
	if !okA {
		av = new(big.Float)
	}
	if !okB {
		bv = new(big.Float)
	}
	return av.Cmp(bv)
}


