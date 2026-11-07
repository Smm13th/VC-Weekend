package rules

import (
	"fmt"
	"regexp"
	"strings"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/refdata"
)

// IBANCheck — результат проверки IBAN.
type IBANCheck struct {
	OK         bool     `json:"ok"`
	Normalized string   `json:"normalized"`
	Country    string   `json:"country,omitempty"`
	Error      string   `json:"error,omitempty"` // format | length | checksum
	Hint       string   `json:"hint,omitempty"`
	Candidates []string `json:"candidates"`
}

const alnum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const digits = "0123456789"

func normalizeIBAN(s string) string { return du.NormalizeIBAN(s) }

// FormatIBAN группирует по 4 символа.
func FormatIBAN(s string) string {
	n := []rune(normalizeIBAN(s))
	var parts []string
	for i := 0; i < len(n); i += 4 {
		e := i + 4
		if e > len(n) {
			e = len(n)
		}
		parts = append(parts, string(n[i:e]))
	}
	return strings.Join(parts, " ")
}

// MaskIBAN: 'AE98 **** 2345'.
func MaskIBAN(s string) string {
	n := normalizeIBAN(s)
	if len([]rune(n)) < 8 {
		return n
	}
	r := []rune(n)
	return string(r[:4]) + " **** " + string(r[len(r)-4:])
}

// mod97OK — контрольная сумма ISO 13616 по кускам, без больших чисел.
func mod97OK(n string) bool {
	if len(n) < 5 {
		return false
	}
	r := n[4:] + n[:4]
	rem := 0
	for _, c := range r {
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
			rem = (rem*10 + v) % 97
			continue
		case c >= 'A' && c <= 'Z':
			v = int(c-'A') + 10
		case c >= 'a' && c <= 'z':
			v = int(c-'a') + 10
		default:
			return false
		}
		rem = (rem*100 + v) % 97
	}
	return rem == 1
}

func countryInfo(code string) *refdata.Country {
	for _, c := range refdata.Countries() {
		if c.Code == code {
			cc := c
			return &cc
		}
	}
	return nil
}

func diffHint(n, refn, label, refLabel string) string {
	if len(n) != len(refn) || n == refn {
		return ""
	}
	var diff []int
	for i := 0; i < len(n); i++ {
		if n[i] != refn[i] {
			diff = append(diff, i)
		}
	}
	if len(diff) == 0 {
		return ""
	}
	a, b := diff[0], diff[len(diff)-1]
	here, there := n[a:b+1], refn[a:b+1]
	pos := fmt.Sprint(a + 1)
	word := "символ"
	if a != b {
		pos = fmt.Sprintf("%d-%d", a+1, b+1)
		word = "символы"
	}
	swap := len(diff) == 2 && b == a+1 && n[a] == refn[b] && n[b] == refn[a]
	tail := ""
	if swap {
		tail = ", похоже на перестановку соседних символов"
	}
	return fmt.Sprintf(`%s %s %s "%s", %s "%s"%s`, label, pos, word, here, refLabel, there, tail)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// typoCandidates — перестановки соседних символов и одиночные замены с верной контрольной суммой.
func typoCandidates(n string, limit int) []string {
	out := []string{}
	add := func(c string) bool {
		if c != n && !containsStr(out, c) && mod97OK(c) {
			out = append(out, c)
		}
		return len(out) >= limit
	}
	b := []byte(n)
	for i := 4; i < len(b)-1; i++ {
		if b[i] != b[i+1] {
			c := append([]byte{}, b...)
			c[i], c[i+1] = c[i+1], c[i]
			if add(string(c)) {
				return out
			}
		}
	}
	idx := []int{}
	for i := 4; i < len(b); i++ {
		idx = append(idx, i)
	}
	idx = append(idx, 2, 3)
	for _, i := range idx {
		if i >= len(b) {
			continue
		}
		alpha := alnum
		if isDigit(b[i]) {
			alpha = digits
		}
		for k := 0; k < len(alpha); k++ {
			ch := alpha[k]
			if ch != b[i] {
				c := append([]byte{}, b...)
				c[i] = ch
				if add(string(c)) {
					return out
				}
			}
		}
	}
	return out
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

var ibanShape = regexp.MustCompile(`^[A-Z]{2}\d{2}[A-Z0-9]{8,30}$`)

// ValidateIBAN проверяет формат, длину по стране и контрольную сумму; reference — верный IBAN для подсказки.
func ValidateIBAN(s, reference string) IBANCheck {
	return validateIBAN(s, reference, "в инвойсе", "в договоре")
}

func validateIBAN(s, reference, label, refLabel string) IBANCheck {
	n := normalizeIBAN(s)
	res := IBANCheck{Normalized: n, Candidates: []string{}}
	if r := []rune(n); len(r) >= 2 {
		res.Country = string(r[:2])
	}
	if !ibanShape.MatchString(n) {
		res.Error = "format"
		res.Hint = "IBAN начинается с двух латинских букв кода страны и двух цифр, дальше только " +
			"латинские буквы и цифры, всего от 12 до 34 символов"
		return res
	}
	if info := countryInfo(n[:2]); info != nil {
		if info.IbanLength == nil {
			res.Error = "format"
			res.Hint = fmt.Sprintf("в стране %s IBAN не используется, нужен номер счёта и код банка", info.Name)
			return res
		}
		if len(n) != *info.IbanLength {
			res.Error = "length"
			res.Hint = fmt.Sprintf("для страны %s IBAN состоит из %d символов, а здесь %d", info.Name, *info.IbanLength, len(n))
			return res
		}
	}
	if !mod97OK(n) {
		res.Error = "checksum"
		refn := ""
		if reference != "" {
			refn = normalizeIBAN(reference)
		}
		hint := ""
		if refn != "" && mod97OK(refn) {
			hint = diffHint(n, refn, label, refLabel)
		}
		if hint != "" {
			res.Hint = hint
			res.Candidates = []string{refn}
		} else {
			res.Candidates = typoCandidates(n, 3)
			res.Hint = "контрольные цифры не сходятся, в номере опечатка"
			if len(res.Candidates) > 0 {
				var f []string
				for _, c := range res.Candidates {
					f = append(f, FormatIBAN(c))
				}
				res.Hint += "; возможно, имелось в виду " + strings.Join(f, ", ")
			}
		}
		return res
	}
	res.OK = true
	return res
}

// looksLikeTypo: a и b отличаются в 1-2 символах.
func looksLikeTypo(a, b string) bool {
	a, b = normalizeIBAN(a), normalizeIBAN(b)
	ra, rb := []rune(a), []rune(b)
	if len(ra) != len(rb) || a == b {
		return false
	}
	d := 0
	for i := range ra {
		if ra[i] != rb[i] {
			d++
		}
	}
	return d <= 2
}
