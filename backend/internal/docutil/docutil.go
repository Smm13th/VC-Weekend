// Package docutil — общие помощники для разбора, извлечения и правил:
// работа с позициями в рунах, цитаты SourceRef, разбор дат и сумм, нормализация IBAN/SWIFT.
// Вынесено отдельно, чтобы extract и rules не импортировали друг друга.
package docutil

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"vedhelper/internal/model"
)

// Doc — документ вместе с текстом в рунах (все позиции в рунах).
type Doc struct {
	model.ParsedDoc
	R []rune
}

func New(d model.ParsedDoc) *Doc { return &Doc{ParsedDoc: d, R: []rune(d.Text)} }

func (d *Doc) Len() int { return len(d.R) }

// Sub — срез текста по рунам с защитой границ.
func (d *Doc) Sub(s, e int) string {
	if s < 0 {
		s = 0
	}
	if e > len(d.R) {
		e = len(d.R)
	}
	if s >= e {
		return ""
	}
	return string(d.R[s:e])
}

func (d *Doc) BlockText(b model.DocBlock) string { return d.Sub(b.Start, b.End) }

// BlockAt — индекс блока, содержащего позицию (как block_at в Python), иначе последний блок, начавшийся до pos.
func (d *Doc) BlockAt(pos int) int {
	last := -1
	for i, b := range d.Blocks {
		if (b.Start <= pos && pos < b.End) || (b.Start == b.End && b.End == pos) {
			return i
		}
		if b.Start <= pos {
			last = i
		}
	}
	return last
}

// Ref — SourceRef с точной цитатой; пробелы по краям обрезаются.
func (d *Doc) Ref(start, end int) model.SourceRef {
	if start < 0 {
		start = 0
	}
	if end > len(d.R) {
		end = len(d.R)
	}
	for start < end && unicode.IsSpace(d.R[start]) {
		start++
	}
	for end > start && unicode.IsSpace(d.R[end-1]) {
		end--
	}
	r := model.SourceRef{DocID: d.ID, Start: start, End: end, Quote: d.Sub(start, end)}
	bi := d.BlockAt(start)
	if bi >= 0 {
		r.Block = bi
		r.Page = d.Blocks[bi].Page
	}
	return r
}

// LineSpan — строка текста внутри блока (строки ячеек тоже отдельные).
type LineSpan struct {
	Start, End int
	Block      int
}

func (d *Doc) Lines() []LineSpan {
	var out []LineSpan
	for bi, b := range d.Blocks {
		s := b.Start
		for _, line := range strings.Split(d.BlockText(b), "\n") {
			n := utf8.RuneCountInString(line)
			out = append(out, LineSpan{s, s + n, bi})
			s += n + 1
		}
	}
	return out
}

// ---------- регулярные выражения в рунах ----------

const spaceExtra = `\x{00A0}\x{202F}\x{2009}\x{2007}`

// Py переводит python-подобный шаблон в RE2: \s и \w становятся юникодными, как в Python 3.
func Py(p string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			n := p[i+1]
			i++
			switch n {
			case 's':
				if inClass {
					b.WriteString(`\s` + spaceExtra)
				} else {
					b.WriteString(`[\s` + spaceExtra + `]`)
				}
			case 'w':
				if inClass {
					b.WriteString(`\pL\pN_`)
				} else {
					b.WriteString(`[\pL\pN_]`)
				}
			default:
				b.WriteByte('\\')
				b.WriteByte(n)
			}
			continue
		}
		if c == '[' && !inClass {
			inClass = true
		} else if c == ']' && inClass {
			inClass = false
		}
		b.WriteByte(c)
	}
	return b.String()
}

func MustPy(p string) *regexp.Regexp { return regexp.MustCompile(Py(p)) }

func byteToRune(s string) []int {
	m := make([]int, len(s)+1)
	r := 0
	for i := range s {
		m[i] = r
		_, sz := utf8.DecodeRuneInString(s[i:])
		for k := 1; k < sz; k++ {
			m[i+k] = r
		}
		r++
	}
	m[len(s)] = r
	return m
}

func conv(idx []int, m []int) []int {
	out := make([]int, len(idx))
	for i, v := range idx {
		if v < 0 {
			out[i] = -1
		} else {
			out[i] = m[v]
		}
	}
	return out
}

// FindAll — все совпадения с подгруппами, индексы в рунах относительно s.
func FindAll(re *regexp.Regexp, s string) [][]int {
	idx := re.FindAllStringSubmatchIndex(s, -1)
	if len(idx) == 0 {
		return nil
	}
	m := byteToRune(s)
	out := make([][]int, len(idx))
	for i, x := range idx {
		out[i] = conv(x, m)
	}
	return out
}

// Find — первое совпадение (индексы в рунах) или nil.
func Find(re *regexp.Regexp, s string) []int {
	x := re.FindStringSubmatchIndex(s)
	if x == nil {
		return nil
	}
	return conv(x, byteToRune(s))
}

// Group — индексы именованной группы в совпадении m.
func Group(re *regexp.Regexp, m []int, name string) (int, int) {
	i := re.SubexpIndex(name)
	if i < 0 || 2*i+1 >= len(m) {
		return -1, -1
	}
	return m[2*i], m[2*i+1]
}

// RuneSub — срез строки по рунам.
func RuneSub(s string, a, b int) string {
	r := []rune(s)
	if a < 0 || b > len(r) || a >= b {
		return ""
	}
	return string(r[a:b])
}

func RuneLen(s string) int { return utf8.RuneCountInString(s) }

// ---------- нормализация и разбор ----------

var Placeholder = regexp.MustCompile(`\[[^\]]{2,80}\]`)

func HasPlaceholder(s string) bool { return Placeholder.MatchString(s) }

var ibanJunk = regexp.MustCompile(`[\s` + spaceExtra + `\-]`)
var swiftJunk = regexp.MustCompile(`[\s` + spaceExtra + `]`)

func NormalizeIBAN(s string) string  { return strings.ToUpper(ibanJunk.ReplaceAllString(s, "")) }
func NormalizeSwift(s string) string { return strings.ToUpper(swiftJunk.ReplaceAllString(s, "")) }

var nameJunk = regexp.MustCompile(`[^0-9a-zа-яё]+`)

func NormName(s string) string { return nameJunk.ReplaceAllString(strings.ToLower(s), "") }

var isoRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
var DateRe = regexp.MustCompile(`\b(\d{1,2})[./](\d{1,2})[./](\d{4})\b`)

// ParseDate: '15.09.2026' или '2026-09-15' -> '2026-09-15'; ” если не дата.
func ParseDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if isoRe.MatchString(s) {
		return s
	}
	m := DateRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	y, _ := strconv.Atoi(m[3])
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d || int(t.Month()) != mo || t.Year() != y {
		return ""
	}
	return t.Format("2006-01-02")
}

var numRe = regexp.MustCompile(`\d[\d\s` + spaceExtra + `,.']*`)
var numStrip = regexp.MustCompile(`[\s` + spaceExtra + `']`)
var negTail = regexp.MustCompile(`-\s*$`)

// ParseAmount: '51,000.00', '51 000,00', 'USD 51,000.00', '51.000,00', '51000' -> 51000.
func ParseAmount(s string) (float64, bool) {
	loc := numRe.FindStringIndex(s)
	if loc == nil {
		return 0, false
	}
	t := strings.TrimRight(numStrip.ReplaceAllString(s[loc[0]:loc[1]], ""), ".,")
	if t == "" {
		return 0, false
	}
	neg := negTail.MatchString(s[:loc[0]])
	hasC, hasD := strings.Contains(t, ","), strings.Contains(t, ".")
	switch {
	case hasC && hasD:
		dec, thou := ".", ","
		if strings.LastIndex(t, ",") > strings.LastIndex(t, ".") {
			dec, thou = ",", "."
		}
		t = strings.ReplaceAll(strings.ReplaceAll(t, thou, ""), dec, ".")
	case hasC:
		parts := strings.Split(t, ",")
		if len(parts) == 2 && (len(parts[1]) == 1 || len(parts[1]) == 2) {
			t = strings.ReplaceAll(t, ",", ".")
		} else {
			t = strings.ReplaceAll(t, ",", "")
		}
	case strings.Count(t, ".") > 1:
		t = strings.ReplaceAll(t, ".", "")
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// CollapseWS — схлопывает пробелы (как re.sub(r'\s+', ' ', v).strip()).
var wsRe = regexp.MustCompile(Py(`\s+`))

func CollapseWS(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

var nlRe = regexp.MustCompile(Py(`\s*\n\s*`))

// JoinLines — переносы строк заменяются пробелом (re.sub(r'\s*\n\s*', ' ', v)).
func JoinLines(s string) string { return nlRe.ReplaceAllString(s, " ") }
