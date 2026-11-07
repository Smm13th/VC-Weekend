package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vedhelper/internal/model"
	"vedhelper/internal/parse"
)

type sampleKit struct {
	ID               string          `json:"id"`
	Title            string          `json:"title"`
	Files            []string        `json:"files"`
	Payments         []model.Payment `json:"payments"`
	AccountingNumber *string         `json:"accountingNumber"`
	Expected         []string        `json:"expected"`
}

func samplesDir() string { return filepath.Join("..", "..", "samples") }

// loadSampleKits читает backend/samples/kits.json и разбирает файлы комплектов; без них тест пропускается.
func loadSampleKits(t *testing.T) ([]sampleKit, map[string][]model.ParsedDoc) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(samplesDir(), "kits.json"))
	if err != nil {
		t.Skip("нет backend/samples/kits.json")
	}
	var kits []sampleKit
	if err := json.Unmarshal(b, &kits); err != nil {
		t.Fatalf("kits.json: %v", err)
	}
	docs := map[string][]model.ParsedDoc{}
	for _, k := range kits {
		for _, f := range k.Files {
			data, err := os.ReadFile(filepath.Join(samplesDir(), filepath.FromSlash(f)))
			if err != nil {
				t.Skipf("нет файла комплекта %s", f)
			}
			d, err := parse.ParseFile(data, f, f)
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			docs[k.ID] = append(docs[k.ID], d)
		}
	}
	return kits, docs
}

func sampleCase(k sampleKit, docs []model.ParsedDoc) model.CaseInput {
	c := model.CaseInput{Docs: docs, Payments: k.Payments, ValueDate: "2026-11-09", Today: "2026-11-07"}
	if k.AccountingNumber != nil {
		c.AccountingNumber = *k.AccountingNumber
	}
	return c
}

// Сверка с реальными комплектами из backend/samples: сработавшие red/yellow правила == expected.
func TestSampleKits(t *testing.T) {
	kits, docs := loadSampleKits(t)
	for _, k := range kits {
		c := sampleCase(k, docs[k.ID])
		rep := CheckPayment(c)
		got := ruleSet(rep.Findings, model.Red, model.Yellow)
		want := set(k.Expected...)
		var missing, extra []string
		for r := range want {
			if !got[r] {
				missing = append(missing, r)
			}
		}
		for r := range got {
			if !want[r] {
				extra = append(extra, r)
			}
		}
		if len(missing)+len(extra) > 0 {
			t.Errorf("комплект %s: не сработали %v, лишние %v", k.ID, missing, extra)
		}
		checkSources(t, c.Docs, rep.Findings)
		for _, d := range c.Docs {
			if d.Kind == model.KindContract {
				checkSources(t, []model.ParsedDoc{d}, CheckContract(d).Findings)
			}
		}
		if len(rep.Form) != 25 || rep.Form["acceptTerms"].Value != "false" {
			t.Errorf("комплект %s: форма", k.ID)
		}
		for _, f := range rep.Findings {
			if strings.Contains(f.Detail, "�") {
				t.Errorf("%s: в тексте битые символы", f.ID)
			}
		}
	}
}
