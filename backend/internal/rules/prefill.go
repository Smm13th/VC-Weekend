package rules

import (
	"fmt"
	"strconv"
	"strings"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
	"vedhelper/internal/refdata"
)

var prepayRx = rxi(`pre-?payment|advance\s+payment|in\s+advance|предоплат|аванс`)

func amt2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// feeFromContract — тип комиссии из пункта договора о банковских комиссиях.
func feeFromContract(c model.CaseInput) (string, *model.SourceRef) {
	for _, pd := range c.Docs {
		if pd.Kind != model.KindContract {
			continue
		}
		for _, a := range alignDoc(du.New(pd)) {
			t := quoteOf(a.p.EN)
			if !rxi(`bank\s+(charges|commissions?|fees)`).MatchString(t) {
				continue
			}
			switch {
			case rxi(`Buyer's\s+bank[^;.]*borne\s+by\s+the\s+Buyer[^.]*other[^.]*borne\s+by\s+the\s+Seller`).MatchString(t):
				return "SHA", a.p.EN
			case rxi(`all\s+bank\s+charges[^.]*borne\s+by\s+the\s+Buyer`).MatchString(t):
				return "OUR", a.p.EN
			case rxi(`all\s+bank\s+charges[^.]*borne\s+by\s+the\s+Seller`).MatchString(t):
				return "BEN", a.p.EN
			}
		}
	}
	return "", nil
}

func getF(e *model.ExtractionResult, key string) *model.Extracted {
	if e == nil {
		return nil
	}
	if v, ok := e.Fields[key]; ok {
		return &v
	}
	return nil
}

// BuildForm предзаполняет 25 полей заявки из извлечённых данных, находок и справочников.
// Поля со статусом user сохраняются, acceptTerms помощник никогда не ставит.
func BuildForm(c model.CaseInput, ex []model.ExtractionResult, findings []model.Finding, s model.ContractSummary) map[string]model.FieldState {
	var cExt *model.ExtractionResult
	var invs, letters []*model.ExtractionResult
	for i := range ex {
		e := &ex[i]
		switch e.Kind {
		case model.KindContract:
			if cExt == nil {
				cExt = e
			}
		case model.KindInvoice:
			invs = append(invs, e)
		case model.KindLetter:
			letters = append(letters, e)
		}
	}
	var inv *model.ExtractionResult
	bestKey := ""
	for _, e := range invs {
		k := ""
		if v := getF(e, "invoiceDate"); v != nil {
			k = normStr(*v)
		}
		if inv == nil || k >= bestKey {
			inv, bestKey = e, k
		}
	}
	red := map[string]bool{}
	for _, f := range findings {
		if f.Severity == model.Red {
			red[f.Rule+"\x00"+idTail(f.ID)] = true
		}
	}
	prev := c.Form
	F := map[string]model.FieldState{}
	tf := refdata.TariffsData()

	cur := ""
	if v := getF(inv, "currency"); v != nil {
		cur = normStr(*v)
	}
	if cur == "" {
		cur = s.Currency
	}
	if cur == "" {
		cur = "USD"
	}
	invAmount := getF(inv, "invoiceAmount")
	var amt *float64
	if invAmount != nil {
		if v, ok := normFloat(*invAmount); ok {
			amt = &v
		}
	}
	today := c.Today
	if today == "" {
		today = c.ValueDate
	}

	F["requestNo"] = model.FieldState{Value: "будет присвоен после сохранения", Status: "system", Confirmed: true,
		Note: "Номер заявки присвоит система после сохранения."}

	// счёт списания
	acc := accountForCurrency(cur)
	var accOpts []model.FieldOption
	for _, a := range accounts() {
		why := "Валюта счёта совпадает с валютой платежа, конвертация не нужна."
		if a.Currency != cur {
			why = "Придётся купить " + cur + " за " + a.Currency + ", банк возьмёт плату за конвертацию."
		}
		accOpts = append(accOpts, model.FieldOption{Value: a.ID, Label: a.Title + ", " + fmtMoney(a.Balance, a.Currency), Why: why})
	}
	if acc != nil {
		F["debitAccount"] = model.FieldState{Value: acc.ID, Status: "ai", Options: accOpts,
			Note: "Платёж в " + cur + ", поэтому выбран счёт «" + acc.Title + "». На нём " + fmtMoney(acc.Balance, cur) + "."}
	} else {
		note := "У вас нет счёта в " + cur + ". Можно платить с тенгового счёта с конвертацией: банк купит " + cur +
			" за тенге по курсу с наценкой " + spreadStr() + "%"
		if kzt := accountForCurrency("KZT"); kzt != nil {
			note += ". На тенговом счёте " + fmtMoney(kzt.Balance, "KZT") + "."
		} else {
			note += "."
		}
		F["debitAccount"] = model.FieldState{Value: "", Status: "conflict", Options: accOpts, Note: note}
	}
	debit := F["debitAccount"].Value

	// сумма
	over := false
	for _, f := range findings {
		if f.Rule == "over_remaining" {
			over = true
			break
		}
	}
	switch {
	case amt != nil && !over:
		F["amount"] = model.FieldState{Value: amt2(*amt), Status: "doc", Source: srcp(invAmount.Source),
			Note: "Сумма из инвойса: " + fmtMoney(*amt, cur) + "."}
	case amt != nil && over:
		rem := 0.0
		if s.Remaining != nil {
			rem = *s.Remaining
		}
		sc := s.Currency
		if sc == "" {
			sc = cur
		}
		F["amount"] = model.FieldState{Value: "", Status: "conflict", Source: srcp(invAmount.Source), Suggested: amt2(rem),
			Note: "Инвойс на " + fmtMoney(*amt, cur) + " больше остатка по договору " + fmtMoney(rem, sc) + ".",
			Options: []model.FieldOption{
				{Value: amt2(rem), Label: "Остаток по договору: " + fmtMoney(rem, cur), Why: "Столько ещё можно заплатить по договору без дополнительного соглашения."},
				{Value: amt2(*amt), Label: "Сумма инвойса: " + fmtMoney(*amt, cur), Why: "Можно заплатить только после допсоглашения об увеличении суммы договора."},
			}}
	default:
		F["amount"] = model.FieldState{Value: "", Status: "conflict", Note: "В документах не нашлась сумма к оплате, введите её."}
	}

	// дата валютирования и срочность
	vds := ValueDates(cur, today, timeZero)
	cutoff := cutoffFor(cur)
	urgentFee := fmtMoneyD(tf.UrgentFeeKzt, "₸", 0)
	vd := parseDate(c.ValueDate)
	if vd == "" {
		vd = vds.Standard
	}
	F["valueDate"] = model.FieldState{Value: vd, Status: "ai",
		Note: "Обычный перевод в " + cur + ", поданный до " + cutoff + ", банк обработает в ближайший рабочий день (" +
			fmtDate(vds.Standard) + "). Если нужно сегодня, включите срочный перевод: он уйдёт " + fmtDate(vds.Urgent) +
			" при подаче до " + cutoff + ", стоит " + urgentFee + ".",
		Options: []model.FieldOption{
			{Value: vds.Standard, Label: fmtDate(vds.Standard) + ", обычный", Why: "Ближайшая дата без доплаты."},
			{Value: vds.Urgent, Label: fmtDate(vds.Urgent) + ", срочный", Why: "Нужна отметка «Срочный перевод», доплата " + urgentFee + "."},
		}}
	F["urgent"] = model.FieldState{Value: "false", Status: "ai",
		Note: "Срочность не нужна, если поставщика устраивает обычная дата. Срочный перевод стоит " + urgentFee + "."}

	// реквизиты получателя
	srcDoc := inv
	if srcDoc == nil && len(letters) > 0 {
		srcDoc = letters[len(letters)-1]
	}
	if srcDoc == nil {
		srcDoc = cExt
	}
	var other []*model.ExtractionResult
	if inv != nil {
		other = append(other, inv)
	}
	other = append(other, letters...)

	fromDoc := func(key, field string, norm func(string) string, ruleKeys []string, whyContract, whyDoc string) {
		val := getF(srcDoc, key)
		cval := getF(cExt, key)
		var conflictDocs []*model.ExtractionResult
		for _, e := range other {
			for _, r := range ruleKeys {
				if red[r+"\x00"+e.DocID] {
					conflictDocs = append(conflictDocs, e)
					break
				}
			}
		}
		switch {
		case len(conflictDocs) > 0 && cval != nil:
			alt := getF(conflictDocs[0], key)
			opts := []model.FieldOption{{Value: norm(cval.Value), Label: "Из договора: " + cval.Value, Why: whyContract}}
			if alt != nil && norm(alt.Value) != norm(cval.Value) {
				kind := "письма"
				if conflictDocs[0].Kind == model.KindInvoice {
					kind = "инвойса"
				}
				opts = append(opts, model.FieldOption{Value: norm(alt.Value), Label: "Из " + kind + ": " + alt.Value, Why: whyDoc})
			}
			src := cval.Source
			if alt != nil {
				src = alt.Source
			}
			F[field] = model.FieldState{Value: "", Status: "conflict", Source: srcp(src), Options: opts, Suggested: norm(cval.Value),
				Note: "Документы противоречат друг другу. Выберите значение только после звонка поставщику по телефону из договора."}
		case val != nil:
			F[field] = model.FieldState{Value: norm(val.Value), Status: "doc", Source: srcp(val.Source)}
		default:
			F[field] = model.FieldState{Value: "", Status: "conflict", Note: "Значение не найдено в документах, введите вручную."}
		}
	}
	fromDoc("sellerIban", "beneficiaryIban", normalizeIBAN, []string{"requisites_changed", "iban_checksum", "iban_format"},
		"Счёт из статьи 13 подписанного договора.", "Счёт из нового документа. Он не совпадает с договором или не проходит проверку, "+
			"платить на него можно только после подтверждения поставщиком.")
	fromDoc("sellerSwift", "beneficiarySwift", normalizeSwift, []string{"requisites_changed", "swift_format", "swift_iban_country"},
		"SWIFT банка из статьи 13 договора.", "SWIFT из нового документа, не совпадает с договором.")
	fromDoc("sellerName", "beneficiaryName", du.CollapseWS, []string{"beneficiary_name_mismatch"},
		"Название продавца из договора.", "Название из инвойса, оно отличается от договора.")
	if st := F["beneficiaryIban"]; st.Status == "doc" {
		v := ValidateIBAN(st.Value, "")
		if v.OK {
			st.Note = "IBAN проходит проверку контрольной суммы."
		} else {
			st.Note = "IBAN не проходит проверку: " + v.Hint + "."
		}
		F["beneficiaryIban"] = st
	}
	if st := F["beneficiarySwift"]; st.Status == "doc" {
		if b := bankBySwift(st.Value); b != nil {
			st.Note = "Банк получателя: " + b.Name + ", " + b.City + "."
			F["beneficiarySwift"] = st
		}
	}
	if inter := getF(inv, "intermediaryBank"); inter != nil {
		F["intermediaryBank"] = model.FieldState{Value: inter.Value, Status: "doc", Source: srcp(inter.Source),
			Note: "Банк-посредник указан поставщиком в инвойсе."}
	} else {
		F["intermediaryBank"] = model.FieldState{Value: "", Status: "empty",
			Note: "Поставщик не указал банк-посредник. Обычно поле можно оставить пустым, банк выберет маршрут сам."}
	}

	// КНП
	task := c.Task
	terms := getF(inv, "paymentTerms")
	prepay := task["timing"] == "prepay" || (task["timing"] != "postpay" && terms != nil && prepayRx.MatchString(terms.Value))
	services := task["goods"] == "services"
	code := "710"
	if services {
		code = "780"
	} else if prepay {
		code = "711"
	}
	whyMap := map[string]string{"710": "Подходит, если товар уже отгружен или оплата после поставки.",
		"711": "Подходит для предоплаты: инвойс выставлен до отгрузки.",
		"780": "Для оплаты услуг, а не товара.", "859": "Только если платёж не подходит ни под один код."}
	var knpOpts []model.FieldOption
	for _, it := range refdata.Knp() {
		why, ok := whyMap[it.Code]
		if !ok {
			why = it.When
		}
		knpOpts = append(knpOpts, model.FieldOption{Value: it.Code, Label: it.Code + " " + it.Title, Why: why})
	}
	var reason string
	switch {
	case services:
		reason = "Задача на стартовом экране: услуги."
	case prepay && terms != nil:
		reason = "В инвойсе условие оплаты «" + terms.Value + "», это предоплата."
	case prepay:
		reason = "Вы указали, что платите до поставки."
	default:
		reason = "Оплата за товар по контракту."
	}
	F["knp"] = model.FieldState{Value: code, Status: "ai", Options: knpOpts,
		Note: reason + " Код " + code + ". Сверьте его с классификатором Национального Банка РК (проверка у ментора)."}

	// назначение
	invNo, invDate := getF(inv, "invoiceNo"), getF(inv, "invoiceDate")
	cNo, cDate := getF(cExt, "contractNo"), getF(cExt, "contractDate")
	goods := ""
	for _, pd := range c.Docs {
		if pd.Kind == model.KindContract {
			if m := rxi(`\b((?:[a-z]+ )?equipment)\b`).FindStringSubmatch(pd.Text); m != nil {
				words := strings.Fields(m[1])
				switch strings.ToLower(words[0]) {
				case "the", "of", "and", "for", "a", "an", "all", "any", "with", "buys", "sells":
					words = words[1:]
				}
				goods = strings.Join(words, " ")
			}
			break
		}
	}
	if goods == "" {
		if g := getF(inv, "goodsDescription"); g != nil {
			goods = g.Value
		}
	}
	pp := PurposeParams{Goods: goods, AccountingNumber: c.AccountingNumber, Prepay: prepay}
	if invNo != nil {
		pp.InvoiceNo = invNo.Value
	}
	if invDate != nil {
		pp.InvoiceDate = normStr(*invDate)
	}
	if cNo != nil {
		pp.ContractNo = cNo.Value
	}
	if cDate != nil {
		pp.ContractDate = normStr(*cDate)
	}
	purpose := BuildPurposeTemplate(pp)
	pnote := "Собрано из номера и даты инвойса и договора латиницей, не длиннее 140 символов"
	if c.AccountingNumber != "" && strings.Contains(purpose, "UN ") {
		pnote += ", с учётным номером контракта."
	} else {
		pnote += "."
	}
	F["purpose"] = model.FieldState{Value: purpose, Status: "ai", Note: pnote}

	// привязка договора
	switch {
	case c.AccountingNumber != "":
		F["linkContract"] = model.FieldState{Value: c.AccountingNumber, Status: "system",
			Note: "Учётный номер контракта, под ним договор стоит на учёте в банке."}
	case s.NeedsAccountingNumber:
		st := model.FieldState{Value: "", Status: "conflict",
			Note: "Сумма договора больше порога, нужен учётный номер контракта. Его пока нет: поставьте договор на учёт и впишите номер."}
		if cNo != nil {
			st.Options = []model.FieldOption{{Value: cNo.Value, Label: "Договор " + cNo.Value, Why: "Подойдёт только после получения учётного номера контракта."}}
		}
		F["linkContract"] = st
	case cNo != nil:
		F["linkContract"] = model.FieldState{Value: cNo.Value, Status: "ai", Source: srcp(cNo.Source),
			Note: "Учётный номер контракта не нужен, привязываем договор по номеру."}
	default:
		F["linkContract"] = model.FieldState{Value: "", Status: "empty", Note: "Договор не загружен."}
	}

	switch {
	case invNo != nil:
		F["docNumber"] = model.FieldState{Value: invNo.Value, Status: "doc", Source: srcp(invNo.Source)}
	case cNo != nil:
		F["docNumber"] = model.FieldState{Value: cNo.Value, Status: "doc", Source: srcp(cNo.Source)}
	default:
		F["docNumber"] = model.FieldState{Value: "", Status: "empty"}
	}
	switch {
	case invDate != nil && normStr(*invDate) != "":
		F["docDate"] = model.FieldState{Value: normStr(*invDate), Status: "doc", Source: srcp(invDate.Source)}
	case cDate != nil && normStr(*cDate) != "":
		F["docDate"] = model.FieldState{Value: normStr(*cDate), Status: "doc", Source: srcp(cDate.Source)}
	default:
		F["docDate"] = model.FieldState{Value: "", Status: "empty"}
	}
	var names []string
	for _, pd := range c.Docs {
		names = append(names, pd.Name)
	}
	if len(names) > 0 {
		F["attachments"] = model.FieldState{Value: strings.Join(names, "; "), Status: "doc", Note: "Приложим все загруженные документы."}
	} else {
		F["attachments"] = model.FieldState{Value: "", Status: "empty"}
	}

	// страна
	ctry := getF(inv, "sellerCountry")
	if ctry == nil {
		ctry = getF(cExt, "sellerCountry")
	}
	if ctry != nil && normStr(*ctry) != "" {
		code := normStr(*ctry)
		nm := countryName(code)
		F["residenceCountry"] = model.FieldState{Value: code, Status: "doc", Source: srcp(ctry.Source), Note: "По адресу продавца: " + nm + "."}
		F["beneficiaryCountry"] = model.FieldState{Value: code, Status: "doc", Source: srcp(ctry.Source), Note: "Страна из адреса регистрации получателя: " + nm + "."}
	} else {
		F["residenceCountry"] = model.FieldState{Value: "", Status: "conflict", Note: "Страна получателя не найдена."}
		F["beneficiaryCountry"] = model.FieldState{Value: "", Status: "conflict", Note: "Страна получателя не найдена."}
	}
	nameForKbe := F["beneficiaryName"].Value
	if nameForKbe == "" {
		if v := getF(cExt, "sellerName"); v != nil {
			nameForKbe = v.Value
		}
	}
	kbe := SuggestKbe(F["residenceCountry"].Value, nameForKbe)
	F["kbe"] = model.FieldState{Value: kbe.Code, Status: "ai", Note: kbe.Why + " Нужно сверить со справочником."}
	F["addressType"] = model.FieldState{Value: "legal", Status: "ai", Note: "В реквизитах указан адрес регистрации компании."}

	// комиссия
	payAmt := 0.0
	if amt != nil {
		payAmt = *amt
	}
	sims := map[string]model.FeeSim{}
	for _, t := range []string{"OUR", "SHA", "BEN"} {
		sims[t] = SimulateFees(payAmt, cur, t, false)
	}
	feeOpts := []model.FieldOption{
		{Value: "OUR", Label: "OUR: все комиссии платите вы",
			Why: "Спишут " + fmtMoney(sims["OUR"].SenderTotal, cur) + ", поставщик получит всю сумму " + fmtMoney(sims["OUR"].ReceiverGets, cur) + "."},
		{Value: "SHA", Label: "SHA: комиссии пополам",
			Why: "Спишут " + fmtMoney(sims["SHA"].SenderTotal, cur) + ", поставщик получит около " + fmtMoney(sims["SHA"].ReceiverGets, cur) +
				", недополучит " + fmtMoney(sims["SHA"].Shortfall, cur) + "."},
		{Value: "BEN", Label: "BEN: все комиссии за счёт получателя",
			Why: "Спишут " + fmtMoney(sims["BEN"].SenderTotal, cur) + ", поставщик получит около " + fmtMoney(sims["BEN"].ReceiverGets, cur) +
				", недополучит " + fmtMoney(sims["BEN"].Shortfall, cur) + "."},
	}
	if ft, ftSrc := feeFromContract(c); ft != "" {
		F["feeType"] = model.FieldState{Value: ft, Status: "doc", Source: ftSrc, Options: feeOpts,
			Note: "В договоре сказано, кто платит комиссии, это соответствует типу " + ft + "."}
	} else {
		F["feeType"] = model.FieldState{Value: "OUR", Status: "ai", Options: feeOpts,
			Note: "В договоре не сказано, кто платит комиссии. С SHA или BEN поставщик получит меньше суммы инвойса, " +
				"и формально это недоплата. OUR надёжнее: " + feeOpts[0].Why}
	}
	if debit != "" {
		F["feeAccount"] = model.FieldState{Value: debit, Status: "ai", Note: "Комиссию спишем с того же счёта, что и перевод."}
	} else {
		F["feeAccount"] = model.FieldState{Value: "", Status: "conflict", Note: "Сначала выберите счёт списания."}
	}
	F["signerHead"] = model.FieldState{Value: refdata.Accounts().Client.Director, Status: "ai", Note: "Руководитель компании из карточки клиента."}
	F["signerAccountant"] = model.FieldState{Value: "Не предусмотрен", Status: "system", Confirmed: true, Note: "Вторая подпись не настроена."}
	F["useBonuses"] = model.FieldState{Value: "false", Status: "ai",
		Note: fmt.Sprintf("Бонусами можно покрыть не больше %s%% комиссии. Включите, если хотите сэкономить.", pyFloat(tf.BonusMaxSharePct))}
	F["acceptTerms"] = model.FieldState{Value: "false", Status: "empty", Note: "Отметку ставит только человек."}

	// suggested, пользовательские значения, подтверждения
	out := map[string]model.FieldState{}
	for _, fid := range model.FieldIDs {
		st := F[fid]
		if st.Suggested == "" {
			st.Suggested = st.Value
		}
		p, hasPrev := prev[fid]
		if fid == "acceptTerms" {
			if hasPrev && p.Status == "user" {
				out[fid] = p
				continue
			}
			out[fid] = model.FieldState{Value: "false", Status: "empty", Note: st.Note, Suggested: "false"}
			continue
		}
		if hasPrev {
			if p.Status == "user" {
				p.Suggested = st.Suggested
				out[fid] = p
				continue
			}
			if p.Confirmed && p.Value == st.Value {
				st.Confirmed = true
			}
		}
		out[fid] = st
	}
	return out
}
