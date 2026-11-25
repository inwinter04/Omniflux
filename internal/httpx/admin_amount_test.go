package httpx

import "testing"

func TestAmountComparePreservesDecimalPrecision(t *testing.T) {
	if got := amountCompare("999999999999.00000001", "999999999999.00000002"); got != -1 {
		t.Fatalf("amountCompare() = %d, want -1", got)
	}
}

func TestParseBudgetAmountRejectsNonDecimalValues(t *testing.T) {
	for _, raw := range []string{"0x10", "+Inf", "1.000000001", "1000000000000"} {
		if _, err := parseBudgetAmount(raw); err == nil {
			t.Fatalf("parseBudgetAmount(%q) succeeded, want error", raw)
		}
	}
}
