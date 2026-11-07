// Package extract — извлечение реквизитов по меткам (samples_spec.md). Каждое значение несёт точную цитату.
package extract

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	du "vedhelper/internal/docutil"
	"vedhelper/internal/model"
)

const (
	pDATE = `\d{1,2}\.\d{1,2}\.\d{4}`
	pNUM  = `\d{1,3}(?:[ \x{00A0}\x{202F},.]\d{3})*(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?`
	pCUR  = `USD|EUR|KZT|CNY|RUB|AED|GBP|CHF|TRY|GEL|JPY|UZS`
)

var pMONEY = `(?:(?P<cur>` + pCUR + `)\s?(?P<num>` + pNUM + `)|(?P<num2>` + pNUM + `)\s?(?P<cur2>` + pCUR + `))`

// ExtraKeys — ключи сверх model.ExtractKeys.
var ExtraKeys = []string{"intermediaryBank"}

var countryAliases = []struct {
	code  string
	names []string
}{
	{"AE", []string{"United Arab Emirates", "UAE", "U.A.E.", "ОАЭ", "Объединённые Арабские Эмираты", "Объединенные Арабские Эмираты"}},
	{"GE", []string{"Georgia", "Грузия"}},
	{"KZ", []string{"Republic of Kazakhstan", "Kazakhstan", "Казахстан"}},
	{"TR", []string{"Turkey", "Türkiye", "Турция"}},
	{"DE", []string{"Germany", "Германия"}},
	{"GB", []string{"United Kingdom", "UK", "Великобритания"}},
	{"CN", []string{"People's Republic of China", "China", "Китай", "КНР"}},
	{"US", []string{"United States", "USA", "США"}},
	{"RU", []string{"Russian Federation", "Russia", "Россия"}},
	{"UZ", []string{"Uzbekistan", "Узбекистан"}},
}

type countryRx struct {
	name string
	code string
	re   *regexp.Regexp
}

var countries = func() []countryRx {
	var out []countryRx
	for _, c := range countryAliases {
		for _, n := range c.names {
			parts := strings.Fields(n)
			for i, p := range parts {
				parts[i] = regexp.QuoteMeta(p)
			}
			out = append(out, countryRx{n, c.code, du.MustPy(strings.Join(parts, `\s+`))})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return du.RuneLen(out[i].name) > du.RuneLen(out[j].name) })
	return out
}()

func isLetterAZ(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= 'А' && r <= 'я')
}

// FindCountry -> код, начало и конец (в рунах) последнего упоминания страны.
func FindCountry(text string) (string, int, int, bool) {
	rs := []rune(text)
	code, bs, be, found := "", 0, 0, false
	for _, c := range countries {
		for _, m := range du.FindAll(c.re, text) {
			s, e := m[0], m[1]
			if s > 0 && isLetterAZ(rs[s-1]) {
				continue
			}
			if e < len(rs) && isLetterAZ(rs[e]) {
				continue
			}
			if !found || s > bs || (s == bs && e > be) {
				code, bs, be, found = c.code, s, e, true
			}
		}
	}
	return code, bs, be, found
}

type hit struct {
	s, e int
	re   *regexp.Regexp
	m    []int // руны относительно начала блока
	bt   string
	b    model.DocBlock
}

func (h hit) group(name string) string {
	a, b := du.Group(h.re, h.m, name)
	if a < 0 {
		return ""
	}
	return du.RuneSub(h.bt, a, b)
}

func (h hit) span(name string) (int, int) {
	a, b := du.Group(h.re, h.m, name)
	if a < 0 {
		return -1, -1
	}
	return h.b.Start + a, h.b.Start + b
}

type normFn func(v string, h hit) any

type ctx struct {
	d      *du.Doc
	fields map[string]model.Extracted
}

var rxCache sync.Map

func cached(key string, mk func() *regexp.Regexp) *regexp.Regexp {
	if r, ok := rxCache.Load(key); ok {
		return r.(*regexp.Regexp)
	}
	r := mk()
	rxCache.Store(key, r)
	return r
}

func rx(p string) *regexp.Regexp {
	return cached("mi:"+p, func() *regexp.Regexp { return regexp.MustCompile(`(?mi)` + du.Py(p)) })
}

func isTrimEnd(r rune) bool   { return r == ' ' || r == '\t' || r == ' ' || r == ';' }
func isTrimStart(r rune) bool { return r == ' ' || r == '\t' || r == ' ' }

func (c *ctx) scan(pattern string, blocks []model.DocBlock) []hit {
	re := rx(pattern)
	vi := re.SubexpIndex("v")
	var out []hit
	for _, b := range blocks {
		t := c.d.BlockText(b)
		for _, m := range du.FindAll(re, t) {
			if m[2*vi] < 0 {
				continue
			}
			s, e := b.Start+m[2*vi], b.Start+m[2*vi+1]
			for e > s && isTrimEnd(c.d.R[e-1]) {
				e--
			}
			for s < e && isTrimStart(c.d.R[s]) {
				s++
			}
			if s >= e {
				continue
			}
			out = append(out, hit{s: s, e: e, re: re, m: m, bt: t, b: b})
		}
	}
	return out
}

var junkVal = regexp.MustCompile(`^[_\s.\-]+$`)

func distinctKey(nv any, val string) string {
	switch v := nv.(type) {
	case float64:
		return fmt.Sprintf("f:%v", v)
	case string:
		return "s:" + strings.ToLower(du.CollapseWS(v))
	}
	return "s:" + strings.ToLower(du.CollapseWS(val))
}

// put берёт первое значение без плейсхолдера; разные значения -> conflict.
func (c *ctx) put(key string, hits []hit, norm normFn, conf, label string) bool {
	if _, ok := c.fields[key]; ok {
		return true
	}
	type good struct {
		s, e int
		val  string
		nv   any
	}
	var gs []good
	for _, h := range hits {
		val := c.d.Sub(h.s, h.e)
		if du.HasPlaceholder(val) || strings.TrimSpace(val) == "" || junkVal.MatchString(val) {
			continue
		}
		gs = append(gs, good{h.s, h.e, val, norm(val, h)})
	}
	if len(gs) == 0 {
		return false
	}
	g := gs[0]
	src := c.d.Ref(g.s, g.e)
	var distinct []string
	for _, x := range gs {
		k := distinctKey(x.nv, x.val)
		if !contains(distinct, k) {
			distinct = append(distinct, k)
		}
	}
	ex := model.Extracted{Key: key, Value: du.JoinLines(src.Quote), Norm: g.nv, Source: src, Confidence: conf, Status: "doc"}
	if len(distinct) > 1 {
		ex.Status = "conflict"
		var vals []string
		for _, x := range gs {
			v := wsAny.ReplaceAllString(x.val, " ")
			if !contains(vals, v) {
				vals = append(vals, v)
			}
		}
		what := "это поле"
		if label != "" {
			what = "метка «" + label + "»"
		}
		ex.Note = "В документе " + what + " встречается с разными значениями: " + strings.Join(vals, "; ")
		ex.Confidence = "low"
	}
	c.fields[key] = ex
	return true
}

var wsAny = du.MustPy(`\s+`)

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func nText(v string, _ hit) any { return du.CollapseWS(v) }
func nDate(v string, _ hit) any {
	if d := du.ParseDate(v); d != "" {
		return d
	}
	return nil
}
func nAmount(v string, _ hit) any {
	if a, ok := du.ParseAmount(v); ok {
		return a
	}
	return nil
}
func nIBAN(v string, _ hit) any  { return du.NormalizeIBAN(v) }
func nSwift(v string, _ hit) any { return du.NormalizeSwift(v) }
func nUpper(v string, _ hit) any { return strings.ToUpper(v) }

var swiftInText = regexp.MustCompile(`\b[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}(?:[A-Z0-9]{3})?\b`)

func nIntermediary(v string, _ hit) any {
	if m := swiftInText.FindString(v); m != "" {
		return m
	}
	return du.CollapseWS(v)
}

func label(lbl string) string { return labelV(lbl, `[^\n]*`) }
func labelV(lbl, value string) string {
	return `^[ \t]*(?:` + lbl + `)[ \t]*:[ \t]*(?P<v>` + value + `)$`
}
func labelFree(lbl, value string) string {
	return `^[ \t]*(?:` + lbl + `)[ \t]*:?[ \t]*(?P<v>` + value + `)[ \t]*$`
}

func (c *ctx) firstLineStarts(b model.DocBlock, pattern string) bool {
	first := strings.SplitN(c.d.BlockText(b), "\n", 2)[0]
	re := cached("first:"+pattern, func() *regexp.Regexp { return regexp.MustCompile(`(?i)^(?:` + du.Py(pattern) + `)`) })
	return re.MatchString(strings.TrimSpace(first))
}

func (c *ctx) moneyCur(key string, hits []hit, conf string) {
	if !c.put(key, hits, nAmount, conf, "") {
		return
	}
	src := c.fields[key].Source
	for _, h := range hits {
		if h.s == src.Start {
			g := "cur"
			if h.group("cur") == "" {
				g = "cur2"
			}
			cs, ce := h.span(g)
			if _, ok := c.fields["currency"]; !ok && cs >= 0 {
				v := c.d.Sub(cs, ce)
				c.fields["currency"] = model.Extracted{Key: "currency", Value: v, Norm: strings.ToUpper(v),
					Source: c.d.Ref(cs, ce), Confidence: conf, Status: "doc"}
			}
			break
		}
	}
}

var reTwoUpper = regexp.MustCompile(`^[A-Z]{2}`)

func (c *ctx) countryFrom(key, addrKey, ibanKey string) {
	if _, ok := c.fields[key]; ok {
		return
	}
	if a, ok := c.fields[addrKey]; ok {
		seg := c.d.Sub(a.Source.Start, a.Source.End)
		if code, s, e, found := FindCountry(seg); found {
			src := c.d.Ref(a.Source.Start+s, a.Source.Start+e)
			c.fields[key] = model.Extracted{Key: key, Value: du.CollapseWS(src.Quote), Norm: code, Source: src,
				Confidence: "high", Status: "doc"}
			return
		}
	}
	if ibanKey != "" {
		if ib, ok := c.fields[ibanKey]; ok && reTwoUpper.MatchString(ib.Source.Quote) {
			src := c.d.Ref(ib.Source.Start, ib.Source.Start+2)
			c.fields[key] = model.Extracted{Key: key, Value: src.Quote, Norm: src.Quote, Source: src,
				Confidence: "medium", Status: "doc", Note: "Страна определена по первым буквам IBAN"}
		}
	}
}

func head(bs []model.DocBlock, n int) []model.DocBlock {
	if len(bs) > n {
		return bs[:n]
	}
	return bs
}

func (c *ctx) contract() {
	d := c.d
	hasLang := false
	for _, b := range d.Blocks {
		if b.Lang != "" {
			hasLang = true
		}
	}
	var en, ru []model.DocBlock
	for _, b := range d.Blocks {
		if !hasLang || b.Lang == "en" {
			en = append(en, b)
		}
		if b.Lang == "ru" {
			ru = append(ru, b)
		}
	}
	headEN, headRU := head(en, 3), head(ru, 3)

	_ = c.put("contractNo", c.scan(labelFree(`Ref\.?(?:\s*No\.?)?`, `\S[^\n]*?`), headEN), nText, "high", "") ||
		c.put("contractNo", c.scan(`(?:Contract|CONTRACT)\s+No\.?\s*:?\s*(?P<v>[A-Z0-9][\w/\-.]*)`, en), nText, "high", "") ||
		c.put("contractNo", c.scan(labelFree(`Ref\.?|№`, `\S[^\n]*?`), headRU), nText, "medium", "")
	_ = c.put("contractDate", c.scan(labelV(`Date`, pDATE), headEN), nDate, "high", "") ||
		c.put("contractDate", c.scan(labelV(`Дата`, pDATE), headRU), nDate, "medium", "")
	_ = c.put("expiryDate", c.scan(`valid\s+(?:until|till|through)\s+(?P<v>`+pDATE+`)`, en), nDate, "high", "") ||
		c.put("expiryDate", c.scan(`действ\w*\s+до\s+(?P<v>`+pDATE+`)`, ru), nDate, "medium", "")
	hits := c.scan(`total\s+(?:value|amount|price)\s+of\s+(?:this|the)\s+Contract\s+(?:is|amounts\s+to)\s+(?P<v>`+pMONEY+`)`, en)
	if len(hits) > 0 {
		c.moneyCur("totalAmount", hits, "high")
	} else if hits = c.scan(`стоимость[^\n]{0,60}?(?:составляет|равна)\s+(?P<v>`+pMONEY+`)`, ru); len(hits) > 0 {
		c.moneyCur("totalAmount", hits, "medium")
	}
	if _, ok := c.fields["currency"]; !ok {
		c.put("currency", c.scan(`currency\s+of\s+the\s+Contract\s+is[^\n(]*\((?P<v>`+pCUR+`)\)`, en), nUpper, "high", "")
	}

	filter := func(bs []model.DocBlock, p string) []model.DocBlock {
		var out []model.DocBlock
		for _, b := range bs {
			if c.firstLineStarts(b, p) {
				out = append(out, b)
			}
		}
		return out
	}
	sellerEN := filter(en, `Seller\s*:`)
	if len(sellerEN) == 0 {
		sellerEN = en
	}
	sellerRU := filter(ru, `Продавец\s*:`)
	buyerEN := filter(en, `Buyer\s*:`)
	if len(buyerEN) == 0 {
		buyerEN = en
	}
	buyerRU := filter(ru, `Покупатель\s*:`)

	lab := func(key, enL, ruL string, norm normFn, bEN, bRU []model.DocBlock, lbl string) bool {
		if c.put(key, c.scan(label(enL), bEN), norm, "high", lbl) {
			return true
		}
		if ruL != "" && len(bRU) > 0 {
			return c.put(key, c.scan(label(ruL), bRU), norm, "medium", lbl)
		}
		return false
	}
	lab("sellerName", `Seller`, `Продавец`, nText, sellerEN, sellerRU, "Seller")
	if _, ok := c.fields["sellerName"]; !ok {
		c.put("sellerName", c.scan(`^(?P<v>[^\n(]+?)\s*\(hereinafter\s+(?:referred\s+to\s+as\s+)?(?:the\s+)?.?Seller`, en), nText, "medium", "")
	}
	_ = c.put("sellerRepresentative", c.scan(`Seller.{0,3}\)\s*,?\s*represented\s+by\s+(?P<v>[^,\n]+)`, en), nText, "high", "") ||
		c.put("sellerRepresentative", c.scan(`Продав\w+.{0,3}\s*,?\s*в\s+лице\s+(?P<v>[^,\n]+)`, ru), nText, "medium", "")
	lab("sellerAddress", `Office\s+Address|Legal\s+Address|Address`, `Адрес\s+офиса|Юридический\s+адрес`, nText, sellerEN, sellerRU, "")
	lab("sellerAddress", `License\s+Address`, `Адрес\s+по\s+лицензии`, nText, sellerEN, sellerRU, "")
	lab("sellerLicenseNo", `Trade\s+License(?:\s+Registration)?\s+No\.?|License\s+No\.?`, `Номер\s+торговой\s+лицензии`, nText, sellerEN, sellerRU, "")
	lab("sellerBank", `Beneficiary\s+Bank|Bank\s+Name|Bank`, `Банк\s+получателя|Банк`, nText, sellerEN, sellerRU, "")
	lab("sellerBankAddress", `Bank\s+Address`, `Адрес\s+банка`, nText, sellerEN, sellerRU, "")
	lab("sellerAccountName", `Account\s+Name`, `Наименование\s+счёта|Наименование\s+счета`, nText, sellerEN, sellerRU, "")
	lab("sellerAccountNo", `Account\s+No\.?|Account\s+Number`, `Номер\s+сч[её]та`, nText, sellerEN, sellerRU, "")
	lab("sellerIban", `IBAN`, `IBAN`, nIBAN, sellerEN, sellerRU, "IBAN")
	lab("sellerSwift", `Swift\s+Code|SWIFT|SWIFT/BIC|BIC`, `SWIFT|БИК`, nSwift, sellerEN, sellerRU, "Swift Code")
	c.countryFrom("sellerCountry", "sellerAddress", "sellerIban")

	lab("buyerName", `Buyer`, `Покупатель`, nText, buyerEN, buyerRU, "")
	lab("buyerAddress", `Legal\s+address`, `Юридический\s+адрес`, nText, buyerEN, buyerRU, "")
	curv := "USD"
	if cur, ok := c.fields["currency"]; ok {
		if s, ok := cur.Norm.(string); ok && s != "" {
			curv = regexp.QuoteMeta(s)
		}
	}
	if !lab("buyerIban", `Settlement\s+account\s+`+curv+`|IBAN`, `Расч[её]тный\s+сч[её]т\s+`+curv, nIBAN, buyerEN, buyerRU, "") {
		lab("buyerIban", `Settlement\s+account(?:\s+[A-Z]{3})?`, `Расч[её]тный\s+сч[её]т(?:\s+[A-Z]{3})?`, nIBAN, buyerEN, buyerRU, "")
	}
	lab("buyerBic", `BIC|SWIFT`, `БИК`, nSwift, buyerEN, buyerRU, "")
	pay := c.scan(`^\s*4\.\d+\.?\s+(?P<v>[^\n]*\bpa(?:y|id|yment)\b[^\n]*\b(?:within|not\s+later\s+than)\s+\d+[^\n]*)$`, en)
	c.put("paymentTerms", pay, nText, "medium", "")
}

var reAddrStop = regexp.MustCompile(`^\s*(No\b|\d+\s)`)

func (c *ctx) invoiceLike(kind model.DocKind) {
	d := c.d
	bl := d.Blocks
	whole := []model.DocBlock{{Start: 0, End: d.Len()}}
	if kind != model.KindLetter {
		c.put("invoiceNo", c.scan(`^\s*(?:Commercial\s+)?Invoice\s*(?:No\.?|Number|№)\s*:?\s*(?P<v>[A-Z0-9][\w/\-.]*)`, bl), nText, "high", "Invoice No")
		c.put("invoiceDate", c.scan(labelV(`(?:Invoice\s+)?Date|Дата`, pDATE), bl), nDate, "high", "Date")
		if hits := c.scan(`^\s*(?:Total(?:\s+amount)?(?:\s+due)?|Grand\s+total|Итого)\s*:?\s*(?P<v>`+pMONEY+`)`, bl); len(hits) > 0 {
			c.moneyCur("invoiceAmount", hits, "high")
		}
		c.put("invoiceDueDate", c.scan(`due\s+date\s*:?\s*(?P<v>`+pDATE+`)`, whole), nDate, "high", "")
		c.put("paymentTerms", c.scan(label(`Payment\s+terms`), bl), nText, "high", "")
		c.put("deliveryTerms", c.scan(label(`Delivery\s+terms|Terms\s+of\s+delivery`), bl), nText, "high", "")
		c.put("goodsDescription", c.scan(`^\s*1\.?\s+(?P<v>\S.*?)\s+\d+(?:[.,]\d+)?\s+(?:`+pNUM+`)\s+(?:`+pNUM+`)\s*$`, bl), nText, "medium", "")
	}
	refPat := `(?P<v>(?P<no>[A-Z0-9][\w/\-.]*?)\s+(?:dated|dd\.?|of|from|от)\s+(?P<d>` + pDATE + `))`
	refNorm := func(v string, h hit) any { return h.group("no") + "|" + du.ParseDate(h.group("d")) }
	if kind == model.KindLetter {
		_ = c.put("invoiceContractRef", c.scan(`Ref\.?\s*:?\s*Contract\s+(?:No\.?\s*)?`+refPat, whole), refNorm, "high", "") ||
			c.put("invoiceContractRef", c.scan(`Contract\s+(?:No\.?\s*)?`+refPat, whole), refNorm, "medium", "")
	} else {
		_ = c.put("invoiceContractRef", c.scan(`Contract\s*(?:Ref\.?|No\.?|Reference)\s*:?\s*`+refPat, whole), refNorm, "high", "") ||
			c.put("invoiceContractRef", c.scan(`Contract\s*(?:Ref\.?|No\.?|Reference)\s*:?\s*(?P<v>[A-Z0-9][\w/\-.]*)`, bl),
				func(v string, _ hit) any { return v + "|" }, "medium", "")
	}
	c.put("sellerName", c.scan(label(`Beneficiary(?:\s+Name)?|Account\s+Name`), bl), nText, "high", "Beneficiary")
	sellerHits := c.scan(`^\s*Seller\s*:\s*(?P<v>[^,\n]+)`, bl)
	if _, ok := c.fields["sellerName"]; !ok {
		c.put("sellerName", sellerHits, nText, "medium", "")
	}
	if len(sellerHits) > 0 {
		h := sellerHits[0]
		R := d.R
		find := func(from int) int {
			for i := from; i < len(R); i++ {
				if R[i] == '\n' {
					return i
				}
			}
			return -1
		}
		e := h.e
		lineEnd := find(e)
		if lineEnd < 0 {
			lineEnd = len(R)
		}
		aStart := e
		if e < len(R) && R[e] == ',' {
			aStart = e + 1
		}
		aEnd := lineEnd
		nxt := lineEnd + 1
		for nxt < len(R) {
			ne := find(nxt)
			if ne < 0 {
				ne = len(R)
			}
			line := string(R[nxt:ne])
			if strings.Contains(line, ":") || strings.TrimSpace(line) == "" || reAddrStop.MatchString(line) {
				break
			}
			aEnd = ne
			nxt = ne + 1
		}
		if aEnd > aStart {
			src := d.Ref(aStart, aEnd)
			if src.Quote != "" {
				c.fields["sellerAddress"] = model.Extracted{Key: "sellerAddress", Value: du.JoinLines(src.Quote),
					Source: src, Confidence: "medium", Status: "doc"}
			}
		}
	}
	c.put("buyerName", c.scan(`^\s*Buyer\s*:\s*(?P<v>[^,\n]+)`, bl), nText, "high", "")
	c.put("sellerBank", c.scan(label(`Beneficiary(?:'s)?\s+Bank|Bank\s+of\s+Beneficiary|Bank\s+Name`), bl), nText, "high", "Beneficiary Bank")
	c.put("sellerAccountNo", c.scan(label(`Account\s+No\.?|Account\s+Number`), bl), nText, "high", "")
	c.put("sellerIban", c.scan(labelV(`IBAN`, `[A-Za-z]{2}[\dA-Za-z \x{00A0}]{8,40}?`), bl), nIBAN, "high", "IBAN")
	c.put("sellerSwift", c.scan(labelV(`Swift(?:\s+Code)?|SWIFT/BIC|BIC`, `[A-Za-z0-9 ]{8,14}?`), bl), nSwift, "high", "Swift Code")
	c.put("intermediaryBank", c.scan(label(`Intermediary(?:\s+Bank)?|Correspondent\s+Bank`), bl), nIntermediary, "high", "")
	c.countryFrom("sellerCountry", "sellerAddress", "sellerIban")
}

// Fields извлекает реквизиты документа (порт extract.py).
func Fields(doc model.ParsedDoc) model.ExtractionResult {
	c := &ctx{d: du.New(doc), fields: map[string]model.Extracted{}}
	if doc.Kind == model.KindContract {
		c.contract()
	} else {
		c.invoiceLike(doc.Kind)
	}
	return model.ExtractionResult{DocID: doc.ID, Kind: doc.Kind, Fields: c.fields}
}

// Keys — порядок ключей извлечения (для вывода).
func Keys() []string { return append(append([]string{}, model.ExtractKeys...), ExtraKeys...) }
