package rules

import (
	du "vedhelper/internal/docutil"
	"vedhelper/internal/refdata"
)

// ParseAmount: '51,000.00', '51 000,00', 'USD 51,000.00', '51.000,00', '51000' -> 51000.
func ParseAmount(s string) (float64, bool) { return du.ParseAmount(s) }

func rateOf(cur string) (float64, bool) {
	r, ok := refdata.Rates().Rates[cur]
	return r, ok
}

func rate(cur string) float64 { r, _ := rateOf(cur); return r }

func convertOK(amount float64, from, to string) (float64, bool) {
	if from == to {
		return round2(amount), true
	}
	rf, ok1 := rateOf(from)
	rt, ok2 := rateOf(to)
	if !ok1 || !ok2 || rt == 0 {
		return 0, false
	}
	return round2(amount * rf / rt), true
}

// Convert пересчитывает через тенге по rates.json; для неизвестной валюты 0.
func Convert(amount float64, from, to string) float64 {
	v, _ := convertOK(amount, from, to)
	return v
}

func spreadPct() float64 { return refdata.Rates().ConversionSpreadPct }
