package rules

import (
	"strings"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/extract"
	"vedhelper/internal/model"
	"vedhelper/internal/refdata"
)

const (
	feesEN    = "Bank charges of the Buyer's bank shall be borne by the Buyer; all other bank charges shall be borne by the Seller."
	feesRU    = "Комиссии банка Покупателя оплачивает Покупатель, все прочие банковские комиссии оплачивает Продавец."
	termEN    = "The Buyer shall pay each Invoice within 10 (ten) banking days from the date of the Invoice."
	termRU    = "Покупатель оплачивает каждый Инвойс в течение 10 (десяти) банковских дней с даты Инвойса."
	payDateEN = "The date of payment is the date of debiting the Buyer's account."
	payDateRU = "Датой оплаты считается дата списания денег со счёта Покупателя."
)

func thresholdUSD() float64 {
	if v := refdata.TariffsData().AccountingNumberThresholdUsd; v > 0 {
		return v
	}
	return 50000
}

func fval(ext *model.ExtractionResult, key string) (model.Extracted, bool) {
	if ext == nil {
		return model.Extracted{}, false
	}
	v, ok := ext.Fields[key]
	return v, ok
}

func normStr(e model.Extracted) string {
	if s, ok := e.Norm.(string); ok {
		return s
	}
	return ""
}

func normFloat(e model.Extracted) (float64, bool) {
	f, ok := e.Norm.(float64)
	return f, ok
}

func contractSummary(ext *model.ExtractionResult, payments []model.Payment) model.ContractSummary {
	s := model.ContractSummary{ThresholdUSD: thresholdUSD()}
	if ext == nil {
		return s
	}
	if v, ok := fval(ext, "contractNo"); ok {
		s.ContractNo = v.Value
	}
	if v, ok := fval(ext, "contractDate"); ok {
		s.ContractDate = normStr(v)
	}
	if v, ok := fval(ext, "currency"); ok {
		s.Currency = normStr(v)
	}
	if v, ok := fval(ext, "totalAmount"); ok {
		if t, ok := normFloat(v); ok {
			s.Total = &t
			cur := s.Currency
			if cur == "" {
				cur = "USD"
			}
			if u, ok := convertOK(t, cur, "USD"); ok {
				s.TotalUSD = &u
			}
		}
	}
	s.Paid, s.Remaining = contractBalanceCur(s.Total, payments, s.Currency)
	s.NeedsAccountingNumber = s.TotalUSD != nil && *s.TotalUSD > 0 && *s.TotalUSD > s.ThresholdUSD
	return s
}

func conversionNote(s model.ContractSummary) string {
	if s.Total == nil || *s.Total == 0 || s.Currency == "" {
		return ""
	}
	if s.Currency == "USD" {
		return "Сумма договора " + fmtMoney(*s.Total, "USD") + " больше порога " + fmtMoneyD(s.ThresholdUSD, "USD", 0) + "."
	}
	r := rate(s.Currency) / rate("USD")
	usd := 0.0
	if s.TotalUSD != nil {
		usd = *s.TotalUSD
	}
	return "Договор в другой валюте, поэтому сумму пересчитали: " + fmtMoneyD(*s.Total, s.Currency, 0) + " это примерно " +
		fmtMoneyD(usd, "USD", 0) + " по курсу " + strings.TrimSpace(fmtMoneyD(r, "", 4)) + " USD за 1 " + s.Currency +
		" (через тенге, курсы на " + fmtDate(refdata.Rates().Date) + "). Это больше порога " + fmtMoneyD(s.ThresholdUSD, "USD", 0) + "."
}

func accNumberContractFinding(d *du.Doc, ext *model.ExtractionResult, s model.ContractSummary) *model.Finding {
	if !s.NeedsAccountingNumber {
		return nil
	}
	var src *model.SourceRef
	if v, ok := fval(ext, "totalAmount"); ok {
		src = srcp(v.Source)
	}
	f := newFinding("acc_number_required", d.ID, model.StageContract, model.Yellow, "До первого платежа нужно получить учётный номер контракта",
		conversionNote(s)+" Такой валютный договор нужно поставить на учёт в банке и получить учётный "+
			"номер контракта до первого платежа, иначе перевод не пропустит валютный контроль. Порог и правила "+
			"стоит сверить с менторами.", []*model.SourceRef{src}, fopt{field: "linkContract", mentorCheck: true})
	return &f
}

func spanRef(d *du.Doc, r *model.SourceRef, s, e int) *model.SourceRef {
	return srcp(d.Ref(r.Start+s, r.Start+e))
}

func consentRegexEN(p string) string {
	return `\bThe\s+(?P<p>` + p + `)\s+shall\s+not\b[^.;]*?\bwithout\s+(?:the\s+)?(?:prior\s+)?(?:written\s+)?` +
		`(?:consent\s+of\s+the\s+` + p + `|` + p + `'s\s+(?:prior\s+)?(?:written\s+)?consent)`
}

func consentRegexRU(p string) string {
	return `(?P<p>` + p + `)\w*\s+не\s+[^.;]*?без\s+[^.;]*?согласия\s+` + p + `\w*`
}

// leftmost — самое левое совпадение из нескольких шаблонов (эмуляция обратной ссылки (?P=p)).
func leftmost(text string, pats ...string) ([]int, string) {
	var best []int
	bestP := ""
	for _, p := range pats {
		m := du.Find(rxi(p), text)
		if m != nil && (best == nil || m[0] < best[0]) {
			best, bestP = m, p
		}
	}
	return best, bestP
}

func clauseFindings(d *du.Doc, pairs []alignedPair) []model.Finding {
	var out []model.Finding
	text := func(p *model.ClausePair, lang string) string {
		if lang == "en" {
			return quoteOf(p.EN)
		}
		return quoteOf(p.RU)
	}
	// согласие не той стороны
	for i := range pairs {
		p := pairs[i].p
		tEN, tRU := text(p, "en"), text(p, "ru")
		me, pe := leftmost(tEN, consentRegexEN("Seller"), consentRegexEN("Buyer"))
		mr, pr := leftmost(tRU, consentRegexRU("Продав"), consentRegexRU("Покупат"))
		if me == nil && mr == nil {
			continue
		}
		var party string
		if me != nil {
			a, b := du.Group(rxi(pe), me, "p")
			party = capitalize(du.RuneSub(tEN, a, b))
		} else {
			a, b := du.Group(rxi(pr), mr, "p")
			if strings.HasPrefix(strings.ToLower(du.RuneSub(tRU, a, b)), "продав") {
				party = "Seller"
			} else {
				party = "Buyer"
			}
		}
		other := "Seller"
		ruParty, ruOther := "Покупателя", "Продавца"
		if party == "Seller" {
			other = "Buyer"
			ruParty, ruOther = "Продавца", "Покупателя"
		}
		enT, _ := body(p.EN)
		ruT, _ := body(p.RU)
		enFix := rx(`\b`+party+`'s(\s+(?:prior\s+)?(?:written\s+)?consent)`).ReplaceAllString(enT, other+"'s${1}")
		enFix = rx(`(consent\s+of\s+the\s+)`+party).ReplaceAllString(enFix, "${1}"+other)
		ruFix := rx(`(согласия\s+)`+ruParty).ReplaceAllString(ruT, "${1}"+ruOther)
		var srcs []*model.SourceRef
		if me != nil {
			whole := du.RuneSub(tEN, me[0], me[1])
			if sub := du.Find(rx(`without[^.;]*consent(?:\s+of\s+the\s+\w+)?`), whole); sub != nil {
				srcs = append(srcs, spanRef(d, p.EN, me[0]+sub[0], me[0]+sub[1]))
			} else {
				srcs = append(srcs, spanRef(d, p.EN, me[0], me[1]))
			}
		}
		if mr != nil {
			whole := du.RuneSub(tRU, mr[0], mr[1])
			if sub := du.Find(rx(`согласия\s+\w+`), whole); sub != nil {
				srcs = append(srcs, spanRef(d, p.RU, mr[0]+sub[0], mr[0]+sub[1]))
			} else {
				srcs = append(srcs, spanRef(d, p.RU, mr[0], mr[1]))
			}
		}
		who := "Покупатель"
		if party == "Seller" {
			who = "Продавец"
		}
		out = append(out, newFinding("clause_consent_party", d.ID, model.StageContract, model.Yellow,
			"Пункт "+p.Num+": согласие не той стороны",
			"По пункту "+p.Num+" "+who+" может раскрыть чужую информацию, если сам на это согласится. Такое условие "+
				"ничего не защищает. Согласие должна давать сторона, чья это информация, то есть "+strings.ToLower(ruOther)+".",
			srcs, fopt{extra: p.Num, suggestion: &model.Suggestion{Clause: p.Num, EN: enFix, RU: ruFix}}))
		addIssue(p, "clause_consent_party")
	}
	// авиадоставка и коносамент
	for i := range pairs {
		p := pairs[i].p
		tEN, tRU := text(p, "en"), text(p, "ru")
		air := rxi(`\bby\s+air\b|\bair\s+(delivery|transport|freight)`).MatchString(tEN) || rxi(`авиа`).MatchString(tRU)
		blEN := du.Find(rxi(`Bill\s+of\s+Lading`), tEN)
		blRU := du.Find(rxi(`коносамент\w*(?:\s*\(Bill\s+of\s+Lading\))?`), tRU)
		if !(air && (blEN != nil || blRU != nil)) {
			continue
		}
		enT, _ := body(p.EN)
		ruT, _ := body(p.RU)
		var srcs []*model.SourceRef
		if blEN != nil {
			srcs = append(srcs, spanRef(d, p.EN, blEN[0], blEN[1]))
		}
		if blRU != nil {
			srcs = append(srcs, spanRef(d, p.RU, blRU[0], blRU[1]))
		}
		out = append(out, newFinding("clause_air_waybill", d.ID, model.StageContract, model.Yellow,
			"Пункт "+p.Num+": для авиадоставки указан морской документ",
			"В пункте "+p.Num+" датой авиадоставки считается дата коносамента (Bill of Lading). "+
				"Коносамент выдают при морской перевозке, для самолёта оформляют авиагрузовую накладную "+
				"(Air Waybill). С таким пунктом сложно доказать дату поставки, а она нужна для валютного "+
				"контроля и гарантии.", srcs, fopt{extra: p.Num, suggestion: &model.Suggestion{Clause: p.Num,
				EN: rx(`Bill\s+of\s+Lading`).ReplaceAllString(enT, "Air Waybill"),
				RU: rx(`коносамента\s*\(Bill\s+of\s+Lading\)`).ReplaceAllString(ruT, "авиагрузовой накладной (Air Waybill)")}}))
		addIssue(p, "clause_air_waybill")
	}
	// комиссии
	feeRx := rxi(`bank\s+(charges|commissions?|fees)|комисси`)
	var art4 []*model.ClausePair
	for i := range pairs {
		if strings.HasPrefix(pairs[i].p.Num, "4.") {
			art4 = append(art4, pairs[i].p)
		}
	}
	anyFee := false
	for i := range pairs {
		if feeRx.MatchString(text(pairs[i].p, "en")) || feeRx.MatchString(text(pairs[i].p, "ru")) {
			anyFee = true
		}
	}
	if len(pairs) > 0 && !anyFee {
		nxt := "4.7"
		var anchor *model.ClausePair
		if len(art4) > 0 {
			mx := 0
			for _, p := range art4 {
				if k := atoiSafe(strings.SplitN(p.Num, ".", 2)[1]); k > mx {
					mx = k
				}
			}
			nxt = "4." + itoa(mx+1)
			anchor = art4[len(art4)-1]
		} else {
			anchor = pairs[0].p
		}
		out = append(out, newFinding("fees_not_specified", d.ID, model.StageContract, model.Yellow, "Не сказано, кто платит банковские комиссии",
			"В договоре нет пункта о банковских комиссиях. Если при переводе выбрать тип комиссии SHA "+
				"или BEN, банки по дороге удержат часть денег и продавец получит меньше суммы инвойса. "+
				"Формально это недоплата, и продавец может потребовать доплатить. Безопаснее добавить "+
				"пункт "+nxt+" или до договорённости платить с типом OUR.",
			[]*model.SourceRef{anchor.EN, anchor.RU}, fopt{field: "feeType", suggestion: &model.Suggestion{Clause: nxt, EN: feesEN, RU: feesRU}}))
	}
	// срок оплаты в статье 4
	termRx := rxi(`(within|not\s+later\s+than|no\s+later\s+than)\s+\d+|в\s+течение\s+\d+|не\s+позднее\s+\d+`)
	payRx := rxi(`\bpa(y|id|yment)\b|оплат`)
	if len(art4) > 0 {
		has := false
		for _, p := range art4 {
			for _, t := range []string{text(p, "en"), text(p, "ru")} {
				if termRx.MatchString(t) && payRx.MatchString(t) {
					has = true
				}
			}
		}
		if !has {
			target := art4[0]
			for _, p := range art4 {
				if rx(`\bshall\s+pay\b|оплачивает`).MatchString(text(p, "en") + text(p, "ru")) {
					target = p
					break
				}
			}
			out = append(out, newFinding("payment_term_missing", d.ID, model.StageContract, model.Yellow, "В договоре нет точного срока оплаты",
				"В статье 4 срок оплаты не назван, пункт "+target.Num+" отсылает к инвойсу. Тогда срок может "+
					"поменяться от инвойса к инвойсу, а без срока трудно понять, когда платёж просрочен и когда "+
					"по нему ждать товар. Попросите записать срок в днях прямо в договоре.",
				[]*model.SourceRef{target.EN, target.RU}, fopt{extra: target.Num,
					suggestion: &model.Suggestion{Clause: target.Num, EN: termEN, RU: termRU}}))
			addIssue(target, "payment_term_missing")
		}
	}
	out = append(out, crossReferenceFindings(d, pairs)...)
	out = append(out, duplicateFindings(d, pairs)...)
	return out
}

// CheckContract проверяет договор: плейсхолдеры, скрытые инструкции, двуязычие, дефекты пунктов, учётный номер, реквизиты.
func CheckContract(doc model.ParsedDoc) model.ContractReport {
	ext := extract.Fields(doc)
	return checkContract(du.New(doc), &ext)
}

func checkContract(d *du.Doc, ext *model.ExtractionResult) model.ContractReport {
	pairs := alignDoc(d)
	findings := []model.Finding{}
	for _, f := range []*model.Finding{placeholderFinding(d, model.StageContract), injectionFinding(d, model.StageContract)} {
		if f != nil {
			findings = append(findings, *f)
		}
	}
	findings = append(findings, bilingualFindings(d, pairs, model.StageContract)...)
	findings = append(findings, clauseFindings(d, pairs)...)
	s := contractSummary(ext, nil)
	if f := accNumberContractFinding(d, ext, s); f != nil {
		findings = append(findings, *f)
	}
	findings = append(findings, requisitesFindings(d, *ext, model.StageContract, "")...)
	clauses := []model.ClausePair{}
	for _, a := range pairs {
		clauses = append(clauses, *a.p)
	}
	return model.ContractReport{Findings: findings, Clauses: clauses, Extraction: *ext}
}
