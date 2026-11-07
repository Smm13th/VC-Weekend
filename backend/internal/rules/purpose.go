package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const maxPurpose = 140

func purposeAllowed(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		strings.ContainsRune(" /-?:().,'+", r)
}

var translitMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "",
	'э': "e", 'ю': "yu", 'я': "ya", 'қ': "k", 'ғ': "g", 'ү': "u", 'ұ': "u", 'ң': "n", 'ө': "o", 'һ': "h",
	'і': "i", 'ә': "a", '№': "NO", '«': "", '»': "", '"': "", '_': " ", '&': " AND ", ';': ",", '#': "NO ",
}

func translit(s string) string {
	var b strings.Builder
	for _, ch := range s {
		if t, ok := translitMap[unicode.ToLower(ch)]; ok {
			if unicode.IsUpper(ch) {
				t = strings.ToUpper(t)
			}
			b.WriteString(t)
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

var multiWS = rx(`\s+`)

func sanitize(s string) string {
	s = translit(s)
	var b strings.Builder
	for _, ch := range s {
		if purposeAllowed(ch) {
			b.WriteRune(ch)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.TrimSpace(multiWS.ReplaceAllString(b.String(), " "))
}

// PurposeIssue — нарушение правил назначения перевода.
type PurposeIssue struct {
	Rule      string `json:"rule"` // purpose_latin | purpose_length
	Message   string `json:"message"`
	Positions []int  `json:"positions"` // индексы в рунах
}

// PurposeCheck — результат проверки назначения.
type PurposeCheck struct {
	OK     bool           `json:"ok"`
	Length int            `json:"length"`
	Issues []PurposeIssue `json:"issues"`
}

// pyRepr — repr() одного символа, как в Python.
func pyRepr(c rune) string {
	switch c {
	case '\'':
		return `"'"`
	case '\\':
		return `'\\'`
	case '\n':
		return `'\n'`
	case '\t':
		return `'\t'`
	case '\r':
		return `'\r'`
	}
	if unicode.IsPrint(c) {
		return "'" + string(c) + "'"
	}
	switch {
	case c < 0x100:
		return fmt.Sprintf(`'\x%02x'`, c)
	case c < 0x10000:
		return fmt.Sprintf(`'\u%04x'`, c)
	}
	return fmt.Sprintf(`'\U%08x'`, c)
}

// ValidatePurpose проверяет латиницу и длину не больше 140 символов.
func ValidatePurpose(text string) PurposeCheck {
	r := []rune(text)
	issues := []PurposeIssue{}
	var bad []int
	var chars []rune
	for i, ch := range r {
		if !purposeAllowed(ch) {
			bad = append(bad, i)
			found := false
			for _, c := range chars {
				if c == ch {
					found = true
				}
			}
			if !found {
				chars = append(chars, ch)
			}
		}
	}
	if len(bad) > 0 {
		var rs []string
		for _, c := range chars {
			rs = append(rs, pyRepr(c))
		}
		issues = append(issues, PurposeIssue{Rule: "purpose_latin",
			Message: "Недопустимые символы: " + strings.Join(rs, " ") + ". Разрешены латинские " +
				"буквы, цифры, пробел и знаки / - ? : ( ) . , ' +", Positions: bad})
	}
	if len(r) > maxPurpose {
		pos := []int{}
		for i := maxPurpose; i < len(r); i++ {
			pos = append(pos, i)
		}
		issues = append(issues, PurposeIssue{Rule: "purpose_length",
			Message:   fmt.Sprintf("Назначение длиннее %d символов: %d. Лишнее банк обрежет.", maxPurpose, len(r)),
			Positions: pos})
	}
	if strings.TrimSpace(text) == "" {
		issues = append(issues, PurposeIssue{Rule: "purpose_length", Message: "Назначение перевода не заполнено.", Positions: []int{}})
	}
	return PurposeCheck{OK: len(issues) == 0, Length: len(r), Issues: issues}
}

// PurposeParams — данные для шаблона назначения.
type PurposeParams struct {
	InvoiceNo, InvoiceDate, ContractNo, ContractDate, Goods, AccountingNumber string
	Prepay                                                                    bool
}

func ddmm(d string) string {
	iso := parseDate(d)
	if iso == "" {
		return sanitize(d)
	}
	p := strings.Split(iso, "-")
	return p[2] + "." + p[1] + "." + p[0]
}

var reSpaces = regexp.MustCompile(`\s+`)

// BuildPurposeTemplate собирает назначение латиницей не длиннее 140 символов.
func BuildPurposeTemplate(p PurposeParams) string {
	head := "PAYMENT FOR"
	if p.Prepay {
		head = "PREPAYMENT FOR"
	}
	goods := p.Goods
	if goods == "" {
		goods = "GOODS"
	}
	goodsS := strings.ToUpper(sanitize(goods))
	var tailParts []string
	if p.InvoiceNo != "" {
		s := "INV " + strings.ToUpper(sanitize(p.InvoiceNo))
		if p.InvoiceDate != "" {
			s += " DD " + ddmm(p.InvoiceDate)
		}
		tailParts = append(tailParts, s)
	}
	if p.ContractNo != "" {
		s := "CONTRACT " + strings.ToUpper(sanitize(p.ContractNo))
		if p.ContractDate != "" {
			s += " DD " + ddmm(p.ContractDate)
		}
		tailParts = append(tailParts, s)
	}
	tail := strings.Join(tailParts, " ")
	un := ""
	if p.AccountingNumber != "" {
		un = "UN " + strings.ToUpper(sanitize(p.AccountingNumber))
	}
	compose := func(g string, withUN bool) string {
		var xs []string
		for _, x := range []string{strings.TrimSpace(head + " " + g), tail} {
			if x != "" {
				xs = append(xs, x)
			}
		}
		if withUN && un != "" {
			xs = append(xs, un)
		}
		return strings.TrimSpace(reSpaces.ReplaceAllString(strings.Join(xs, " "), " "))
	}
	modes := []bool{false}
	if un != "" {
		modes = []bool{true, false}
	}
	for _, withUN := range modes {
		s := compose(goodsS, withUN)
		if runeLen(s) <= maxPurpose {
			return s
		}
		words := strings.Fields(goodsS)
		for len(words) > 1 {
			words = words[:len(words)-1]
			s = compose(strings.Join(words, " "), withUN)
			if runeLen(s) <= maxPurpose {
				return s
			}
		}
		s = compose("GOODS", withUN)
		if runeLen(s) <= maxPurpose {
			return s
		}
	}
	s := []rune(compose("GOODS", false))
	if len(s) > maxPurpose {
		s = s[:maxPurpose]
	}
	return strings.TrimRight(string(s), " \t\n")
}
