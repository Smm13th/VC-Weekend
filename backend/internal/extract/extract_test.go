package extract

import (
	"strings"
	"testing"

	"vedhelper/internal/model"
	"vedhelper/internal/testkit"
)

func checkQuotes(t *testing.T, d model.ParsedDoc, ex model.ExtractionResult) {
	t.Helper()
	r := []rune(d.Text)
	for k, v := range ex.Fields {
		if string(r[v.Source.Start:v.Source.End]) != v.Source.Quote || v.Source.DocID != d.ID {
			t.Errorf("%s: цитата %+v", k, v.Source)
		}
		b := d.Blocks[v.Source.Block]
		if !(b.Start <= v.Source.Start && v.Source.Start < b.End) {
			t.Errorf("%s: Block %d не содержит начало цитаты", k, v.Source.Block)
		}
	}
}

func TestContractFields(t *testing.T) {
	d := testkit.Contract{}.Parsed("A/contract.docx")
	ex := Fields(d)
	checkQuotes(t, d, ex)
	f := ex.Fields
	cases := []struct {
		key   string
		value string
		norm  any
	}{
		{"contractNo", "AI-DXB-2026/014", "AI-DXB-2026/014"},
		{"contractDate", "15.09.2026", "2026-09-15"},
		{"expiryDate", "31.12.2026", "2026-12-31"},
		{"totalAmount", "USD 51,000.00", 51000.0},
		{"currency", "USD", "USD"},
		{"sellerName", "Gulf Horizon Machinery FZE", "Gulf Horizon Machinery FZE"},
		{"sellerRepresentative", "Manager Omar Al-Rashidi", "Manager Omar Al-Rashidi"},
		{"sellerCountry", "United Arab Emirates", "AE"},
		{"sellerIban", testkit.SellerIBAN, "AE980710123456789012345"},
		{"sellerSwift", "DPBLAEAD", "DPBLAEAD"},
		{"sellerAccountName", "Gulf Horizon Machinery FZE", "Gulf Horizon Machinery FZE"},
		{"sellerLicenseNo", "214587", "214587"},
		{"sellerBank", "Desert Pearl Bank PJSC, Jebel Ali Branch, branch code 071", nil},
		{"buyerName", "Alatau Industrial LLP", "Alatau Industrial LLP"},
		{"buyerIban", "KZ49 9980 0100 0041 2577", "KZ499980010000412577"},
		{"buyerBic", "KCJBKZKX", "KCJBKZKX"},
	}
	for _, c := range cases {
		v, ok := f[c.key]
		if !ok {
			t.Errorf("нет %s", c.key)
			continue
		}
		if v.Value != c.value || (c.norm != nil && v.Norm != c.norm) {
			t.Errorf("%s: %q %v", c.key, v.Value, v.Norm)
		}
		if v.Status != "doc" {
			t.Errorf("%s: статус %s", c.key, v.Status)
		}
	}
	if f["currency"].Source.Start != f["totalAmount"].Source.Start {
		t.Error("валюта должна браться из той же цитаты, что и сумма")
	}
	for _, k := range []string{"contractNo", "totalAmount", "sellerIban", "sellerSwift"} {
		if d.Blocks[f[k].Source.Block].Lang != "en" {
			t.Errorf("%s не из английской колонки", k)
		}
	}
	if _, ok := f["paymentTerms"]; ok {
		t.Error("в дефектном договоре явного срока оплаты нет")
	}
	fixed := Fields(testkit.Contract{Defects: testkit.NoDefects()}.Parsed("F/contract.docx"))
	if !strings.Contains(fixed.Fields["paymentTerms"].Value, "within 10 (ten) banking days") {
		t.Errorf("срок оплаты в исправленном: %+v", fixed.Fields["paymentTerms"])
	}
}

func TestContractEURAndRussianFallback(t *testing.T) {
	f := Fields(testkit.BContract().Parsed("B/contract.docx")).Fields
	if f["totalAmount"].Norm != 48000.0 || f["currency"].Norm != "EUR" {
		t.Fatalf("EUR: %+v %+v", f["totalAmount"], f["currency"])
	}
	if f["buyerIban"].Status != "conflict" {
		t.Error("счёта в EUR у покупателя нет, разные счета дают conflict")
	}
	d := testkit.Contract{AmountEN: "[AMOUNT]"}.Parsed("x/contract.docx")
	ta := Fields(d).Fields["totalAmount"]
	if ta.Norm != 51000.0 || ta.Confidence != "medium" || d.Blocks[ta.Source.Block].Lang != "ru" {
		t.Fatalf("запасной русский источник: %+v", ta)
	}
}

func TestPlaceholdersAreNotValues(t *testing.T) {
	f := Fields(testkit.RawContract().Parsed("raw/contract.docx")).Fields
	for _, k := range []string{"contractNo", "contractDate", "sellerName", "sellerIban", "sellerSwift", "buyerName", "expiryDate"} {
		if v, ok := f[k]; ok {
			t.Errorf("%s извлечён из шаблона: %q", k, v.Value)
		}
	}
	for k, v := range f {
		if strings.Contains(v.Value, "[") {
			t.Errorf("%s: %q", k, v.Value)
		}
	}
}

func TestInvoiceFields(t *testing.T) {
	d := testkit.LinesDoc(testkit.Invoice{}.Lines(), "A/invoice.pdf", model.KindInvoice)
	ex := Fields(d)
	checkQuotes(t, d, ex)
	f := ex.Fields
	want := map[string][2]any{
		"invoiceNo":          {"GHM-INV-0412", nil},
		"invoiceDate":        {"02.10.2026", "2026-10-02"},
		"invoiceAmount":      {"USD 51,000.00", 51000.0},
		"currency":           {"USD", "USD"},
		"invoiceDueDate":     {"12.10.2026", "2026-10-12"},
		"invoiceContractRef": {"AI-DXB-2026/014 dated 15.09.2026", "AI-DXB-2026/014|2026-09-15"},
		"sellerName":         {"Gulf Horizon Machinery FZE", nil},
		"sellerBank":         {"Desert Pearl Bank PJSC, Dubai, UAE", nil},
		"sellerAccountNo":    {"0123456789012345", nil},
		"sellerIban":         {testkit.SellerIBAN, "AE980710123456789012345"},
		"sellerSwift":        {"DPBLAEAD", "DPBLAEAD"},
		"intermediaryBank":   {"Northern Harbor Trust Company, New York, SWIFT NHTRUS33", "NHTRUS33"},
		"goodsDescription":   {"Hydraulic press HP-250 with control unit", nil},
		"deliveryTerms":      {"CPT Almaty (Incoterms 2020)", nil},
		"sellerCountry":      {"United Arab Emirates", "AE"},
		"buyerName":          {"Alatau Industrial LLP", nil},
	}
	for k, w := range want {
		v, ok := f[k]
		if !ok {
			t.Errorf("нет %s", k)
			continue
		}
		if v.Value != w[0] || (w[1] != nil && v.Norm != w[1]) {
			t.Errorf("%s: %q %v", k, v.Value, v.Norm)
		}
	}
	if !strings.Contains(f["paymentTerms"].Value, "prepayment") {
		t.Error("условия оплаты")
	}
	if !strings.HasPrefix(f["sellerAddress"].Value, "Office 1204") {
		t.Errorf("адрес: %q", f["sellerAddress"].Value)
	}
}

func TestLetterFields(t *testing.T) {
	d := testkit.LinesDoc(testkit.LetterLines(testkit.Hidden), "C/letter.pdf", model.KindLetter)
	ex := Fields(d)
	checkQuotes(t, d, ex)
	f := ex.Fields
	if f["sellerIban"].Norm != "GE47KV0000000483920175" || f["sellerSwift"].Norm != "KVRTGE22" ||
		!strings.HasPrefix(f["sellerBank"].Value, "Kavkaz Trade Bank JSC") || f["invoiceContractRef"].Norm != "AI-DXB-2026/014|2026-09-15" {
		t.Fatalf("%+v", f)
	}
	if _, ok := f["invoiceAmount"]; ok {
		t.Error("у письма нет суммы")
	}
	if f["sellerCountry"].Norm != "GE" {
		t.Errorf("страна %v", f["sellerCountry"].Norm)
	}
}

func TestConflictSameLabel(t *testing.T) {
	lines := testkit.Invoice{}.Lines()
	lines = append(lines[:len(lines)-1], "IBAN: "+testkit.SellerIBAN2, lines[len(lines)-1])
	f := Fields(testkit.LinesDoc(lines, "x/invoice.pdf", model.KindInvoice)).Fields
	ib := f["sellerIban"]
	if ib.Status != "conflict" || ib.Confidence != "low" || !strings.Contains(ib.Note, "AE69") || !strings.Contains(ib.Note, "«IBAN»") {
		t.Fatalf("%+v", ib)
	}
	if f["invoiceNo"].Status != "doc" {
		t.Error("номер инвойса без конфликта")
	}
}

func TestWrappedDueDateAndRef(t *testing.T) {
	var lines []string
	for _, l := range (testkit.Invoice{}).Lines() {
		switch {
		case strings.HasPrefix(l, "Payment terms"):
			lines = append(lines, "Payment terms: 100% prepayment within 10 calendar days from the invoice date (due", "date 12.10.2026)")
		case strings.HasPrefix(l, "Contract Ref."):
			lines = append(lines, "Contract Ref.: AI-DXB-2026/014 dated", "15.09.2026")
		default:
			lines = append(lines, l)
		}
	}
	d := testkit.LinesDoc(lines, "w/invoice.pdf", model.KindInvoice)
	ex := Fields(d)
	checkQuotes(t, d, ex)
	if ex.Fields["invoiceDueDate"].Norm != "2026-10-12" || ex.Fields["invoiceContractRef"].Norm != "AI-DXB-2026/014|2026-09-15" {
		t.Fatalf("%+v %+v", ex.Fields["invoiceDueDate"], ex.Fields["invoiceContractRef"])
	}
	if !strings.Contains(ex.Fields["invoiceContractRef"].Value, "dated 15.09.2026") {
		t.Error("перенос строки в значении заменяется пробелом")
	}
}

func TestFindCountry(t *testing.T) {
	cases := []struct{ in, code string }{
		{"Jebel Ali Free Zone, Dubai, United Arab Emirates", "AE"},
		{"12 Rustaveli Ave., Tbilisi, Georgia", "GE"},
		{"Алматы, Республика Казахстан", "KZ"},
		{"Dubai, UAE", "AE"},
		{"UAEX street", ""},
	}
	for _, c := range cases {
		code, _, _, _ := FindCountry(c.in)
		if code != c.code {
			t.Errorf("%q -> %q", c.in, code)
		}
	}
}
