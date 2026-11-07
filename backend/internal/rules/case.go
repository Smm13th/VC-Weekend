package rules

import (
	"sort"
	"strings"
	"time"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/extract"
	"vedhelper/internal/model"
)

var sevOrder = map[model.Severity]int{model.Red: 0, model.Yellow: 1, model.Green: 2}
var stageOrder = map[model.Stage]int{model.StageContract: 0, model.StageDocuments: 1, model.StagePayment: 2}

func extNorm(ext *model.ExtractionResult, key string) string {
	if v, ok := fval(ext, key); ok {
		return normStr(v)
	}
	return ""
}

func primaryInvoice(docs []*du.Doc, exts map[string]*model.ExtractionResult) *du.Doc {
	var best *du.Doc
	bestKey := ""
	for _, d := range docs {
		if d.Kind != model.KindInvoice {
			continue
		}
		k := extNorm(exts[d.ID], "invoiceDate")
		if best == nil || k >= bestKey {
			best, bestKey = d, k
		}
	}
	return best
}

func clauseRef(d *du.Doc, pattern string) *model.SourceRef {
	if d == nil {
		return nil
	}
	re := rxi(pattern)
	for _, a := range alignDoc(d) {
		for _, r := range []*model.SourceRef{a.p.EN, a.p.RU} {
			if r != nil && re.MatchString(r.Quote) {
				return r
			}
		}
	}
	return nil
}

func hasAutoextension(d *du.Doc) bool {
	return d != nil && rxi(`automatically\s+(extended|renewed|prolonged)|автоматически\s+(продлева|пролонгир)`).MatchString(d.Text)
}

func idTail(id string) string {
	if i := strings.Index(id, ":"); i >= 0 {
		return id[i+1:]
	}
	return ""
}

func crossDocFindings(d *du.Doc, ext *model.ExtractionResult, contract *du.Doc, cExt *model.ExtractionResult) []model.Finding {
	var out []model.Finding
	if contract == nil || cExt == nil {
		return out
	}
	lbl := docLabel(d.ParsedDoc)
	cNo, hasCNo := fval(cExt, "contractNo")
	cDate, hasCDate := fval(cExt, "contractDate")
	cTotal, hasCTotal := fval(cExt, "totalAmount")
	cCur, hasCCur := fval(cExt, "currency")
	amt, hasAmt := fval(ext, "invoiceAmount")
	cur, hasCur := fval(ext, "currency")
	if d.Kind == model.KindInvoice {
		a, okA := normFloat(amt)
		t, okT := normFloat(cTotal)
		if hasCur && hasCCur && normStr(cur) != normStr(cCur) {
			out = append(out, newFinding("currency_mismatch", d.ID, model.StageDocuments, model.Red, "Валюта инвойса не совпадает с договором",
				"Инвойс выставлен в "+normStr(cur)+", а договор заключён в "+normStr(cCur)+". Платёж в другой "+
					"валюте не закроет обязательства по договору без дополнительного соглашения. Попросите "+
					"поставщика переделать инвойс или подписать допсоглашение о валюте.",
				[]*model.SourceRef{srcp(cur.Source), srcp(cCur.Source)}, fopt{field: "amount"}))
		} else if hasAmt && hasCTotal && okA && okT && a > t {
			cu, ccu := "", ""
			if hasCur {
				cu = normStr(cur)
			}
			if hasCCur {
				ccu = normStr(cCur)
			}
			out = append(out, newFinding("amount_exceeds_contract", d.ID, model.StageDocuments, model.Red, "Инвойс больше суммы договора",
				"В инвойсе "+fmtMoney(a, cu)+", а весь договор на "+fmtMoney(t, ccu)+". Заплатить больше суммы договора "+
					"нельзя без дополнительного соглашения об увеличении суммы.",
				[]*model.SourceRef{srcp(amt.Source), srcp(cTotal.Source)}, fopt{field: "amount"}))
		}
		if iDate, ok := fval(ext, "invoiceDate"); ok && hasCDate && normStr(iDate) != "" && normStr(cDate) != "" && normStr(iDate) < normStr(cDate) {
			out = append(out, newFinding("invoice_before_contract", d.ID, model.StageDocuments, model.Yellow, "Инвойс выписан раньше договора",
				"Инвойс датирован "+iDate.Value+", а договор подписан "+cDate.Value+". Банк может спросить, "+
					"на каком основании выставлен счёт до договора. Уточните у поставщика дату или основание.",
				[]*model.SourceRef{srcp(iDate.Source), srcp(cDate.Source)}, fopt{field: "docDate"}))
		}
	}
	// ссылка на договор
	cref, hasRef := fval(ext, "invoiceContractRef")
	if (d.Kind == model.KindInvoice || d.Kind == model.KindLetter) && hasCNo {
		if !hasRef {
			dt := "?"
			if hasCDate {
				dt = cDate.Value
			}
			out = append(out, newFinding("invoice_contract_ref", d.ID, model.StageDocuments, model.Yellow, "В "+lbl+" нет ссылки на договор",
				"В "+lbl+" не указано, к какому договору он относится. Без ссылки на договор "+cNo.Value+" от "+dt+
					" банк может не принять документ как основание платежа. Попросите добавить номер и дату договора.",
				[]*model.SourceRef{srcp(cNo.Source)}, fopt{field: "linkContract"}))
		} else {
			no, dt, _ := strings.Cut(normStr(cref), "|")
			badNo := du.NormName(no) != du.NormName(cNo.Value)
			badDt := dt != "" && hasCDate && dt != normStr(cDate)
			if badNo || badDt {
				var what []string
				if badNo {
					what = append(what, "номер "+no+" вместо "+cNo.Value)
				}
				if badDt {
					what = append(what, "дата "+fmtDate(dt)+" вместо "+cDate.Value)
				}
				out = append(out, newFinding("invoice_contract_ref", d.ID, model.StageDocuments, model.Yellow,
					capitalize(docTitle(d.ParsedDoc))+" ссылается на другой договор",
					"В "+lbl+" в ссылке на договор указаны "+strings.Join(what, " и ")+". Возможно, это опечатка "+
						"или документ от другого договора. Уточните у поставщика и попросите исправить.",
					[]*model.SourceRef{srcp(cref.Source), srcp(cNo.Source)}, fopt{field: "linkContract"}))
			}
		}
	}
	// смена реквизитов
	ib, hasIB := fval(ext, "sellerIban")
	cIB, hasCIB := fval(cExt, "sellerIban")
	sw, hasSW := fval(ext, "sellerSwift")
	cSW, hasCSW := fval(cExt, "sellerSwift")
	ibanDiff := hasIB && hasCIB && normalizeIBAN(ib.Value) != normalizeIBAN(cIB.Value)
	swiftDiff := hasSW && hasCSW && first8(normalizeSwift(sw.Value)) != first8(normalizeSwift(cSW.Value))
	typo := false
	if ibanDiff {
		v := ValidateIBAN(ib.Value, "")
		typo = v.Error == "checksum" && looksLikeTypo(ib.Value, cIB.Value)
	}
	if (ibanDiff && !typo) || swiftDiff {
		bank, hasBank := fval(ext, "sellerBank")
		c4 := clauseRef(contract, `change\s+of\s+bank\s+details|изменени\w+\s+банковских\s+реквизитов`)
		cBank, hasCBank := fval(cExt, "sellerBank")
		var newS, oldS []string
		if ibanDiff {
			newS = append(newS, "счёт "+FormatIBAN(ib.Value))
		}
		if hasBank {
			newS = append(newS, "в банке "+bank.Value)
		}
		if hasSW {
			newS = append(newS, "(SWIFT "+normalizeSwift(sw.Value)+")")
		}
		if hasCIB {
			oldS = append(oldS, "счёт "+FormatIBAN(cIB.Value))
		}
		if hasCBank {
			oldS = append(oldS, "в банке "+cBank.Value)
		}
		if hasCSW {
			oldS = append(oldS, "(SWIFT "+normalizeSwift(cSW.Value)+")")
		}
		n46 := "4.6"
		if c4 != nil {
			if m := rx(`^\s*(\d+\.\d+)`).FindStringSubmatch(c4.Quote); m != nil {
				n46 = m[1]
			}
		}
		s1, s2 := srcp(sw.Source), srcp(cSW.Source)
		if ibanDiff {
			s1, s2 = srcp(ib.Source), srcp(cIB.Source)
		}
		out = append(out, newFinding("requisites_changed", d.ID, model.StageDocuments, model.Red, "В "+lbl+" другие реквизиты продавца",
			"В "+lbl+" указан "+strings.Join(newS, " ")+". В статье 13 договора другие реквизиты: "+strings.Join(oldS, " ")+". "+
				"По пункту "+n46+" договора об изменении реквизитов стороны сообщают заранее. Подмена счёта в "+
				"письме или инвойсе встречается часто, так работают мошенники. Перезвоните поставщику по телефону из "+
				"договора, а не из письма, и спросите, меняли ли они банк. Пока не подтвердили, не платите "+
				"на новый счёт.", []*model.SourceRef{s1, s2, c4}, fopt{field: "beneficiaryIban"}))
	} else if hasIB && hasCIB && !ibanDiff && (!hasSW || !hasCSW || !swiftDiff) {
		and := ""
		if hasSW && hasCSW {
			and = " и SWIFT"
		}
		out = append(out, newFinding("requisites_changed", d.ID, model.StageDocuments, model.Green, "Реквизиты в "+lbl+" совпадают с договором",
			"IBAN"+and+" в "+lbl+" такие же, как в статье 13 договора.",
			[]*model.SourceRef{srcp(ib.Source), srcp(cIB.Source)}, fopt{field: "beneficiaryIban"}))
	}
	// имя получателя
	if nm, ok := fval(ext, "sellerName"); ok {
		var cNames []model.Extracted
		for _, k := range []string{"sellerName", "sellerAccountName"} {
			if v, ok := fval(cExt, k); ok {
				cNames = append(cNames, v)
			}
		}
		all := len(cNames) > 0
		for _, c := range cNames {
			if du.NormName(nm.Value) == du.NormName(c.Value) {
				all = false
			}
		}
		if all {
			out = append(out, newFinding("beneficiary_name_mismatch", d.ID, model.StageDocuments, model.Red,
				"Получатель в "+lbl+" назван иначе, чем в договоре",
				"В "+lbl+" получатель «"+nm.Value+"», а в договоре продавец «"+cNames[0].Value+"». Банк получателя "+
					"сверяет название с владельцем счёта и может вернуть перевод. Другое название может значить и "+
					"другую компанию. Уточните у поставщика и платите только компании из договора.",
				[]*model.SourceRef{srcp(nm.Source), srcp(cNames[0].Source)}, fopt{field: "beneficiaryName"}))
		}
	}
	return out
}

func first8(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func amountToPay(c model.CaseInput, invExt *model.ExtractionResult) (float64, bool) {
	if st, ok := c.Form["amount"]; ok && st.Status == "user" {
		if v, ok := ParseAmount(st.Value); ok && v != 0 {
			return v, true
		}
	}
	if a, ok := fval(invExt, "invoiceAmount"); ok {
		return normFloat(a)
	}
	return 0, false
}

// CheckPayment проверяет кейс платежа: все документы, сверка с договором, форма, комиссии, остаток.
func CheckPayment(c model.CaseInput) model.PaymentReport {
	today := todayISO(c.Today)
	var now time.Time
	if parseDate(c.Today) == "" {
		now = time.Now()
	}
	var docs []*du.Doc
	exts := map[string]*model.ExtractionResult{}
	var extractions []model.ExtractionResult
	for _, pd := range c.Docs {
		d := du.New(pd)
		docs = append(docs, d)
		e := extract.Fields(pd)
		exts[d.ID] = &e
		extractions = append(extractions, e)
	}
	var contract *du.Doc
	for _, d := range docs {
		if d.Kind == model.KindContract {
			contract = d
			break
		}
	}
	var cExt *model.ExtractionResult
	if contract != nil {
		cExt = exts[contract.ID]
	}
	findings := []model.Finding{}
	if contract != nil {
		for _, f := range checkContract(contract, cExt).Findings {
			// Учётный номер уже получен: напоминание стадии договора больше не нужно, остаётся зелёная отметка стадии заявки.
			if f.Rule == "acc_number_required" && strings.TrimSpace(c.AccountingNumber) != "" {
				continue
			}
			findings = append(findings, f)
		}
	}
	summary := contractSummary(cExt, c.Payments)
	refIBAN := ""
	if v, ok := fval(cExt, "sellerIban"); ok {
		refIBAN = v.Value
	}
	for _, d := range docs {
		if d == contract {
			continue
		}
		ext := exts[d.ID]
		for _, f := range []*model.Finding{placeholderFinding(d, model.StageDocuments), injectionFinding(d, model.StageDocuments)} {
			if f != nil {
				findings = append(findings, *f)
			}
		}
		findings = append(findings, requisitesFindings(d, *ext, model.StageDocuments, refIBAN)...)
		findings = append(findings, crossDocFindings(d, ext, contract, cExt)...)
	}
	// у документа с подменой реквизитов зелёные отметки о формате счёта сбивают с толку
	changed := map[string]bool{}
	for _, f := range findings {
		if f.Rule == "requisites_changed" && f.Severity == model.Red {
			changed[idTail(f.ID)] = true
		}
	}
	kept := findings[:0:0]
	for _, f := range findings {
		if f.Severity == model.Green && (f.Rule == "iban_checksum" || f.Rule == "swift_format") && changed[idTail(f.ID)] {
			continue
		}
		kept = append(kept, f)
	}
	findings = kept

	inv := primaryInvoice(docs, exts)
	var invExt *model.ExtractionResult
	payID := "case"
	if inv != nil {
		invExt = exts[inv.ID]
		payID = inv.ID
	} else if contract != nil {
		payID = contract.ID
	}
	currency := extNorm(invExt, "currency")
	if currency == "" {
		currency = summary.Currency
	}
	if currency == "" {
		currency = "USD"
	}
	amount, hasAmount := amountToPay(c, invExt)

	cTotalSrc := func() *model.SourceRef {
		if v, ok := fval(cExt, "totalAmount"); ok {
			return srcp(v.Source)
		}
		return nil
	}
	if summary.Total != nil && hasAmount && summary.Remaining != nil && amount > *summary.Remaining+0.005 {
		var aSrc *model.SourceRef
		if a, ok := fval(invExt, "invoiceAmount"); ok {
			aSrc = srcp(a.Source)
		}
		rem := *summary.Remaining
		findings = append(findings, newFinding("over_remaining", payID, model.StagePayment, model.Red, "Сумма больше остатка по договору",
			"Договор на "+fmtMoney(*summary.Total, summary.Currency)+", уже оплачено "+fmtMoney(summary.Paid, summary.Currency)+
				", остаток "+fmtMoney(rem, summary.Currency)+". К оплате "+fmtMoney(amount, currency)+", это на "+
				fmtMoney(amount-rem, currency)+" больше. Такой платёж выйдет за сумму договора: нужно дополнительное соглашение об "+
				"увеличении суммы или оплатить только остаток.", []*model.SourceRef{aSrc, cTotalSrc()}, fopt{field: "amount"}))
	}
	if summary.NeedsAccountingNumber && contract != nil {
		src := cTotalSrc()
		if strings.TrimSpace(c.AccountingNumber) == "" {
			findings = append(findings, newFinding("acc_number_required", payID, model.StagePayment, model.Red, "Нет учётного номера контракта",
				conversionNote(summary)+" Без учётного номера контракта банк не проведёт платёж по этому "+
					"договору. Поставьте договор на учёт в банке, получите учётный номер контракта и укажите его в "+
					"поле «Привязать валютный договор». Порог стоит сверить с менторами.",
				[]*model.SourceRef{src}, fopt{field: "linkContract", mentorCheck: true}))
		} else {
			findings = append(findings, newFinding("acc_number_required", payID, model.StagePayment, model.Green, "Учётный номер контракта указан",
				"Договор стоит на учёте, учётный номер контракта "+c.AccountingNumber+". Он попадёт в "+
					"привязку договора и в назначение платежа.", []*model.SourceRef{src}, fopt{field: "linkContract", mentorCheck: true}))
		}
	}

	form := BuildForm(c, extractions, findings, summary)

	vd := parseDate(form["valueDate"].Value)
	if vd == "" {
		vd = parseDate(c.ValueDate)
	}
	if exp, ok := fval(cExt, "expiryDate"); ok && normStr(exp) != "" && vd != "" && normStr(exp) < vd {
		c31 := clauseRef(contract, `valid\s+until|действует\s+до`)
		if hasAutoextension(contract) {
			findings = append(findings, newFinding("contract_expired", contract.ID, model.StagePayment, model.Yellow,
				"Срок договора истёк, но он продлевается сам",
				"Договор действует до "+exp.Value+", а дата валютирования "+fmtDate(vd)+". По пункту о "+
					"сроке договор автоматически продлевается, если ни одна сторона не заявила о "+
					"расторжении. Уточните у поставщика, что договор не расторгали; банк может попросить "+
					"подтверждение.", []*model.SourceRef{srcp(exp.Source), c31}, fopt{field: "valueDate"}))
		} else {
			findings = append(findings, newFinding("contract_expired", contract.ID, model.StagePayment, model.Red, "Срок договора истёк",
				"Договор действует до "+exp.Value+", а дата валютирования "+fmtDate(vd)+". По истёкшему "+
					"договору платить нельзя: нужно дополнительное соглашение о продлении.",
				[]*model.SourceRef{srcp(exp.Source), c31}, fopt{field: "valueDate"}))
		}
	}
	pv := ValidatePurpose(form["purpose"].Value)
	for _, iss := range pv.Issues {
		title := "Назначение перевода не заполнено"
		if iss.Rule == "purpose_latin" {
			title = "Назначение перевода: недопустимые символы"
		} else if pv.Length > maxPurpose {
			title = "Назначение перевода слишком длинное"
		}
		findings = append(findings, newFinding(iss.Rule, "form", model.StagePayment, model.Red, title, iss.Message, nil, fopt{field: "purpose"}))
	}
	if fib := form["beneficiaryIban"].Value; fib != "" {
		v := validateIBAN(fib, refIBAN, "в форме", "в договоре")
		if v.Error != "" {
			rule := "iban_format"
			if v.Error == "checksum" {
				rule = "iban_checksum"
			}
			findings = append(findings, newFinding(rule, "form", model.StagePayment, model.Red, "IBAN получателя в форме не проходит проверку",
				"Номер "+fib+" в поле «Счёт получателя» неверный: "+v.Hint+". Перевод с ним вернётся.", nil, fopt{field: "beneficiaryIban"}))
		}
	}

	urgent := form["urgent"].Value == "true"
	payAmount, _ := ParseAmount(form["amount"].Value)
	if payAmount == 0 && hasAmount {
		payAmount = amount
	}
	fees := map[string]model.FeeSim{}
	for _, t := range []string{"OUR", "SHA", "BEN"} {
		fees[t] = SimulateFees(payAmount, currency, t, urgent)
	}
	feeType := form["feeType"].Value
	if _, ok := fees[feeType]; !ok {
		feeType = "OUR"
	}
	debit := form["debitAccount"].Value
	var balance *model.BalanceCheck
	if payAmount != 0 {
		b := CheckBalance(debit, payAmount, fees[feeType].SenderFees, currency)
		balance = &b
		if !b.OK {
			acc := accountByID(debit)
			var head string
			if acc == nil {
				head = "У вас нет счёта в " + currency + ", а платить нужно " + fmtMoney(payAmount, currency) + " плюс " +
					"комиссия " + fmtMoney(fees[feeType].SenderFees, currency) + ". "
			} else {
				head = "На счёте «" + acc.Title + "» " + fmtMoney(b.Available, b.Currency) + ", а нужно " +
					fmtMoney(b.Needed, b.Currency) + " вместе с комиссией " + feeType + ". Не хватает " +
					fmtMoney(b.Shortfall, b.Currency) + ". "
			}
			var opts []string
			for _, o := range b.Options {
				opts = append(opts, "Вариант: "+lowerFirst(o.Title)+". "+o.Detail)
			}
			findings = append(findings, newFinding("insufficient_balance", payID, model.StagePayment, model.Red, "Не хватает денег на счёте",
				head+strings.Join(opts, " "), nil, fopt{field: "debitAccount"}))
		}
	}

	seen := map[string]bool{}
	uniq := []model.Finding{}
	for _, f := range findings {
		if seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		uniq = append(uniq, f)
	}
	sort.SliceStable(uniq, func(i, j int) bool {
		a, b := uniq[i], uniq[j]
		if stageOrder[a.Stage] != stageOrder[b.Stage] {
			return stageOrder[a.Stage] < stageOrder[b.Stage]
		}
		return sevOrder[a.Severity] < sevOrder[b.Severity]
	})

	country := form["beneficiaryCountry"].Value
	if country == "" {
		country = form["residenceCountry"].Value
	}
	name := form["beneficiaryName"].Value
	if name == "" {
		if v, ok := fval(cExt, "sellerName"); ok {
			name = v.Value
		}
	}
	var kbe *model.KbeInfo
	if country != "" || name != "" {
		k := SuggestKbe(country, name)
		kbe = &k
	}
	if extractions == nil {
		extractions = []model.ExtractionResult{}
	}
	return model.PaymentReport{Findings: uniq, Form: form, Extractions: extractions, Contract: summary, Fees: fees,
		Balance: balance, ValueDates: ValueDates(currency, today, now), Kbe: kbe}
}

func extractFields(d model.ParsedDoc) model.ExtractionResult { return extract.Fields(d) }
