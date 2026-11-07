// Package rules — детерминированные правила проверки договора, документов и платежа, предзаполнение формы.
package rules

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
	"vedhelper/internal/refdata"
)

// round2 — округление как round(x, 2) в Python (по точному двоичному значению).
func roundN(x float64, n int) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', n, 64), 64)
	return v
}
func round2(x float64) float64 { return roundN(x, 2) }

// fmtNum: 51000 -> '51 000,00'.
func fmtNum(x float64, decimals int) string {
	s := strconv.FormatFloat(math.Abs(x), 'f', decimals, 64)
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i+1:]
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if frac != "" {
		out += "," + frac
	}
	if x < 0 && strings.Trim(out, "0, ") != "" {
		out = "-" + out
	}
	return out
}

func fmtMoneyD(x float64, cur string, decimals int) string {
	s := fmtNum(x, decimals)
	if cur != "" {
		s += " " + cur
	}
	return s
}

func fmtMoney(x float64, cur string) string { return fmtMoneyD(x, cur, 2) }

// FormatMoney: 51000, 'USD' -> '51 000,00 USD'.
func FormatMoney(n float64, cur string) string { return fmtMoney(n, cur) }

func fmtDate(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("02.01.2006")
}

func pyFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func docLabel(d model.ParsedDoc) string {
	switch d.Kind {
	case model.KindContract:
		return "договоре"
	case model.KindInvoice:
		return "инвойсе"
	case model.KindLetter:
		return "письме"
	}
	return "документе " + d.Name
}

func docTitle(d model.ParsedDoc) string {
	switch d.Kind {
	case model.KindContract:
		return "договор"
	case model.KindInvoice:
		return "инвойс"
	case model.KindLetter:
		return "письмо"
	}
	return d.Name
}

// capitalize как str.capitalize в Python.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

func countryName(code string) string {
	for _, c := range refdata.Countries() {
		if c.Code == code {
			return c.Name
		}
	}
	if code == "" {
		return "?"
	}
	return code
}

func srcp(s model.SourceRef) *model.SourceRef { return &s }

type fopt struct {
	field       string
	extra       string
	suggestion  *model.Suggestion
	mentorCheck bool
}

func newFinding(rule, docID string, stage model.Stage, sev model.Severity, title, detail string,
	sources []*model.SourceRef, o fopt) model.Finding {
	id := rule + ":" + docID
	if o.extra != "" {
		id += ":" + o.extra
	}
	srcs := []model.SourceRef{}
	for _, s := range sources {
		if s != nil {
			srcs = append(srcs, *s)
		}
	}
	return model.Finding{ID: id, Rule: rule, Stage: stage, Severity: sev, Title: title, Detail: detail,
		Sources: srcs, Field: o.field, Suggestion: o.suggestion, MentorCheck: o.mentorCheck}
}

// ---------- регулярные выражения ----------

var rxCache sync.Map

// rxi — шаблон в стиле Python с флагами (например "i", "mi"); \s и \w юникодные.
func rxf(flags, p string) *regexp.Regexp {
	key := flags + "\x00" + p
	if r, ok := rxCache.Load(key); ok {
		return r.(*regexp.Regexp)
	}
	pre := ""
	if flags != "" {
		pre = "(?" + flags + ")"
	}
	r := regexp.MustCompile(pre + du.Py(p))
	rxCache.Store(key, r)
	return r
}
func rxi(p string) *regexp.Regexp { return rxf("i", p) }
func rx(p string) *regexp.Regexp  { return rxf("", p) }

// wordB — юникодная граница слова \b для начала (и конца) альтернатив.
const wbL = `(?:^|[^\pL\pN_])`
const wbR = `(?:$|[^\pL\pN_])`

func runeLen(s string) int { return utf8.RuneCountInString(s) }
