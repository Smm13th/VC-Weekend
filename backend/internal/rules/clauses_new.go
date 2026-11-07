package rules

import (
	"regexp"
	"strconv"
	"strings"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
)

func atoiSafe(s string) int { v, _ := strconv.Atoi(strings.TrimSpace(s)); return v }
func itoa(v int) string     { return strconv.Itoa(v) }

// Темы, по которым проверяем ссылки на статьи: заголовок статьи и упоминание в строке пункта.
type refTopic struct {
	head, line string
	acc, prep  string // «условие про …», «правила о …»
}

var refTopics = []refTopic{
	{`FORCE\s+MAJEURE|ФОРС-МАЖОР|НЕПРЕОДОЛИМ`, `force\s+majeure|форс-мажор|непреодолим`, "форс-мажор", "форс-мажоре"},
	{`CONFIDENTIAL|КОНФИДЕНЦИАЛЬН`, `confidential|конфиденциальн`, "конфиденциальность", "конфиденциальности"},
	{`DISPUTES?|ARBITRATION|СПОР|АРБИТРАЖ`, `\bdisputes?\b|arbitration|споры|спора|арбитраж`, "споры", "спорах"},
}

var (
	refEN = rxi(`(?:(?:in\s+accordance\s+with|according\s+to|pursuant\s+to|as\s+per|under|see)\s+)?(?:the\s+)?` +
		`\b(?P<w>clause|article|section|paragraph|item)\s+(?P<n>\d{1,2})(?:\.(?P<s>\d{1,2}))?\b`)
	refRU = rxi(`(?:(?:в\s+соответствии\s+со?|согласно|по)\s+)?(?P<w>пункт\w*|стать\w*|раздел\w*)\s+(?P<n>\d{1,2})(?:\.(?P<s>\d{1,2}))?`)
)

type article struct {
	num    string
	pair   *model.ClausePair
	en, ru string // заголовок без номера
}

func articles(pairs []alignedPair) map[string]article {
	out := map[string]article{}
	for i := range pairs {
		p := pairs[i].p
		if strings.Contains(p.Num, ".") {
			continue
		}
		if _, ok := out[p.Num]; ok {
			continue
		}
		en, _ := body(p.EN)
		ru, _ := body(p.RU)
		out[p.Num] = article{num: p.Num, pair: p, en: strings.TrimSpace(en), ru: strings.TrimSpace(ru)}
	}
	return out
}

func (a article) matches(t refTopic) bool {
	return rxi(t.head).MatchString(a.en + " " + a.ru)
}

func (a article) title() string {
	h := a.ru
	if h == "" {
		h = a.en
	}
	return capitalize(strings.Split(h, "\n")[0])
}

type refHit struct {
	s, e   int // руны относительно начала ячейки
	ws, we int // слово + номер (то, что заменяем)
	word   string
	n      string
}

func findRefs(re *regexp.Regexp, text string) []refHit {
	var out []refHit
	for _, m := range du.FindAll(re, text) {
		ws, _ := du.Group(re, m, "w")
		ns, ne := du.Group(re, m, "n")
		_, se := du.Group(re, m, "s")
		we := ne
		if se >= 0 {
			we = se
		}
		out = append(out, refHit{s: m[0], e: m[1], ws: ws, we: we, word: strings.ToLower(du.RuneSub(text, ws, m[3])), n: du.RuneSub(text, ns, ne)})
	}
	return out
}

func lineAt(text string, pos int) (int, string) {
	r := []rune(text)
	s, e := pos, pos
	for s > 0 && r[s-1] != '\n' {
		s--
	}
	for e < len(r) && r[e] != '\n' {
		e++
	}
	idx := strings.Count(string(r[:s]), "\n")
	return idx, string(r[s:e])
}

func replaceRunes(text string, s, e int, with string) string {
	r := []rune(text)
	return string(r[:s]) + with + string(r[e:])
}

// «в соответствии с статьёй» -> «со статьёй».
var reSoFix = rx(`(^|\s)с(\s+)(стат)`)

func ruArticleWord(w string) string {
	switch {
	case strings.HasPrefix(w, "пунктом"):
		return "статьёй"
	case strings.HasPrefix(w, "пункта"):
		return "статьи"
	case strings.HasPrefix(w, "пункту"), strings.HasPrefix(w, "пункте"):
		return "статье"
	case strings.HasPrefix(w, "пункт"):
		return "статью"
	}
	return ""
}

// crossReferenceFindings — пункт ссылается на статью с другим смыслом (например, форс-мажор не в той статье).
func crossReferenceFindings(d *du.Doc, pairs []alignedPair) []model.Finding {
	var out []model.Finding
	arts := articles(pairs)
	for i := range pairs {
		p := pairs[i].p
		if !strings.Contains(p.Num, ".") || p.EN == nil {
			continue
		}
		cell := p.EN.Quote
		for _, h := range findRefs(refEN, cell) {
			li, line := lineAt(cell, h.s)
			var topics []refTopic
			for _, t := range refTopics {
				if rxi(t.line).MatchString(line) {
					topics = append(topics, t)
				}
			}
			if len(topics) == 0 {
				continue
			}
			target, hasTarget := arts[h.n]
			ok := false
			for _, t := range topics {
				if hasTarget && target.matches(t) {
					ok = true
				}
			}
			if ok {
				continue
			}
			// статья, где эта тема описана на самом деле
			var right *article
			var topic refTopic
			for _, t := range topics {
				for _, num := range sortedArticleNums(arts) {
					a := arts[num]
					if a.matches(t) {
						aa := a
						right, topic = &aa, t
						break
					}
				}
				if right != nil {
					break
				}
			}
			if right == nil {
				continue
			}
			// та же ссылка в русской колонке (в той же строке ячейки, если она есть)
			var ruRef *model.SourceRef
			ruFix := ""
			if p.RU != nil {
				rcell := p.RU.Quote
				for _, rh := range findRefs(refRU, rcell) {
					rli, _ := lineAt(rcell, rh.s)
					if rh.n != h.n || (rli != li && strings.Count(rcell, "\n") == strings.Count(cell, "\n")) {
						continue
					}
					ruRef = spanRef(d, p.RU, rh.s, rh.e)
					with := right.num
					if w := ruArticleWord(rh.word); w != "" {
						with = w + " " + right.num
					} else {
						with = du.RuneSub(rcell, rh.ws, rh.ws+runeLen(rh.word)) + " " + right.num
					}
					fixed := replaceRunes(rcell, rh.ws, rh.we, with)
					fixed = reSoFix.ReplaceAllString(fixed, "${1}со${2}${3}")
					_, off := clauseNum(fixed)
					ruFix = du.RuneSub(fixed, off, runeLen(fixed))
					break
				}
				if ruFix == "" {
					ruFix, _ = body(p.RU)
				}
			}
			enWith := "Article " + right.num
			refWord := "пункт"
			switch h.word {
			case "article":
				enWith = du.RuneSub(cell, h.ws, h.ws+runeLen(h.word)) + " " + right.num
				refWord = "статью"
			case "section":
				enWith = du.RuneSub(cell, h.ws, h.ws+runeLen(h.word)) + " " + right.num
				refWord = "раздел"
			}
			fixed := replaceRunes(cell, h.ws, h.we, enWith)
			_, off := clauseNum(fixed)
			enFix := du.RuneSub(fixed, off, runeLen(fixed))
			refNum := du.RuneSub(cell, h.ws, h.we)
			refNum = strings.TrimSpace(refNum[strings.IndexAny(refNum, "0123456789"):])
			wrong := "Статьи " + h.n + " в договоре нет."
			if hasTarget {
				wrong = "Статья " + h.n + " называется «" + target.title() + "»."
			}
			detail := "В пункте " + p.Num + " условие про " + topic.acc + " ссылается на " + refWord + " " + refNum + ". " +
				"Правила о " + topic.prep + " в этом договоре собраны в статье " + right.num + " «" + right.title() + "». " +
				wrong + " Если дойдёт до спора, по такой ссылке трудно понять, какие правила применять, и сторона сможет " +
				"толковать условие в свою пользу. Попросите поставщика исправить ссылку на статью " + right.num + "."
			srcs := []*model.SourceRef{spanRef(d, p.EN, h.s, h.e), right.pair.EN, ruRef}
			if right.pair.EN == nil {
				srcs[1] = right.pair.RU
			}
			out = append(out, newFinding("clause_cross_reference", d.ID, model.StageContract, model.Yellow,
				"Пункт "+p.Num+": ссылка ведёт не в ту статью", detail, srcs,
				fopt{extra: p.Num, suggestion: &model.Suggestion{Clause: p.Num, EN: enFix, RU: ruFix}}))
			addIssue(p, "clause_cross_reference")
			break
		}
	}
	return out
}

func sortedArticleNums(arts map[string]article) []string {
	var ns []int
	for k := range arts {
		ns = append(ns, atoiSafe(k))
	}
	for i := 1; i < len(ns); i++ {
		for j := i; j > 0 && ns[j] < ns[j-1]; j-- {
			ns[j], ns[j-1] = ns[j-1], ns[j]
		}
	}
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = itoa(n)
	}
	return out
}

var wordRe = regexp.MustCompile(`[\pL\pN]+`)

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		out[w] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

// DuplicateThreshold — порог сходства по множеству слов (Jaccard) английских текстов двух пунктов.
// Подобран на шаблоне: дубль 4.2/4.3 даёт 0,53, самая похожая честная пара 0,35.
const DuplicateThreshold = 0.45

// duplicateFindings — два пункта одной статьи почти дословно повторяют друг друга.
func duplicateFindings(d *du.Doc, pairs []alignedPair) []model.Finding {
	var out []model.Finding
	arts := articles(pairs)
	var cl []*model.ClausePair
	for i := range pairs {
		p := pairs[i].p
		if strings.Contains(p.Num, ".") && p.EN != nil {
			if b, _ := body(p.EN); runeLen(strings.TrimSpace(b)) > 40 {
				cl = append(cl, p)
			}
		}
	}
	for i := 0; i < len(cl); i++ {
		for j := i + 1; j < len(cl); j++ {
			a, b := cl[i], cl[j]
			art := strings.SplitN(a.Num, ".", 2)[0]
			if art != strings.SplitN(b.Num, ".", 2)[0] {
				continue
			}
			ba, _ := body(a.EN)
			bb, _ := body(b.EN)
			if jaccard(wordSet(ba), wordSet(bb)) < DuplicateThreshold {
				continue
			}
			quote := ""
			if b.RU != nil {
				quote, _ = body(b.RU)
			} else {
				quote = bb
			}
			// в статье об оплате на месте дубля обычно стоит условие о дате оплаты
			payArt := false
			if ar, ok := arts[art]; ok && rxi(`PAYMENT|ОПЛАТ`).MatchString(ar.en+" "+ar.ru) {
				payArt = true
			}
			hasPayDate := false
			for k := range pairs {
				p := pairs[k].p
				if strings.HasPrefix(p.Num, art+".") && rxi(`date\s+of\s+payment|датой\s+оплаты`).MatchString(quoteOf(p.EN)+quoteOf(p.RU)) {
					hasPayDate = true
				}
			}
			sug := &model.Suggestion{Clause: b.Num, EN: "Deleted by agreement of the Parties.", RU: "Исключён по соглашению Сторон."}
			hint := " Похоже, на месте пункта " + b.Num + " должно было стоять другое условие."
			if payArt && !hasPayDate {
				sug = &model.Suggestion{Clause: b.Num, EN: payDateEN, RU: payDateRU}
				hint += " В статье об оплате обычно ещё пишут, какой день считается днём оплаты, например день списания денег со счёта Покупателя."
			}
			detail := "Пункт " + b.Num + " почти дословно повторяет пункт " + a.Num + ": «" + strings.TrimRight(strings.TrimSpace(quote), ".") + "». " +
				"Если позже поправят только один из них, условия начнут противоречить друг другу, и каждая сторона " +
				"сошлётся на удобный ей пункт." + hint + " Попросите поставщика убрать повтор."
			out = append(out, newFinding("clause_duplicate", d.ID, model.StageContract, model.Yellow,
				"Пункты "+a.Num+" и "+b.Num+" повторяют друг друга", detail,
				[]*model.SourceRef{a.EN, b.EN, a.RU, b.RU}, fopt{extra: a.Num + "-" + b.Num, suggestion: sug}))
			addIssue(a, "clause_duplicate")
			addIssue(b, "clause_duplicate")
		}
	}
	return out
}
