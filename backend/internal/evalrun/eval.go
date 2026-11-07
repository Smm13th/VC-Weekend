// Package evalrun executes the checked-in synthetic quality suite.
package evalrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"vedhelper/internal/model"
	"vedhelper/internal/parse"
	"vedhelper/internal/rules"
)

type Case struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Files            []string          `json:"files"`
	Payments         []model.Payment   `json:"payments"`
	AccountingNumber string            `json:"accountingNumber"`
	ValueDate        string            `json:"valueDate"`
	Today            string            `json:"today"`
	Form             map[string]string `json:"form"`
	Task             map[string]string `json:"task"`
	ExpectedRules    []string          `json:"expectedRules"`
}
type suite struct {
	Cases []Case `json:"cases"`
}
type Result struct {
	Cases, Passed, TruePositive, FalsePositive, FalseNegative int
	Precision, Recall                                         float64
}

// Run reads cases.json and compares expected rule identifiers to findings.
func Run(root string) (Result, error) {
	b, err := os.ReadFile(filepath.Join(root, "eval", "cases.json"))
	if err != nil {
		return Result{}, err
	}
	var s suite
	if err = json.Unmarshal(b, &s); err != nil {
		return Result{}, err
	}
	res := Result{Cases: len(s.Cases)}
	for _, tc := range s.Cases {
		form := map[string]model.FieldState{}
		for key, value := range tc.Form {
			form[key] = model.FieldState{Value: value, Status: "user", Confirmed: true}
		}
		input := model.CaseInput{Payments: tc.Payments, AccountingNumber: tc.AccountingNumber, ValueDate: tc.ValueDate, Today: tc.Today, Form: form, Task: tc.Task}
		for _, file := range tc.Files {
			data, e := os.ReadFile(filepath.Join(root, "eval", "docs", file))
			if e != nil {
				return res, fmt.Errorf("%s: %w", file, e)
			}
			doc, e := parse.ParseFile(data, filepath.Base(file), file)
			if e != nil {
				return res, fmt.Errorf("%s: %w", file, e)
			}
			input.Docs = append(input.Docs, doc)
		}
		report := rules.CheckPayment(input)
		want := set(tc.ExpectedRules)
		got := map[string]bool{}
		for _, f := range report.Findings {
			if f.Severity != model.Green {
				got[f.Rule] = true
			}
		}
		ok := true
		for rule := range want {
			if got[rule] {
				res.TruePositive++
			} else {
				res.FalseNegative++
				ok = false
			}
		}
		for rule := range got {
			if want[rule] {
				continue
			}
			res.FalsePositive++
			ok = false
		}
		if ok {
			res.Passed++
		} else {
			fmt.Fprintf(os.Stderr, "%s: missing=%v unexpected=%v\n", tc.ID, difference(want, got), difference(got, want))
		}
	}
	if res.TruePositive+res.FalsePositive > 0 {
		res.Precision = float64(res.TruePositive) / float64(res.TruePositive+res.FalsePositive)
	}
	if res.TruePositive+res.FalseNegative > 0 {
		res.Recall = float64(res.TruePositive) / float64(res.TruePositive+res.FalseNegative)
	}
	return res, nil
}
func set(in []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range in {
		m[s] = true
	}
	return m
}
func difference(a, b map[string]bool) []string {
	var out []string
	for s := range a {
		if !b[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
