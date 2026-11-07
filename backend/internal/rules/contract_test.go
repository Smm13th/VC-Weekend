package rules

import (
	"sort"
	"strings"
	"testing"

	"vedhelper/internal/model"
	"vedhelper/internal/testkit"
)

// templateRules — дефекты шаблона договора (expected во всех комплектах с дефектным шаблоном).
var templateRules = []string{"bilingual_length_ratio", "payment_term_missing", "fees_not_specified", "clause_air_waybill",
	"clause_consent_party", "bilingual_numbering_gap", "clause_cross_reference", "clause_duplicate"}

func ruleSet(fs []model.Finding, sev ...model.Severity) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		ok := len(sev) == 0
		for _, s := range sev {
			if f.Severity == s {
				ok = true
			}
		}
		if ok {
			out[f.Rule] = true
		}
	}
	return out
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func checkSources(t *testing.T, docs []model.ParsedDoc, fs []model.Finding) {
	t.Helper()
	byID := map[string]model.ParsedDoc{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	for _, f := range fs {
		for _, s := range f.Sources {
			d, ok := byID[s.DocID]
			if !ok {
				t.Errorf("%s: неизвестный документ %s", f.ID, s.DocID)
				continue
			}
			r := []rune(d.Text)
			if s.Start < 0 || s.End > len(r) || string(r[s.Start:s.End]) != s.Quote || s.Quote == "" {
				t.Errorf("%s: цитата не равна срезу рун: %+v", f.ID, s)
				continue
			}
			b := d.Blocks[s.Block]
			if !(b.Start <= s.Start && s.Start < b.End) {
				t.Errorf("%s: Block %d не содержит начало цитаты", f.ID, s.Block)
			}
		}
		if f.Title == "" || f.Detail == "" || strings.Contains(f.Detail, "—") || strings.Contains(f.Title, "—") {
			t.Errorf("%s: заголовок или текст", f.ID)
		}
	}
}

func byRule(fs []model.Finding) map[string]model.Finding {
	out := map[string]model.Finding{}
	for _, f := range fs {
		if f.Severity == model.Green {
			continue
		}
		if _, ok := out[f.Rule]; !ok {
			out[f.Rule] = f
		}
	}
	return out
}

func TestAlignClauses(t *testing.T) {
	pairs := AlignClauses(testkit.Contract{}.Parsed("A/contract.docx"))
	var nums []string
	for _, p := range pairs {
		nums = append(nums, p.Num)
	}
	joined := "," + strings.Join(nums, ",") + ","
	if !strings.HasPrefix(joined, ",1,1.1,1.2,") || strings.Contains(joined, ",12.6,") || !strings.Contains(joined, ",12.7,") || !strings.Contains(joined, ",13,") {
		t.Fatal(nums)
	}
	get := func(n string) model.ClausePair {
		for _, p := range pairs {
			if p.Num == n {
				return p
			}
		}
		t.Fatalf("нет пункта %s", n)
		return model.ClausePair{}
	}
	p22 := get("2.2")
	if !strings.HasPrefix(p22.EN.Quote, "2.2. The total value") || !strings.HasPrefix(p22.RU.Quote, "2.2. Общая стоимость") ||
		strings.Join(p22.NumbersEN, ",") != "0,51000" || strings.Join(p22.NumbersRU, ",") != "0,51000" {
		t.Fatalf("%+v", p22)
	}
	p31 := get("3.1")
	if strings.Join(p31.NumbersEN, ",") != "2,D2026-12-31" || strings.Join(p31.NumbersRU, ",") != "2,D2026-12-31" || p31.Ratio <= 1.15 {
		t.Fatalf("%+v", p31)
	}
	p111 := get("11.1")
	if strings.Join(p111.NumbersEN, ",") != strings.Join(p111.NumbersRU, ",") || !strings.Contains(p111.EN.Quote, "\nd) ") {
		t.Fatalf("%+v", p111)
	}
}

func TestDefectiveContract(t *testing.T) {
	d := testkit.Contract{}.Parsed("A/contract.docx")
	rep := CheckContract(d)
	checkSources(t, []model.ParsedDoc{d}, rep.Findings)
	yellow := ruleSet(rep.Findings, model.Yellow)
	for _, r := range append(templateRules, "acc_number_required") {
		if !yellow[r] {
			t.Errorf("нет жёлтого %s; есть %v", r, keys(yellow))
		}
	}
	if red := ruleSet(rep.Findings, model.Red); len(red) != 0 {
		t.Errorf("красных быть не должно: %v", keys(red))
	}
	by := byRule(rep.Findings)
	if !strings.HasSuffix(by["bilingual_numbering_gap"].ID, ":12.6") {
		t.Error(by["bilingual_numbering_gap"].ID)
	}
	lr := by["bilingual_length_ratio"]
	if !strings.HasSuffix(lr.ID, ":3.1") || lr.Sources[0].Quote != "Количество пролонгаций не ограничено." ||
		!strings.Contains(lr.Detail, "12.9") || !strings.Contains(lr.Detail, "Количество пролонгаций не ограничено") ||
		strings.Contains(lr.Suggestion.RU, "Количество пролонгаций") || lr.Suggestion.Clause != "3.1" {
		t.Errorf("%+v", lr)
	}
	cp := by["clause_consent_party"]
	if cp.Sources[0].Quote != "without the Seller's prior written consent" || cp.Sources[1].Quote != "согласия Продавца" ||
		!strings.Contains(cp.Suggestion.EN, "Buyer's prior written consent") || !strings.Contains(cp.Suggestion.RU, "согласия Покупателя") {
		t.Errorf("%+v", cp)
	}
	aw := by["clause_air_waybill"]
	if aw.Sources[0].Quote != "Bill of Lading" || !strings.Contains(aw.Suggestion.EN, "Air Waybill") || !strings.Contains(aw.Suggestion.RU, "авиагрузовой накладной") {
		t.Errorf("%+v", aw)
	}
	fe := by["fees_not_specified"]
	if fe.Field != "feeType" || fe.Suggestion.Clause != "4.7" || !strings.Contains(fe.Suggestion.EN, "Bank charges") {
		t.Errorf("%+v", fe)
	}
	pt := by["payment_term_missing"]
	if pt.Suggestion.Clause != "4.2" || !strings.Contains(pt.Suggestion.EN, "within 10 (ten) banking days") {
		t.Errorf("%+v", pt)
	}
	acc := by["acc_number_required"]
	if !acc.MentorCheck || acc.Sources[0].Quote != "USD 51,000.00" || !strings.Contains(acc.Detail, "51 000,00 USD") || !strings.Contains(acc.Title, "учётный номер контракта") {
		t.Errorf("%+v", acc)
	}
	if g := ruleSet(rep.Findings, model.Green); !g["iban_checksum"] || !g["swift_format"] {
		t.Error("зелёные отметки о реквизитах")
	}
	for _, f := range rep.Findings {
		if f.Stage != model.StageContract {
			t.Errorf("%s: этап %s", f.ID, f.Stage)
		}
	}
}

func TestCrossReferenceRule(t *testing.T) {
	d := testkit.Contract{}.Parsed("A/contract.docx")
	f := byRule(CheckContract(d).Findings)["clause_cross_reference"]
	if f.ID != "clause_cross_reference:A/contract.docx:11.1" || f.Severity != model.Yellow || f.Stage != model.StageContract {
		t.Fatalf("%+v", f)
	}
	if f.Sources[0].Quote != "in accordance with clause 10" || f.Sources[1].Quote != "9. FORCE MAJEURE" || f.Sources[2].Quote != "в соответствии с пунктом 10" {
		t.Errorf("источники: %+v", f.Sources)
	}
	s := f.Suggestion
	if s == nil || s.Clause != "11.1" || !strings.HasSuffix(s.EN, "in accordance with Article 9.") || !strings.HasSuffix(s.RU, "в соответствии со статьёй 9.") ||
		!strings.HasPrefix(s.EN, "This Contract may be terminated:") {
		t.Errorf("исправление: %+v", s)
	}
	for _, w := range []string{"статье 9 «Форс-мажор»", "Статья 10 называется «Применимое право и споры»", "исправить ссылку на статью 9"} {
		if !strings.Contains(f.Detail, w) {
			t.Errorf("в тексте нет %q: %s", w, f.Detail)
		}
	}
}

func TestDuplicateRule(t *testing.T) {
	f := byRule(CheckContract(testkit.Contract{}.Parsed("A/contract.docx")).Findings)["clause_duplicate"]
	if f.ID != "clause_duplicate:A/contract.docx:4.2-4.3" || len(f.Sources) != 4 ||
		!strings.HasPrefix(f.Sources[0].Quote, "4.2. ") || !strings.HasPrefix(f.Sources[1].Quote, "4.3. ") {
		t.Fatalf("%+v", f)
	}
	if f.Suggestion == nil || f.Suggestion.Clause != "4.3" || f.Suggestion.EN != payDateEN || f.Suggestion.RU != payDateRU {
		t.Errorf("%+v", f.Suggestion)
	}
	a := wordSet("The Buyer shall pay by bank transfer within the period specified in the Seller's Invoice.")
	b := wordSet("Payment shall be made by bank transfer within the term indicated in the Seller's Invoice.")
	if j := jaccard(a, b); j < DuplicateThreshold || j > 0.6 {
		t.Errorf("Jaccard %v", j)
	}
}

func TestFixedContractIsQuiet(t *testing.T) {
	d := testkit.Contract{Defects: testkit.NoDefects()}.Parsed("F/contract.docx")
	rep := CheckContract(d)
	if bad := ruleSet(rep.Findings, model.Red, model.Yellow); len(bad) != 1 || !bad["acc_number_required"] {
		t.Fatalf("на исправленном договоре: %v", keys(bad))
	}
	greens := ruleSet(rep.Findings, model.Green)
	if len(greens) < 1 || len(greens) > 3 {
		t.Errorf("зелёных %v", keys(greens))
	}
	for _, p := range rep.Clauses {
		if len(p.Issues) != 0 {
			t.Errorf("%s: %v", p.Num, p.Issues)
		}
	}
}

func TestEachDefectFlagIsolated(t *testing.T) {
	flagRule := map[string]string{"extra_ru_31": "bilingual_length_ratio", "no_payment_term": "payment_term_missing",
		"no_fees_clause": "fees_not_specified", "bill_of_lading_54": "clause_air_waybill", "consent_seller_82": "clause_consent_party",
		"skip_126": "bilingual_numbering_gap", "dup_42_43": "clause_duplicate", "cross_ref_111d": "clause_cross_reference"}
	for flag, rule := range flagRule {
		fl := testkit.NoDefects()
		fl[flag] = true
		rep := CheckContract(testkit.Contract{Defects: fl}.Parsed("x/contract.docx"))
		got := ruleSet(rep.Findings, model.Yellow, model.Red)
		delete(got, "acc_number_required")
		if flag == "dup_42_43" {
			// 4.3 повторяет дефектный 4.2 («в срок, указанный в Инвойсе»), поэтому проверяем вместе с no_payment_term
			fl["no_payment_term"] = true
			got = ruleSet(CheckContract(testkit.Contract{Defects: fl}.Parsed("x/contract.docx")).Findings, model.Yellow, model.Red)
			delete(got, "acc_number_required")
			delete(got, "payment_term_missing")
		}
		if len(got) != 1 || !got[rule] {
			t.Errorf("%s: ждали только %s, есть %v", flag, rule, keys(got))
		}
	}
}

func TestNumberMismatch(t *testing.T) {
	rep := CheckContract(testkit.Contract{RUAmountOverride: "15 000,00", Defects: testkit.NoDefects()}.Parsed("x/contract.docx"))
	var f *model.Finding
	for i := range rep.Findings {
		if rep.Findings[i].Rule == "bilingual_number_mismatch" {
			f = &rep.Findings[i]
		}
	}
	if f == nil || !strings.HasSuffix(f.ID, ":2.2") || !strings.Contains(f.Detail, "15 000") || !strings.Contains(f.Detail, "51 000") || !strings.Contains(f.Detail, "12.9") {
		t.Fatalf("%+v", f)
	}
	for _, p := range rep.Clauses {
		if p.Num == "2.2" && (len(p.Issues) != 1 || p.Issues[0] != "bilingual_number_mismatch") {
			t.Errorf("%v", p.Issues)
		}
	}
}

func TestEURThresholdAndBelow(t *testing.T) {
	f := byRule(CheckContract(testkit.BContract().Parsed("B/contract.docx")).Findings)["acc_number_required"]
	if !strings.Contains(f.Detail, "48 000 EUR это примерно 52 094 USD") || !f.MentorCheck {
		t.Errorf("%s", f.Detail)
	}
	below := CheckContract(testkit.Contract{AmountEN: "40,000.00", AmountRU: "40 000,00", Defects: testkit.NoDefects()}.Parsed("x/contract.docx"))
	if ruleSet(below.Findings)["acc_number_required"] {
		t.Error("40 000 USD ниже порога")
	}
}

func TestRawTemplatePlaceholders(t *testing.T) {
	d := testkit.RawContract().Parsed("raw/contract.docx")
	rep := CheckContract(d)
	checkSources(t, []model.ParsedDoc{d}, rep.Findings)
	var ph []model.Finding
	for _, f := range rep.Findings {
		if f.Rule == "placeholder" {
			ph = append(ph, f)
		}
	}
	if len(ph) != 1 || ph[0].Severity != model.Red {
		t.Fatalf("%+v", ph)
	}
	quotes := map[string]bool{}
	for _, s := range ph[0].Sources {
		quotes[s.Quote] = true
	}
	for _, q := range []string{"[CONTRACT No.]", "[SELLER NAME]", "[ACCOUNT / IBAN]", "[SWIFT CODE]", "[BIC]"} {
		if !quotes[q] {
			t.Errorf("нет %s", q)
		}
	}
	all := ruleSet(rep.Findings)
	for _, r := range templateRules {
		if !all[r] {
			t.Errorf("нет %s", r)
		}
	}
}

func TestContractFromRealDOCXParserMatches(t *testing.T) {
	// содержимое отчёта не зависит от того, собран ParsedDoc в testkit или разобран парсером (см. parse тесты)
	d := testkit.Contract{}.Parsed("A/contract.docx")
	if len(CheckContract(d).Clauses) < 50 {
		t.Error("мало пунктов")
	}
}
