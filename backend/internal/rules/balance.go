package rules

import (
	"math"
	"strings"

	"vedhelper/internal/model"
	"vedhelper/internal/refdata"
)

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func accounts() []model.Account {
	var out []model.Account
	for _, a := range refdata.Accounts().Accounts {
		out = append(out, model.Account{ID: a.ID, IBAN: a.IBAN, Currency: a.Currency, Balance: a.Balance, Title: a.Title})
	}
	return out
}

func accountByID(id string) *model.Account {
	for _, a := range accounts() {
		if a.ID == id || (id != "" && a.IBAN == strings.ReplaceAll(id, " ", "")) {
			aa := a
			return &aa
		}
	}
	return nil
}

func accountForCurrency(cur string) *model.Account {
	for _, a := range accounts() {
		if a.Currency == cur {
			aa := a
			return &aa
		}
	}
	return nil
}

func kztFor(amount float64, cur string) float64 {
	return round2(amount * rate(cur) * (1 + spreadPct()/100))
}

func spreadStr() string { return strings.ReplaceAll(pyFloat(spreadPct()), ".", ",") }

// CheckBalance проверяет, хватит ли денег на счёте списания с учётом комиссии отправителя.
func CheckBalance(accountID string, amount, senderFees float64, currency string) model.BalanceCheck {
	acc := accountByID(accountID)
	needCur := round2(amount + senderFees)
	var available, needed float64
	acur := currency
	switch {
	case acc == nil:
		available, needed = 0, needCur
	case acc.Currency == currency:
		available, needed = acc.Balance, needCur
	default:
		acur = acc.Currency
		available = acc.Balance
		if acur == "KZT" {
			needed = kztFor(needCur, currency)
		} else {
			needed = Convert(needCur*(1+spreadPct()/100), currency, acur)
		}
	}
	ok := available >= needed
	shortfall := round2(math.Max(0, needed-available))
	options := []model.BalanceOption{}
	if !ok {
		kzt := accountForCurrency("KZT")
		shortCur := shortfall
		if acur != currency {
			shortCur = round2(shortfall * rate(acur) / rate(currency))
		}
		if kzt != nil && (acc == nil || kzt.ID != acc.ID) {
			kztNeed := kztFor(shortCur, currency)
			enough := kzt.Balance >= kztNeed
			tail := ", этого тоже не хватает."
			if enough {
				tail = "."
			}
			options = append(options, model.BalanceOption{Kind: "convert", FromAccountID: kzt.ID, Amount: kztNeed,
				Title: "Купить " + fmtMoney(shortCur, currency) + " за тенге",
				Detail: "С тенгового счёта спишут примерно " + fmtMoney(kztNeed, "KZT") + " по курсу " +
					fmtMoney(rate(currency), "KZT") + " за 1 " + currency + " плюс " + spreadStr() +
					"% за конвертацию. На тенговом счёте " + fmtMoney(kzt.Balance, "KZT") + tail})
		}
		if acc != nil && available > 0 && acc.Currency == currency {
			part := round2(math.Max(0, available-senderFees))
			if part > 0 {
				options = append(options, model.BalanceOption{Kind: "partial", FromAccountID: acc.ID, Amount: part,
					Title: "Оплатить часть: " + fmtMoney(part, currency),
					Detail: "Заплатить сейчас столько, сколько есть на счёте за вычетом комиссии, остаток доплатить " +
						"позже. Частичную оплату лучше заранее согласовать с поставщиком."})
			}
		}
	}
	return model.BalanceCheck{OK: ok, Currency: acur, Available: round2(available), Needed: needed, Shortfall: shortfall, Options: options}
}

// ContractBalance — сколько оплачено по договору и сколько осталось (без пересчёта валют).
func ContractBalance(total float64, p []model.Payment) (paid, remaining float64) {
	paid, rem := contractBalanceCur(&total, p, "")
	return paid, *rem
}

func contractBalanceCur(total *float64, payments []model.Payment, cur string) (float64, *float64) {
	paid := 0.0
	for _, p := range payments {
		amt := p.Amount
		if cur != "" && p.Currency != "" && p.Currency != cur {
			if v, ok := convertOK(amt, p.Currency, cur); ok {
				amt = v
			}
		}
		paid += amt
	}
	paid = round2(paid)
	if total == nil {
		return paid, nil
	}
	r := round2(*total - paid)
	return paid, &r
}
