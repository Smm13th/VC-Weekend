// Package parse — разбор DOCX, PDF и TXT в model.ParsedDoc: плоский текст и блоки с позициями в рунах.
package parse

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"

	"vedhelper/internal/model"
)

var cyr = regexp.MustCompile(`[А-Яа-яЁё]`)
var multiSpace = regexp.MustCompile(`[ \t\x{00A0}\x{202F}\x{2009}]{2,}`)

type builder struct {
	parts  []string
	blocks []model.DocBlock
	pos    int
}

func (b *builder) add(text string, blk model.DocBlock) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if len(b.parts) > 0 {
		b.parts = append(b.parts, "\n")
		b.pos++
	}
	n := utf8.RuneCountInString(text)
	b.parts = append(b.parts, text)
	blk.Start, blk.End = b.pos, b.pos+n
	b.blocks = append(b.blocks, blk)
	b.pos += n
}

func (b *builder) text() string { return strings.Join(b.parts, "") }

func cleanLine(s string) string {
	s = strings.NewReplacer("\u00a0", " ", "\uFFFD", "").Replace(s)
	return strings.TrimSpace(multiSpace.ReplaceAllString(s, " "))
}

// ---------- DOCX ----------

type xnode struct {
	name     string
	children []*xnode
	text     string // для w:t
}

func (n *xnode) kids(name string) []*xnode {
	var out []*xnode
	for _, c := range n.children {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

func readXMLTree(r io.Reader) (*xnode, error) {
	dec := xml.NewDecoder(r)
	root := &xnode{name: "#root"}
	stack := []*xnode{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{name: t.Name.Local}
			top := stack[len(stack)-1]
			top.children = append(top.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top := stack[len(stack)-1]
			if top.name == "t" {
				top.text += string(t)
			}
		}
	}
	return root, nil
}

func find(n *xnode, name string) *xnode {
	if n.name == name {
		return n
	}
	for _, c := range n.children {
		if f := find(c, name); f != nil {
			return f
		}
	}
	return nil
}

// paraText — текст абзаца: w:t подряд, w:tab -> пробел, w:br/w:cr -> перевод строки (только внутри w:r).
func paraText(p *xnode) string {
	var b strings.Builder
	var walk func(n *xnode, inRun bool)
	walk = func(n *xnode, inRun bool) {
		for _, c := range n.children {
			switch c.name {
			case "pPr", "rPr", "tbl", "del", "delText", "instrText":
				continue
			case "r":
				walk(c, true)
			case "t":
				if inRun {
					b.WriteString(c.text)
				}
			case "tab":
				if inRun {
					b.WriteByte(' ')
				}
			case "br", "cr":
				if inRun {
					b.WriteByte('\n')
				}
			default:
				walk(c, inRun)
			}
		}
	}
	walk(p, false)
	return b.String()
}

func cellText(tc *xnode) string {
	var paras []string
	for _, p := range tc.kids("p") {
		paras = append(paras, strings.TrimRightFunc(paraText(p), unicode.IsSpace))
	}
	for len(paras) > 0 && strings.TrimSpace(paras[len(paras)-1]) == "" {
		paras = paras[:len(paras)-1]
	}
	for len(paras) > 0 && strings.TrimSpace(paras[0]) == "" {
		paras = paras[1:]
	}
	return strings.Join(paras, "\n")
}

func parseDOCX(data []byte) (string, []model.DocBlock, int, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, 0, fmt.Errorf("docx: не zip-архив: %w", err)
	}
	var f *zip.File
	for _, zf := range zr.File {
		if zf.Name == "word/document.xml" {
			f = zf
		}
	}
	if f == nil {
		return "", nil, 0, errors.New("docx: нет word/document.xml")
	}
	rc, err := f.Open()
	if err != nil {
		return "", nil, 0, err
	}
	defer rc.Close()
	root, err := readXMLTree(rc)
	if err != nil {
		return "", nil, 0, fmt.Errorf("docx: %w", err)
	}
	body := find(root, "body")
	if body == nil {
		return "", nil, 0, errors.New("docx: нет w:body")
	}
	b := &builder{}
	rowIdx := 0
	for _, child := range body.children {
		switch child.name {
		case "p":
			b.add(strings.TrimRightFunc(paraText(child), unicode.IsSpace), model.DocBlock{})
		case "tbl":
			var rows [][]string
			for _, tr := range child.kids("tr") {
				var cells []string
				for _, tc := range tr.kids("tc") {
					cells = append(cells, cellText(tc))
				}
				rows = append(rows, cells)
			}
			two := len(rows) > 0
			for _, r := range rows {
				if len(r) != 2 {
					two = false
				}
			}
			bilingual := false
			if two {
				for _, r := range rows {
					if cyr.MatchString(r[1]) {
						bilingual = true
					}
				}
			}
			for _, cells := range rows {
				for ci, t := range cells {
					row := rowIdx
					blk := model.DocBlock{Row: &row}
					if bilingual {
						if ci == 0 {
							blk.Lang = "en"
						} else {
							blk.Lang = "ru"
						}
					}
					b.add(t, blk)
				}
				rowIdx++
			}
		}
	}
	return b.text(), b.blocks, 1, nil
}

// ---------- PDF ----------

type glyph struct {
	x, y, w, size float64
	s             string
}

type run struct {
	x0, x1, y, size float64
	known           bool // x1 известен (ширины глифов > 0)
	text            strings.Builder
}

func lineTol(a, b float64) float64 {
	m := math.Min(a, b)
	if m <= 0 {
		m = math.Max(a, b)
	}
	return math.Max(1.0, m*0.4)
}

// pdfLines собирает строки страницы: глифы по порядку потока -> отрезки -> строки по Y, внутри строки по X.
// Мелкий и светлый текст не отбрасывается (цвет и размер не учитываются).
func pdfLines(gl []glyph) []string {
	var runs []*run
	var cur *run
	var prev *glyph
	for i := range gl {
		g := &gl[i]
		if g.s == "" {
			continue
		}
		newRun := cur == nil
		space := false
		if !newRun {
			if math.Abs(g.y-prev.y) > lineTol(g.size, prev.size) {
				newRun = true
			} else if prev.w > 0 {
				exp := prev.x + prev.w
				gap := g.x - exp
				switch {
				case gap < -math.Max(g.size, prev.size)*0.5:
					newRun = true
				case gap > math.Max(g.size, prev.size)*3:
					newRun = true
				case gap > math.Max(g.size, prev.size)*0.15:
					space = true
				}
			} else {
				// ширины глифов неизвестны (шрифт без /Widths): X меняется только от смещений в TJ.
				// Малый сдвиг — кернинг, сдвиг около пробела — граница слова, большой — другой отрезок текста.
				em := math.Max(g.size, prev.size)
				dx := g.x - prev.x
				switch {
				case dx <= -em*0.5 || dx >= em*0.9:
					newRun = true
				case dx > em*0.15:
					space = true
				}
			}
		}
		if newRun {
			cur = &run{x0: g.x, x1: g.x + g.w, y: g.y, size: g.size, known: g.w > 0}
			runs = append(runs, cur)
		} else if space {
			cur.text.WriteByte(' ')
		}
		cur.text.WriteString(g.s)
		if g.w > 0 {
			cur.x1 = g.x + g.w
		} else {
			cur.known = false
		}
		prev = g
	}
	// строки: сверху вниз
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].y > runs[j].y })
	var lines [][]*run
	for _, r := range runs {
		if len(lines) > 0 {
			last := lines[len(lines)-1]
			ref := last[0]
			if math.Abs(r.y-ref.y) <= lineTol(r.size, ref.size) {
				lines[len(lines)-1] = append(last, r)
				continue
			}
		}
		lines = append(lines, []*run{r})
	}
	var out []string
	for _, ln := range lines {
		sort.SliceStable(ln, func(i, j int) bool { return ln[i].x0 < ln[j].x0 })
		var b strings.Builder
		for i, r := range ln {
			if i > 0 {
				p := ln[i-1]
				if !(p.known && r.x0-p.x1 < math.Max(r.size, p.size)*0.15) {
					b.WriteByte(' ')
				}
			}
			b.WriteString(r.text.String())
		}
		if s := cleanLine(b.String()); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parsePDF(data []byte) (text string, blocks []model.DocBlock, pages int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pdf: не удалось прочитать файл: %v", r)
		}
	}()
	rd, e := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return "", nil, 0, fmt.Errorf("pdf: %w", e)
	}
	n := rd.NumPage()
	b := &builder{}
	for pi := 1; pi <= n; pi++ {
		p := rd.Page(pi)
		if p.V.IsNull() {
			continue
		}
		c := p.Content()
		gl := make([]glyph, 0, len(c.Text))
		for _, t := range c.Text {
			gl = append(gl, glyph{x: t.X, y: t.Y, w: t.W, size: t.FontSize, s: t.S})
		}
		for _, line := range pdfLines(gl) {
			b.add(line, model.DocBlock{Page: pi})
		}
	}
	if n < 1 {
		n = 1
	}
	return b.text(), b.blocks, n, nil
}

// ---------- TXT ----------

func decodeText(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data)
	}
	// cp1251
	var b strings.Builder
	for _, c := range data {
		if c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(cp1251[c-0x80])
		}
	}
	return b.String()
}

var cp1251 = []rune("ЂЃ‚ѓ„…†‡€‰Љ‹ЊЌЋЏђ‘’“”•–—�™љ›њќћџ ЎўЈ¤Ґ¦§Ё©Є«¬­®Ї°±Ііґµ¶·ё№є»јЅѕї" +
	"АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгдежзийклмнопрстуфхцчшщъыьэюя")

func parseTXT(data []byte) (string, []model.DocBlock, int) {
	s := decodeText(data)
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	b := &builder{}
	for _, line := range strings.Split(s, "\n") {
		b.add(strings.TrimRightFunc(line, unicode.IsSpace), model.DocBlock{})
	}
	return b.text(), b.blocks, 1
}

// ---------- классификация ----------

var (
	reLetter1 = regexp.MustCompile(`(?i)notice of change|change of bank details|уведомлени[ея] об изменении|письмо`)
	reLetter2 = regexp.MustCompile(`(?im)^dear\b`)
	reInvoice = regexp.MustCompile(`(?im)\binvoice\s*(no|№|number)|commercial invoice|proforma invoice|^invoice\b|инвойс\s*№|счёт-фактура`)
	reContr   = regexp.MustCompile(`(?im)^\s*(contract|договор|контракт)(?:$|[^\pL\pN_])`)
)

func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// Classify определяет вид документа по началу текста и имени файла.
func Classify(text, name string) model.DocKind {
	head := headRunes(text, 600)
	low := strings.ToLower(head)
	if reLetter1.MatchString(low) || reLetter2.MatchString(low) {
		return model.KindLetter
	}
	if reInvoice.MatchString(low) {
		return model.KindInvoice
	}
	if reContr.MatchString(low) || strings.Contains(strings.ToLower(headRunes(text, 3000)), "hereinafter referred to") {
		return model.KindContract
	}
	n := strings.ToLower(name)
	has := func(ks ...string) bool {
		for _, k := range ks {
			if strings.Contains(n, k) {
				return true
			}
		}
		return false
	}
	switch {
	case has("letter", "notice", "письмо", "change"):
		return model.KindLetter
	case has("invoice", "инвойс", "inv"):
		return model.KindInvoice
	case has("contract", "договор", "контракт"):
		return model.KindContract
	}
	return model.KindOther
}

// ParseFile разбирает файл по расширению или сигнатуре. docID пустой -> name.
func ParseFile(data []byte, name, docID string) (model.ParsedDoc, error) {
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = strings.ToLower(name[i+1:])
	}
	var (
		text   string
		blocks []model.DocBlock
		pages  int
		format string
		err    error
	)
	switch {
	case ext == "docx" || (bytes.HasPrefix(data, []byte("PK")) && ext != "pdf" && ext != "txt"):
		format = "docx"
		text, blocks, pages, err = parseDOCX(data)
	case ext == "pdf" || bytes.HasPrefix(data, []byte("%PDF-")):
		format = "pdf"
		text, blocks, pages, err = parsePDF(data)
	default:
		format = "txt"
		text, blocks, pages = parseTXT(data)
	}
	if err != nil {
		return model.ParsedDoc{}, err
	}
	if docID == "" {
		docID = name
	}
	base := name
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if blocks == nil {
		blocks = []model.DocBlock{}
	}
	return model.ParsedDoc{ID: docID, Name: base, Kind: Classify(text, name), Format: format,
		Text: text, Blocks: blocks, Pages: pages}, nil
}
