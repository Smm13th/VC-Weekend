package rules

import (
	"math"

	"vedhelper/internal/model"
	"vedhelper/internal/refdata"
)

func tariff(table map[string]float64, cur string) float64 {
	if v, ok := table[cur]; ok {
		return v
	}
	if v, ok := convertOK(table["USD"], "USD", cur); ok {
		return v
	}
	return table["USD"]
}

func senderFee(amount float64, cur string) float64 {
	t := refdata.TariffsData().SenderFee
	fee := amount * t.Pct / 100
	lo, hi := tariff(t.Min, cur), tariff(t.Max, cur)
	return round2(math.Min(math.Max(fee, lo), hi))
}

// SimulateFees считает комиссии OUR / SHA / BEN по tariffs.json.
func SimulateFees(amount float64, currency, feeType string, urgent bool) model.FeeSim {
	t := refdata.TariffsData()
	amount = round2(amount)
	ft := upper(feeType)
	if ft == "" {
		ft = "OUR"
	}
	sf := 0.0
	if amount > 0 {
		sf = senderFee(amount, currency)
	}
	pct := t.SenderFee.Pct
	lo, hi := tariff(t.SenderFee.Min, currency), tariff(t.SenderFee.Max, currency)
	sfLabel := "Комиссия нашего банка за перевод: " + fmtNum(pct, 1) + "% от суммы, не меньше " + fmtNum(lo, 0) +
		" и не больше " + fmtNum(hi, 0) + " " + currency
	inter := tariff(t.IntermediaryFee, currency)
	benf := tariff(t.BeneficiaryBankFee, currency)
	guar := tariff(t.OurGuaranteeFee, currency)
	var lines []model.FeeLine
	var senderFees, withheld float64
	switch ft {
	case "OUR":
		lines = append(lines,
			model.FeeLine{Who: "sender", Label: sfLabel, Amount: sf},
			model.FeeLine{Who: "sender", Label: "Доплата за гарантию полной суммы: комиссии банка-посредника и банка " +
				"получателя оплачиваете вы, поставщик получит всю сумму", Amount: guar})
		senderFees, withheld = sf+guar, 0
	case "SHA":
		lines = append(lines,
			model.FeeLine{Who: "sender", Label: sfLabel, Amount: sf},
			model.FeeLine{Who: "beneficiary", Label: "Комиссия банка-посредника, её удержат из суммы перевода", Amount: inter},
			model.FeeLine{Who: "beneficiary", Label: "Комиссия банка получателя, её удержат из суммы перевода", Amount: benf})
		senderFees, withheld = sf, inter+benf
	default:
		ft = "BEN"
		lines = append(lines,
			model.FeeLine{Who: "beneficiary", Label: "Комиссия нашего банка, её удержат из суммы перевода", Amount: sf},
			model.FeeLine{Who: "beneficiary", Label: "Комиссия банка-посредника, её удержат из суммы перевода", Amount: inter},
			model.FeeLine{Who: "beneficiary", Label: "Комиссия банка получателя, её удержат из суммы перевода", Amount: benf})
		senderFees, withheld = 0, sf+inter+benf
	}
	withheld = round2(math.Min(withheld, amount))
	senderFees = round2(senderFees)
	urg := 0.0
	if urgent {
		urg = t.UrgentFeeKzt
	}
	return model.FeeSim{FeeType: ft, Currency: currency, SenderTotal: round2(amount + senderFees), SenderFees: senderFees,
		ReceiverGets: round2(amount - withheld), Shortfall: withheld, UrgentFeeKZT: urg, Lines: lines}
}
