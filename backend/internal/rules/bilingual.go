package rules

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
)

// Номер пункта в начале ячейки: '2.2. Текст' -> '2.2'. Группа 2 эмулирует просмотр вперёд (?=\s|$).
var numPrefix = regexp.MustCompile(du.Py(`^\s*(\d{1,2}(?:\.\d{1,2})?)\.?(\s|$)`))

// Числа: группы 1 и 2 эмулируют (?!\d) из Python.
var numsRe = regexp.MustCompile(du.Py(`\d{1,3}(?:[ \x{00A0}\x{202F}]\d{3})+(?:,\d{1,2})?(\D|$)|\d{1,3}(?:,\d{3})+(?:\.\d{1,2})?(\D|$)|\d+(?:[.,]\d+)?`))

// Конец предложения: знак, за которым пробел и заглавная буква (эмуляция просмотра вперёд).
var sentRe = regexp.MustCompile(du.Py(`[.!?;](\s+[A-ZА-ЯЁ«"(])`))

// clauseNum: номер пункта и длина префикса в рунах.
func clauseNum(text string) (string, int) {
	m := du.Find(numPrefix, text)
	if m == nil {
		return "", 0
	}
	end := m[4]
	r := []rune(text)
	for end < len(r) && (r[end] == ' ' || r[end] == '\t') {
		end++
	}
	return string(r[m[2]:m[3]]), end
}

func fmtTokNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

// numbers — отсортированные числа и даты пункта ('D2026-12-31', '51000').
func numbers(text string) []string {
	toks := []string{}
	r := []rune(text)
	for _, m := range du.FindAll(du.DateRe, text) {
		s := string(r[m[0]:m[1]])
		if iso := du.ParseDate(s); iso != "" {
			toks = append(toks, "D"+iso)
		} else {
			toks = append(toks, s)
		}
		for i := m[0]; i < m[1]; i++ {
			r[i] = ' '
		}
	}
	t := string(r)
	tr := []rune(t)
	pos := 0
	for pos < len(tr) {
		rest := string(tr[pos:])
		m := du.Find(numsRe, rest)
		if m == nil {
			break
		}
		end := m[1]
		if m[2] >= 0 {
			end = m[2]
		} else if m[4] >= 0 {
			end = m[4]
		}
		tok := string(tr[pos+m[0] : pos+end])
		if v, ok := du.ParseAmount(tok); ok {
			toks = append(toks, fmtTokNum(v))
		}
		if end <= m[0] {
			end = m[0] + 1
		}
		pos += end
	}
	sort.Strings(toks)
	return toks
}

// sentenceEnds — позиции (в рунах) сразу после знаков конца предложения.
func sentenceEnds(text string) []int {
	var out []int
	for _, m := range du.FindAll(sentRe, text) {
		out = append(out, m[0]+1)
	}
	return out
}

func sentences(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return len(sentenceEnds(strings.TrimSpace(text))) + 1
}

func splitSentences(text string) []string {
	r := []rune(text)
	var parts []string
	last := 0
	for _, e := range sentenceEnds(text) {
		parts = append(parts, string(r[last:e]))
		last = e
	}
	parts = append(parts, string(r[last:]))
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// body — текст ячейки без номера пункта и смещение начала тела.
func body(r *model.SourceRef) (string, int) {
	if r == nil {
		return "", 0
	}
	_, off := clauseNum(r.Quote)
	return du.RuneSub(r.Quote, off, runeLen(r.Quote)), off
}

type alignedPair struct {
	p          *model.ClausePair
	nEN, nRU   string
	enBlk, ruB int
}

func alignDoc(d *du.Doc) []alignedPair {
	type cells struct{ en, ru int }
	rows := map[int]*cells{}
	var order []int
	for i, b := range d.Blocks {
		if b.Row == nil || b.Lang == "" {
			continue
		}
		c, ok := rows[*b.Row]
		if !ok {
			c = &cells{-1, -1}
			rows[*b.Row] = c
			order = append(order, *b.Row)
		}
		if b.Lang == "en" && c.en < 0 {
			c.en = i
		}
		if b.Lang == "ru" && c.ru < 0 {
			c.ru = i
		}
	}
	var out []alignedPair
	for _, r := range order {
		c := rows[r]
		var nEN, nRU string
		if c.en >= 0 {
			nEN, _ = clauseNum(d.BlockText(d.Blocks[c.en]))
		}
		if c.ru >= 0 {
			nRU, _ = clauseNum(d.BlockText(d.Blocks[c.ru]))
		}
		num := nEN
		if num == "" {
			num = nRU
		}
		if num == "" {
			continue
		}
		p := &model.ClausePair{Num: num, NumbersEN: []string{}, NumbersRU: []string{}, Issues: []string{}}
		if c.en >= 0 {
			p.EN = srcp(d.Ref(d.Blocks[c.en].Start, d.Blocks[c.en].End))
			b, _ := body(p.EN)
			p.NumbersEN = numbers(b)
		}
		if c.ru >= 0 {
			p.RU = srcp(d.Ref(d.Blocks[c.ru].Start, d.Blocks[c.ru].End))
			b, _ := body(p.RU)
			p.NumbersRU = numbers(b)
		}
		if p.EN != nil && p.RU != nil {
			be, _ := body(p.EN)
			br, _ := body(p.RU)
			if le := runeLen(be); le > 0 {
				p.Ratio = roundN(float64(runeLen(br))/float64(le), 3)
			}
		}
		out = append(out, alignedPair{p: p, nEN: nEN, nRU: nRU, enBlk: c.en, ruB: c.ru})
	}
	return out
}

// AlignClauses — пары пунктов EN/RU двуязычного договора.
func AlignClauses(doc model.ParsedDoc) []model.ClausePair {
	out := []model.ClausePair{}
	for _, a := range alignDoc(du.New(doc)) {
		out = append(out, *a.p)
	}
	return out
}

func quoteOf(r *model.SourceRef) string {
	if r == nil {
		return ""
	}
	return r.Quote
}

func prevailClause(pairs []alignedPair) string {
	for _, a := range pairs {
		t := quoteOf(a.p.EN) + " " + quoteOf(a.p.RU)
		if rxi(`prevail|приоритет|преимуществ\w+\s+имеет\s+текст`).MatchString(t) && rxi(`Russian|русск`).MatchString(t) {
			return a.p.Num
		}
	}
	return "12.9"
}

func fmtTok(t string) string {
	if strings.HasPrefix(t, "D") {
		p := strings.Split(t[1:], "-")
		if len(p) == 3 {
			return p[2] + "." + p[1] + "." + p[0]
		}
		return t
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return t
	}
	if v == math.Trunc(v) {
		return fmtNum(v, 0)
	}
	return fmtNum(v, 2)
}

func median(xs []float64) float64 {
	s := append([]float64{}, xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func addIssue(p *model.ClausePair, rule string) {
	for _, x := range p.Issues {
		if x == rule {
			return
		}
	}
	p.Issues = append(p.Issues, rule)
}

func onlyIn(a, b []string) []string {
	var out []string
	for _, t := range a {
		if !containsStr(b, t) {
			out = append(out, t)
		}
	}
	return out
}

func fmtToks(ts []string) string {
	var out []string
	for _, t := range ts {
		out = append(out, fmtTok(t))
	}
	return strings.Join(out, ", ")
}

func bilingualFindings(d *du.Doc, pairs []alignedPair, stage model.Stage) []model.Finding {
	var out []model.Finding
	if len(pairs) == 0 {
		return out
	}
	prev := prevailClause(pairs)
	prevNote := " По пункту " + prev + " при расхождении текстов приоритет у русского, поэтому разница может стоить денег."

	// 1. Пропуски нумерации в каждой колонке
	type miss struct {
		langs []string
		after *alignedPair
	}
	missing := map[string]*miss{}
	var missOrder []string
	addMiss := func(num, lang string, after *alignedPair) {
		m, ok := missing[num]
		if !ok {
			m = &miss{after: after}
			missing[num] = m
			missOrder = append(missOrder, num)
		}
		m.langs = append(m.langs, lang)
	}
	for li, lang := range []string{"en", "ru"} {
		type item struct {
			n string
			a *alignedPair
		}
		var seq []item
		for i := range pairs {
			a := &pairs[i]
			n := a.nEN
			has := a.p.EN != nil
			if li == 1 {
				n, has = a.nRU, a.p.RU != nil
			}
			if n != "" && has {
				seq = append(seq, item{n, a})
			}
		}
		type sub struct {
			k int
			a *alignedPair
		}
		arts := map[string][]sub{}
		var artOrder []string
		for _, it := range seq {
			art := strings.SplitN(it.n, ".", 2)[0]
			if _, ok := arts[art]; !ok {
				arts[art] = nil
				artOrder = append(artOrder, art)
			}
			if strings.Contains(it.n, ".") {
				k, _ := strconv.Atoi(strings.SplitN(it.n, ".", 2)[1])
				arts[art] = append(arts[art], sub{k, it.a})
			}
		}
		var ints []int
		for _, a := range artOrder {
			v, _ := strconv.Atoi(a)
			ints = append(ints, v)
		}
		for i := 0; i+1 < len(ints); i++ {
			x, y := ints[i], ints[i+1]
			for k := x + 1; k < y; k++ {
				var after *alignedPair
				for _, it := range seq {
					if it.n == strconv.Itoa(y) {
						after = it.a
						break
					}
				}
				if after == nil {
					for _, it := range seq {
						if strings.SplitN(it.n, ".", 2)[0] == strconv.Itoa(y) {
							after = it.a
							break
						}
					}
				}
				addMiss(strconv.Itoa(k), lang, after)
			}
		}
		for _, art := range artOrder {
			prevN := 0
			for _, s := range arts[art] {
				if s.k > prevN+1 {
					for m := prevN + 1; m < s.k; m++ {
						addMiss(art+"."+strconv.Itoa(m), lang, s.a)
					}
				}
				if s.k > prevN {
					prevN = s.k
				}
			}
		}
	}
	for _, num := range missOrder {
		info := missing[num]
		p := info.after.p
		both := len(info.langs) == 2
		where := "в русском тексте"
		if both {
			where = "в обеих колонках"
		} else if info.langs[0] == "en" {
			where = "в английском тексте"
		}
		var before string
		if i := strings.LastIndex(num, "."); i >= 0 {
			k, _ := strconv.Atoi(num[i+1:])
			before = num[:i] + "." + strconv.Itoa(k-1)
		} else {
			k, _ := strconv.Atoi(num)
			before = strconv.Itoa(k - 1)
		}
		first := p.RU
		if containsStr(info.langs, "en") || p.RU == nil {
			first = p.EN
		}
		var second *model.SourceRef
		if p.EN != nil {
			second = p.RU
		}
		out = append(out, newFinding("bilingual_numbering_gap", d.ID, stage, model.Yellow, "Пропущен пункт "+num,
			"После пункта "+before+" сразу идёт пункт "+p.Num+", пункта "+num+" нет "+where+". Возможно, при "+
				"редактировании потеряли условие. Уточните у поставщика, был ли такой пункт, или перенумеруйте "+
				"пункты, чтобы ссылки в договоре не вели в пустоту.",
			[]*model.SourceRef{first, second}, fopt{extra: num}))
	}

	// 2. Разные числа и даты в парах
	for i := range pairs {
		p := pairs[i].p
		if p.EN == nil || p.RU == nil {
			continue
		}
		if strings.Join(p.NumbersEN, "|") != strings.Join(p.NumbersRU, "|") {
			oEN, oRU := onlyIn(p.NumbersEN, p.NumbersRU), onlyIn(p.NumbersRU, p.NumbersEN)
			detail := "В пункте " + p.Num + " числа в двух языках не совпадают. "
			if len(oEN) > 0 {
				detail += "Только в английском тексте: " + fmtToks(oEN) + ". "
			}
			if len(oRU) > 0 {
				detail += "Только в русском тексте: " + fmtToks(oRU) + ". "
			}
			detail += "Попросите поставщика исправить одну из версий." + prevNote
			out = append(out, newFinding("bilingual_number_mismatch", d.ID, stage, model.Yellow,
				"Пункт "+p.Num+": в английском и русском тексте разные цифры", detail,
				[]*model.SourceRef{p.RU, p.EN}, fopt{extra: p.Num}))
			addIssue(p, "bilingual_number_mismatch")
		}
	}

	// 3. Длина: русский текст заметно длиннее или короче английского
	var cand []*model.ClausePair
	for i := range pairs {
		p := pairs[i].p
		if p.EN == nil || p.RU == nil || p.Ratio == 0 {
			continue
		}
		if b, _ := body(p.EN); runeLen(b) > 60 {
			cand = append(cand, p)
		}
	}
	if len(cand) >= 3 {
		var rs []float64
		for _, p := range cand {
			rs = append(rs, p.Ratio)
		}
		med := median(rs)
		for _, p := range cand {
			enBody, _ := body(p.EN)
			ruBody, ruOff := body(p.RU)
			expected := float64(runeLen(enBody)) * med
			excess := float64(runeLen(ruBody)) - expected
			sEN, sRU := sentences(enBody), sentences(ruBody)
			byRatio := (p.Ratio > med*1.3 || p.Ratio < med*0.7) && math.Abs(excess) > 25
			bySent := sEN != sRU && math.Abs(excess) > 25 && (excess > 0) == (sRU > sEN)
			if !(byRatio || bySent) {
				continue
			}
			longer := excess > 0
			sources := []*model.SourceRef{p.RU, p.EN}
			var sug *model.Suggestion
			extraText := ""
			if sRU > sEN && longer {
				parts := splitSentences(ruBody)
				if len(parts) > sEN {
					extra := parts[sEN:]
					extraText = strings.Join(extra, " ")
					from := p.RU.Start + ruOff
					if pos := runeIndex(d.R, []rune(extra[0]), from); pos >= 0 {
						end := pos + runeLen(extra[0])
						if d.Sub(pos, pos+runeLen(extraText)) == extraText {
							end = pos + runeLen(extraText)
						}
						sources = []*model.SourceRef{srcp(d.Ref(pos, end)), p.EN}
					}
					sug = &model.Suggestion{Clause: p.Num, EN: strings.TrimSpace(enBody), RU: strings.TrimSpace(strings.Join(parts[:sEN], " "))}
				}
			} else if sEN > sRU && !longer {
				parts := splitSentences(enBody)
				if len(parts) > sRU {
					extraText = strings.Join(parts[sRU:], " ")
					sug = &model.Suggestion{Clause: p.Num, EN: strings.TrimSpace(strings.Join(parts[:sRU], " ")), RU: strings.TrimSpace(ruBody)}
				}
			}
			what := "короче"
			if longer {
				what = "длиннее"
			}
			detail := "Русский текст пункта " + p.Num + " заметно " + what + " английского. Обычно в этом договоре русский " +
				"текст занимает около " + strconv.Itoa(int(math.RoundToEven(med*100))) + "% длины английского, а здесь " +
				strconv.Itoa(int(math.RoundToEven(p.Ratio*100))) + "%. "
			switch {
			case extraText != "" && longer:
				detail += "В русской версии есть фраза «" + strings.TrimRight(extraText, ".") + "», которой нет в английской. "
			case extraText != "":
				detail += "В английской версии есть фраза «" + strings.TrimRight(extraText, ".") + "», которой нет в русской. "
			default:
				detail += "Похоже, в одной из версий есть условие, которого нет в другой. "
			}
			detail += "Согласуйте с поставщиком одинаковый текст." + prevNote
			out = append(out, newFinding("bilingual_length_ratio", d.ID, stage, model.Yellow,
				"Пункт "+p.Num+": русский и английский текст не совпадают по смыслу", detail, sources,
				fopt{extra: p.Num, suggestion: sug}))
			addIssue(p, "bilingual_length_ratio")
		}
	}
	return out
}

func runeIndex(hay, needle []rune, from int) int {
	if len(needle) == 0 {
		return from
	}
	for i := from; i+len(needle) <= len(hay); i++ {
		match := true
		for k := range needle {
			if hay[i+k] != needle[k] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
