package rules

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"vedhelper/internal/model"
	"vedhelper/internal/testkit"
)

const accNo = "02/2026/00731"

func invDoc(o testkit.Invoice, id string) model.ParsedDoc {
	return testkit.LinesDoc(o.Lines(), id, model.KindInvoice)
}

// kitCase собирает комплект как в спецификации (документы строятся в Go, без файлов).
func kitCase(id string) model.CaseInput {
	c := model.CaseInput{ValueDate: "2026-11-09", Today: "2026-11-07"}
	contract := func() model.ParsedDoc { return testkit.Contract{}.Parsed(id + "/contract.docx") }
	switch id {
	case "A":
		c.Docs = []model.ParsedDoc{contract(), invDoc(testkit.Invoice{}, "A/invoice.pdf")}
	case "B":
		c.Docs = []model.ParsedDoc{testkit.BContract().Parsed("B/contract.docx"), invDoc(testkit.Invoice{No: "GHM-INV-0450", Date: "08.10.2026",
			ContractNo: "AI-DXB-2026/021", ContractDate: "01.10.2026", Currency: "EUR", Total: "48,000.00", Due: "18.10.2026",
			Intermediary: "Rhein Nord Bank AG, Frankfurt am Main, SWIFT RHNBDEFF",
			Items:        [][4]string{{"Hydraulic press HP-250 with control unit", "1", "48,000.00", "48,000.00"}}}, "B/invoice.docx")}
	case "C":
		c.Docs = []model.ParsedDoc{contract(), invDoc(testkit.Invoice{No: "GHM-INV-0431", Date: "22.10.2026",
			Bank: "Kavkaz Trade Bank JSC, Tbilisi, Georgia", IBAN: testkit.FraudIBAN, Swift: "KVRTGE22", Due: "01.11.2026"}, "C/invoice.pdf"),
			testkit.LinesDoc(testkit.LetterLines(testkit.Hidden), "C/letter.pdf", model.KindLetter)}
		c.AccountingNumber = accNo
	case "D":
		c.Docs = []model.ParsedDoc{contract(), invDoc(testkit.Invoice{No: "GHM-INV-0440", Date: "25.10.2026", Total: "25,000.00",
			Items: [][4]string{{"Hydraulic press HP-250 with control unit", "1", "25,000.00", "25,000.00"}}}, "D/invoice.pdf")}
		c.AccountingNumber = accNo
		c.Payments = []model.Payment{{Date: "2026-10-05", Amount: 30000, Currency: "USD", Ref: "Invoice GHM-INV-0398"}}
	case "E":
		c.Docs = []model.ParsedDoc{contract(), invDoc(testkit.Invoice{No: "GHM-INV-0415", Date: "06.10.2026",
			Beneficiary: "Gulf Horizon Machinery Trading LLC", IBAN: testkit.TypoIBAN}, "E/invoice.pdf")}
		c.AccountingNumber = accNo
	case "raw":
		c.Docs = []model.ParsedDoc{testkit.RawContract().Parsed("raw/contract.docx")}
	case "fixed":
		c.Docs = []model.ParsedDoc{testkit.Contract{Defects: testkit.NoDefects()}.Parsed("F/contract.docx"), invDoc(testkit.Invoice{}, "F/invoice.pdf")}
		c.AccountingNumber = accNo
	}
	return c
}

func withTemplate(extra ...string) map[string]bool {
	return set(append(append([]string{}, templateRules...), extra...)...)
}

var kitExpected = map[string]map[string]bool{
	"A":     withTemplate("acc_number_required"),
	"B":     withTemplate("acc_number_required", "insufficient_balance"),
	"C":     withTemplate("acc_number_required", "requisites_changed", "hidden_instruction"),
	"D":     withTemplate("acc_number_required", "over_remaining"),
	"E":     withTemplate("acc_number_required", "beneficiary_name_mismatch", "iban_checksum"),
	"raw":   withTemplate("acc_number_required", "placeholder"),
	"fixed": set("acc_number_required"),
}

func TestKitsExpectedRules(t *testing.T) {
	for id, want := range kitExpected {
		c := kitCase(id)
		rep := CheckPayment(c)
		got := ruleSet(rep.Findings, model.Red, model.Yellow)
		if strings.Join(keys(got), ",") != strings.Join(keys(want), ",") {
			t.Errorf("%s:\n есть  %v\n ждали %v", id, keys(got), keys(want))
		}
		// форма отчёта
		seen := map[string]bool{}
		for _, f := range rep.Findings {
			if seen[f.ID] {
				t.Errorf("%s: повтор id %s", id, f.ID)
			}
			seen[f.ID] = true
		}
		checkSources(t, c.Docs, rep.Findings)
		if len(rep.Form) != 25 || len(rep.Fees) != 3 || rep.Fees["OUR"].FeeType != "OUR" || rep.Fees["BEN"].FeeType != "BEN" {
			t.Errorf("%s: форма %d, комиссии %d", id, len(rep.Form), len(rep.Fees))
		}
		for i := 1; i < len(rep.Findings); i++ {
			a, b := rep.Findings[i-1], rep.Findings[i]
			if stageOrder[a.Stage] > stageOrder[b.Stage] || (a.Stage == b.Stage && sevOrder[a.Severity] > sevOrder[b.Severity]) {
				t.Errorf("%s: порядок %s, %s", id, a.ID, b.ID)
			}
		}
		if _, err := json.Marshal(rep); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func find(rep model.PaymentReport, pred func(model.Finding) bool) *model.Finding {
	for i := range rep.Findings {
		if pred(rep.Findings[i]) {
			return &rep.Findings[i]
		}
	}
	return nil
}

func ruleIs(rule string, sev model.Severity) func(model.Finding) bool {
	return func(f model.Finding) bool { return f.Rule == rule && (sev == "" || f.Severity == sev) }
}

func TestKitAAccountingNumberRed(t *testing.T) {
	rep := CheckPayment(kitCase("A"))
	f := find(rep, func(f model.Finding) bool { return f.Rule == "acc_number_required" && f.Stage == model.StagePayment })
	if f == nil || f.Severity != model.Red || f.Field != "linkContract" || !f.MentorCheck || !strings.Contains(f.Title, "учётного номера контракта") {
		t.Fatalf("%+v", f)
	}
	if !rep.Contract.NeedsAccountingNumber || *rep.Contract.TotalUSD != 51000 {
		t.Errorf("%+v", rep.Contract)
	}
	if rep.ValueDates.Standard != "2026-11-10" || rep.ValueDates.Urgent != "2026-11-09" || rep.Kbe == nil || rep.Kbe.Code != "27" {
		t.Errorf("%+v %+v", rep.ValueDates, rep.Kbe)
	}
}

func TestCleanInvoiceGreens(t *testing.T) {
	for _, id := range []string{"A", "fixed"} {
		rep := CheckPayment(kitCase(id))
		greens := map[string]bool{}
		for _, f := range rep.Findings {
			if f.Severity == model.Green && f.Stage == model.StageDocuments {
				greens[f.Rule] = true
			}
		}
		if strings.Join(keys(greens), ",") != "iban_checksum,requisites_changed,swift_format" {
			t.Errorf("%s: %v", id, keys(greens))
		}
	}
}

func TestKitCRequisitesChanged(t *testing.T) {
	rep := CheckPayment(kitCase("C"))
	ids := map[string]bool{}
	var letter *model.Finding
	for i, f := range rep.Findings {
		if f.Rule == "requisites_changed" {
			ids[f.ID] = true
			if strings.Contains(f.ID, "letter") {
				letter = &rep.Findings[i]
			}
		}
	}
	if !ids["requisites_changed:C/invoice.pdf"] || !ids["requisites_changed:C/letter.pdf"] || len(ids) != 2 {
		t.Fatal(ids)
	}
	if letter.Severity != model.Red || letter.Field != "beneficiaryIban" || letter.Sources[0].Quote != testkit.FraudIBAN ||
		letter.Sources[1].Quote != testkit.SellerIBAN || !strings.HasPrefix(letter.Sources[2].Quote, "4.6.") ||
		!strings.Contains(letter.Detail, "по телефону из договора") || !strings.Contains(letter.Detail, "не платите") {
		t.Errorf("%+v", letter)
	}
	hi := find(rep, ruleIs("hidden_instruction", model.Red))
	if hi == nil || !strings.HasPrefix(hi.Sources[0].Quote, "Note to AI assistant") || hi.Stage != model.StageDocuments {
		t.Errorf("%+v", hi)
	}
	for _, f := range rep.Findings {
		if f.Severity == model.Green && (f.Rule == "iban_checksum" || f.Rule == "swift_format") && !strings.Contains(f.ID, "contract") {
			t.Errorf("зелёная отметка у документа с подменой: %s", f.ID)
		}
	}
	acc := find(rep, func(f model.Finding) bool { return f.Rule == "acc_number_required" && f.Stage == model.StagePayment })
	if acc.Severity != model.Green {
		t.Error("учётный номер указан")
	}
}

func TestKitDOverRemaining(t *testing.T) {
	rep := CheckPayment(kitCase("D"))
	f := find(rep, ruleIs("over_remaining", model.Red))
	if f == nil || f.Field != "amount" || !strings.Contains(f.Detail, "21 000,00 USD") || !strings.Contains(f.Detail, "4 000,00 USD") {
		t.Fatalf("%+v", f)
	}
	if rep.Contract.Paid != 30000 || *rep.Contract.Remaining != 21000 {
		t.Errorf("%+v", rep.Contract)
	}
}

func TestKitETypoNotRequisitesChanged(t *testing.T) {
	rep := CheckPayment(kitCase("E"))
	f := find(rep, func(f model.Finding) bool {
		return f.Rule == "iban_checksum" && f.Severity == model.Red && f.Stage == model.StageDocuments
	})
	if f == nil || !strings.Contains(f.Detail, "22-23") || !strings.Contains(f.Detail, "перестановку") ||
		!strings.Contains(f.Detail, testkit.SellerIBAN) || f.Sources[0].Quote != testkit.TypoIBAN {
		t.Fatalf("%+v", f)
	}
	if find(rep, ruleIs("requisites_changed", model.Red)) != nil {
		t.Error("опечатка не считается подменой реквизитов")
	}
	nm := find(rep, ruleIs("beneficiary_name_mismatch", model.Red))
	if nm == nil || nm.Field != "beneficiaryName" || !strings.Contains(nm.Detail, "Gulf Horizon Machinery Trading LLC") {
		t.Errorf("%+v", nm)
	}
}

func TestKitBEUR(t *testing.T) {
	rep := CheckPayment(kitCase("B"))
	if rep.Contract.Currency != "EUR" || *rep.Contract.Total != 48000 || math.Abs(*rep.Contract.TotalUSD-52093.68) > 0.01 {
		t.Errorf("%+v", rep.Contract)
	}
	ib := find(rep, ruleIs("insufficient_balance", model.Red))
	if ib == nil || !strings.Contains(ib.Detail, "нет счёта в EUR") || !strings.Contains(ib.Detail, "тенгового счёта") {
		t.Fatalf("%+v", ib)
	}
	if rep.Balance == nil || rep.Balance.OK || rep.Balance.Options[0].Kind != "convert" || rep.ValueDates.Cutoff != "14:00" {
		t.Errorf("%+v %+v", rep.Balance, rep.ValueDates)
	}
}

func TestContractExpired(t *testing.T) {
	c := kitCase("fixed")
	c.ValueDate, c.Today = "2027-01-11", "2027-01-08"
	f := find(CheckPayment(c), ruleIs("contract_expired", ""))
	if f == nil || f.Severity != model.Yellow || f.Field != "valueDate" || !strings.Contains(f.Detail, "автоматически") {
		t.Fatalf("%+v", f)
	}
	rows := testkit.Contract{Defects: testkit.NoDefects()}.Rows()
	for i := range rows {
		for k, p := range rows[i].EN {
			rows[i].EN[k] = strings.Replace(p, "shall be automatically extended", "may be extended", 1)
		}
		for k, p := range rows[i].RU {
			rows[i].RU[k] = strings.Replace(p, "автоматически продлевается", "может быть продлён", 1)
		}
	}
	c.Docs[0] = testkit.ParsedRows(rows, "F/contract.docx")
	if f := find(CheckPayment(c), ruleIs("contract_expired", "")); f == nil || f.Severity != model.Red {
		t.Fatalf("%+v", f)
	}
}

func TestFormPurposeAndIBANChecks(t *testing.T) {
	c := kitCase("fixed")
	c.Form = map[string]model.FieldState{"purpose": {Value: "Оплата по инвойсу", Status: "user"},
		"beneficiaryIban": {Value: "AE980710123456789012354", Status: "user"}}
	rep := CheckPayment(c)
	lat := find(rep, ruleIs("purpose_latin", model.Red))
	ib := find(rep, func(f model.Finding) bool { return f.ID == "iban_checksum:form" })
	if lat == nil || lat.Field != "purpose" || ib == nil || ib.Field != "beneficiaryIban" || ib.Stage != model.StagePayment {
		t.Fatalf("%+v %+v", lat, ib)
	}
	c.Form = map[string]model.FieldState{"purpose": {Value: strings.Repeat("A", 150), Status: "user"}}
	if f := find(CheckPayment(c), ruleIs("purpose_length", model.Red)); f == nil || f.Title != "Назначение перевода слишком длинное" {
		t.Fatalf("%+v", f)
	}
}

func TestAmountCurrencyDateRefCountry(t *testing.T) {
	contract := testkit.Contract{Defects: testkit.NoDefects()}.Parsed("x/contract.docx")
	run := func(o testkit.Invoice) map[string]model.Finding {
		c := kitCase("fixed")
		c.Docs = []model.ParsedDoc{contract, invDoc(o, "x/invoice.pdf")}
		out := map[string]model.Finding{}
		for _, f := range CheckPayment(c).Findings {
			if f.Severity != model.Green {
				out[f.Rule] = f
			}
		}
		return out
	}
	if r := run(testkit.Invoice{Total: "60,000.00"}); r["amount_exceeds_contract"].ID == "" || r["over_remaining"].ID == "" {
		t.Error("сумма больше договора")
	}
	if r := run(testkit.Invoice{Currency: "EUR"}); r["currency_mismatch"].ID == "" {
		t.Error("валюта")
	}
	r := run(testkit.Invoice{Date: "01.09.2026", ContractNo: "AI-DXB-2026/099"})
	if r["invoice_before_contract"].Severity != model.Yellow || r["invoice_contract_ref"].Severity != model.Yellow ||
		!strings.Contains(r["invoice_contract_ref"].Detail, "AI-DXB-2026/099") {
		t.Errorf("%+v", r)
	}
	if r := run(testkit.Invoice{Swift: "KVRTGE22"}); r["swift_iban_country"].Severity != model.Red || r["requisites_changed"].Severity != model.Red {
		t.Error("страна SWIFT и IBAN")
	}
	big := testkit.Contract{AmountEN: "90,000.00", AmountRU: "90 000,00", Defects: testkit.NoDefects()}.Parsed("x/contract.docx")
	c := kitCase("fixed")
	c.Docs = []model.ParsedDoc{big, invDoc(testkit.Invoice{Total: "70,000.00"}, "x/invoice.pdf")}
	rep := CheckPayment(c)
	f := find(rep, ruleIs("insufficient_balance", model.Red))
	if f == nil || f.Field != "debitAccount" || !strings.Contains(f.Detail, "Не хватает") || len(rep.Balance.Options) != 2 {
		t.Fatalf("%+v", f)
	}
}

// ---------- предзаполнение ----------

func TestFormKitA(t *testing.T) {
	rep := CheckPayment(kitCase("A"))
	f := rep.Form
	for _, id := range model.FieldIDs {
		if _, ok := f[id]; !ok {
			t.Errorf("нет поля %s", id)
		}
	}
	checks := []struct {
		field, value, status string
	}{
		{"requestNo", "будет присвоен после сохранения", "system"}, {"debitAccount", "acc-usd", "ai"},
		{"amount", "51000.00", "doc"}, {"valueDate", "2026-11-09", "ai"}, {"urgent", "false", "ai"},
		{"beneficiaryIban", "AE980710123456789012345", "doc"}, {"beneficiarySwift", "DPBLAEAD", "doc"},
		{"beneficiaryName", "Gulf Horizon Machinery FZE", "doc"}, {"knp", "711", "ai"},
		{"purpose", "PREPAYMENT FOR INDUSTRIAL EQUIPMENT INV GHM-INV-0412 DD 02.10.2026 CONTRACT AI-DXB-2026/014 DD 15.09.2026", "ai"},
		{"linkContract", "", "conflict"}, {"docNumber", "GHM-INV-0412", "doc"}, {"docDate", "2026-10-02", "doc"},
		{"attachments", "contract.docx; invoice.pdf", "doc"}, {"residenceCountry", "AE", "doc"}, {"beneficiaryCountry", "AE", "doc"},
		{"kbe", "27", "ai"}, {"addressType", "legal", "ai"}, {"feeType", "OUR", "ai"}, {"feeAccount", "acc-usd", "ai"},
		{"signerHead", "Сейткали Нурлан Ерланович", "ai"}, {"signerAccountant", "Не предусмотрен", "system"},
		{"useBonuses", "false", "ai"}, {"acceptTerms", "false", "empty"},
	}
	for _, c := range checks {
		if f[c.field].Value != c.value || f[c.field].Status != c.status {
			t.Errorf("%s: %q/%s, ждали %q/%s", c.field, f[c.field].Value, f[c.field].Status, c.value, c.status)
		}
	}
	if !strings.Contains(f["debitAccount"].Note, "64 300") || f["amount"].Source.Quote != "USD 51,000.00" || !strings.Contains(f["valueDate"].Note, "15:00") {
		t.Error("пояснения счёта, суммы, даты")
	}
	if f["intermediaryBank"].Status != "doc" || !strings.Contains(f["intermediaryBank"].Value, "NHTRUS33") {
		t.Error("банк-посредник")
	}
	opts := map[string]bool{}
	for _, o := range f["knp"].Options {
		opts[o.Value] = true
	}
	if !opts["710"] || !opts["711"] || !strings.Contains(f["knp"].Note, "Национального Банка") || !strings.Contains(f["kbe"].Note, "сверить") {
		t.Error("КНП и КБе")
	}
	ft := f["feeType"]
	if !strings.Contains(ft.Note, "недоплата") || len(ft.Options) != 3 || ft.Options[0].Value != "OUR" || ft.Options[2].Value != "BEN" ||
		!strings.Contains(ft.Options[1].Why, "50 960,00") {
		t.Errorf("%+v", ft)
	}
	for id, st := range f {
		if st.Suggested == "" && st.Value != "" {
			t.Errorf("%s: нет suggested", id)
		}
	}
}

func TestFormConflictOnSubstitution(t *testing.T) {
	f := CheckPayment(kitCase("C")).Form
	ib := f["beneficiaryIban"]
	if ib.Status != "conflict" || ib.Value != "" || len(ib.Options) != 2 || ib.Options[0].Value != "AE980710123456789012345" ||
		ib.Options[1].Value != "GE47KV0000000483920175" || ib.Options[0].Why == "" || ib.Options[1].Why == "" || ib.Suggested != "AE980710123456789012345" {
		t.Fatalf("%+v", ib)
	}
	if f["beneficiarySwift"].Status != "conflict" || f["beneficiaryName"].Status != "doc" {
		t.Error("SWIFT в конфликте, имя совпадает")
	}
	if f["linkContract"].Value != accNo || f["linkContract"].Status != "system" || !strings.HasSuffix(f["purpose"].Value, "UN "+accNo) || runeLen(f["purpose"].Value) > 140 {
		t.Errorf("%+v %+v", f["linkContract"], f["purpose"])
	}
}

func TestFormConflictsKitsEAndD(t *testing.T) {
	f := CheckPayment(kitCase("E")).Form
	if f["beneficiaryIban"].Status != "conflict" || f["beneficiaryIban"].Options[0].Value != "AE980710123456789012345" {
		t.Errorf("%+v", f["beneficiaryIban"])
	}
	nm := f["beneficiaryName"]
	if nm.Status != "conflict" || len(nm.Options) != 2 || nm.Options[0].Value != "Gulf Horizon Machinery FZE" || nm.Options[1].Value != "Gulf Horizon Machinery Trading LLC" {
		t.Errorf("%+v", nm)
	}
	d := CheckPayment(kitCase("D")).Form["amount"]
	if d.Status != "conflict" || d.Suggested != "21000.00" || len(d.Options) != 2 || d.Options[0].Value != "21000.00" || d.Options[1].Value != "25000.00" {
		t.Errorf("%+v", d)
	}
}

func TestFormKitBNoEURAccount(t *testing.T) {
	f := CheckPayment(kitCase("B")).Form
	if f["debitAccount"].Status != "conflict" || f["debitAccount"].Value != "" || !strings.Contains(f["debitAccount"].Note, "конвертац") ||
		!strings.Contains(f["intermediaryBank"].Value, "RHNBDEFF") || f["feeAccount"].Status != "conflict" {
		t.Errorf("%+v", f["debitAccount"])
	}
}

func TestFormUserFieldsPreservedAndAcceptTerms(t *testing.T) {
	c := kitCase("A")
	c.Form = map[string]model.FieldState{
		"amount":           {Value: "50000.00", Status: "user", Confirmed: true},
		"beneficiarySwift": {Value: "DPBLAEAD", Status: "doc", Confirmed: true},
		"debitAccount":     {Value: "acc-kzt", Status: "ai", Confirmed: true},
		"acceptTerms":      {Value: "true", Status: "doc", Confirmed: true},
	}
	rep := CheckPayment(c)
	f := rep.Form
	if f["amount"].Value != "50000.00" || f["amount"].Status != "user" || f["amount"].Suggested != "51000.00" || !f["amount"].Confirmed {
		t.Errorf("%+v", f["amount"])
	}
	if !f["beneficiarySwift"].Confirmed || f["debitAccount"].Value != "acc-usd" || f["debitAccount"].Confirmed {
		t.Error("подтверждения")
	}
	if f["acceptTerms"].Value != "false" || f["acceptTerms"].Status != "empty" || f["acceptTerms"].Confirmed {
		t.Errorf("acceptTerms помощник не ставит: %+v", f["acceptTerms"])
	}
	if rep.Fees["OUR"].SenderTotal != 50000+100+30 {
		t.Errorf("комиссии от суммы пользователя: %+v", rep.Fees["OUR"])
	}
}

func TestFormTaskTimingAndServices(t *testing.T) {
	c := kitCase("fixed")
	c.Task = map[string]string{"timing": "postpay"}
	f := CheckPayment(c).Form
	if f["knp"].Value != "710" || !strings.HasPrefix(f["purpose"].Value, "PAYMENT FOR") {
		t.Errorf("%+v %+v", f["knp"], f["purpose"])
	}
	if f["feeType"].Value != "SHA" || f["feeType"].Status != "doc" || !strings.HasPrefix(f["feeType"].Source.Quote, "4.7.") {
		t.Errorf("тип комиссии из пункта 4.7: %+v", f["feeType"])
	}
	c.Task = map[string]string{"goods": "services"}
	if v := CheckPayment(c).Form["knp"].Value; v != "780" {
		t.Error(v)
	}
}

func TestBuildFormWithoutContract(t *testing.T) {
	c := kitCase("A")
	c.Docs = c.Docs[1:]
	exts := []model.ExtractionResult{}
	for _, d := range c.Docs {
		exts = append(exts, extractFields(d))
	}
	f := BuildForm(c, exts, nil, model.ContractSummary{})
	if len(f) != 25 || f["beneficiaryIban"].Status != "doc" || f["linkContract"].Status != "empty" {
		t.Errorf("%+v %+v", f["beneficiaryIban"], f["linkContract"])
	}
	rep := CheckPayment(c)
	if find(rep, ruleIs("acc_number_required", "")) != nil || len(rep.Form) != 25 {
		t.Error("без договора учётный номер не проверяется")
	}
}

func TestEmptyCase(t *testing.T) {
	rep := CheckPayment(model.CaseInput{ValueDate: "2026-11-09", Today: "2026-11-07"})
	if len(rep.Form) != 25 || rep.Balance != nil || rep.Kbe != nil || rep.Findings == nil {
		t.Errorf("%+v", rep)
	}
	if _, err := json.Marshal(rep); err != nil {
		t.Error(err)
	}
}
