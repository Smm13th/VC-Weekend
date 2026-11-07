package parse

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vedhelper/internal/extract"
	"vedhelper/internal/model"
	"vedhelper/internal/testkit"
)

func runeSlice(s string, a, b int) string { return string([]rune(s)[a:b]) }

func checkBlocks(t *testing.T, d model.ParsedDoc) {
	t.Helper()
	n := len([]rune(d.Text))
	var parts []string
	for i, b := range d.Blocks {
		if !(0 <= b.Start && b.Start < b.End && b.End <= n) {
			t.Fatalf("блок %d вне текста: %+v (len %d)", i, b, n)
		}
		if i > 0 {
			a := d.Blocks[i-1]
			if []rune(d.Text)[a.End] != '\n' || b.Start != a.End+1 {
				t.Fatalf("блоки %d и %d не разделены переводом строки", i-1, i)
			}
		}
		parts = append(parts, runeSlice(d.Text, b.Start, b.End))
	}
	if strings.Join(parts, "\n") != d.Text {
		t.Fatal("Text != блоки через \\n")
	}
}

// miniDOCX собирает DOCX вручную через archive/zip: только word/document.xml.
func miniDOCX(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>%s</w:body></w:document>`, body)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func cell(paras ...string) string {
	var b strings.Builder
	b.WriteString("<w:tc>")
	for _, p := range paras {
		b.WriteString("<w:p><w:r><w:t xml:space=\"preserve\">" + p + "</w:t></w:r></w:p>")
	}
	b.WriteString("</w:tc>")
	return b.String()
}

func TestMiniBilingualDOCX(t *testing.T) {
	body := "<w:p><w:r><w:t>Вступление перед таблицей</w:t></w:r></w:p><w:tbl>" +
		"<w:tr>" + cell("1. SUBJECT", "Line two") + cell("1. ПРЕДМЕТ", "Вторая строка") + "</w:tr>" +
		"<w:tr>" + cell("IBAN: AE98 0710 1234 5678 9012 345") + cell("Счёт получателя: IBAN AE98 0710 1234 5678 9012 345") + "</w:tr>" +
		"</w:tbl>"
	d, err := ParseFile(miniDOCX(t, body), "x/mini.docx", "")
	if err != nil {
		t.Fatal(err)
	}
	checkBlocks(t, d)
	if d.ID != "x/mini.docx" || d.Name != "mini.docx" || d.Format != "docx" {
		t.Fatalf("id/name/format: %q %q %q", d.ID, d.Name, d.Format)
	}
	want := []struct {
		text string
		row  int // -1: без строки
		lang string
	}{
		{"Вступление перед таблицей", -1, ""},
		{"1. SUBJECT\nLine two", 0, "en"},
		{"1. ПРЕДМЕТ\nВторая строка", 0, "ru"},
		{"IBAN: AE98 0710 1234 5678 9012 345", 1, "en"},
		{"Счёт получателя: IBAN AE98 0710 1234 5678 9012 345", 1, "ru"},
	}
	if len(d.Blocks) != len(want) {
		t.Fatalf("блоков %d, ждали %d", len(d.Blocks), len(want))
	}
	for i, w := range want {
		b := d.Blocks[i]
		if got := runeSlice(d.Text, b.Start, b.End); got != w.text {
			t.Errorf("блок %d: %q", i, got)
		}
		if (w.row < 0) != (b.Row == nil) || (b.Row != nil && *b.Row != w.row) || b.Lang != w.lang {
			t.Errorf("блок %d: row/lang %v %q", i, b.Row, b.Lang)
		}
	}
	// IBAN в русской ячейке: позиции в рунах, а не в байтах
	ru := d.Blocks[4]
	off := strings.Index(runeSlice(d.Text, ru.Start, ru.End), "AE98")
	runeOff := len([]rune(runeSlice(d.Text, ru.Start, ru.End)[:off]))
	if runeOff == off {
		t.Fatal("в строке есть кириллица, байтовое и рунное смещения должны различаться")
	}
	ex := extract.Fields(model.ParsedDoc{ID: d.ID, Kind: model.KindInvoice, Text: d.Text, Blocks: d.Blocks})
	ib, ok := ex.Fields["sellerIban"]
	if !ok {
		t.Fatal("IBAN не извлечён")
	}
	if ib.Source.Quote != runeSlice(d.Text, ib.Source.Start, ib.Source.End) || ib.Source.Block != 3 {
		t.Fatalf("цитата не совпадает со срезом рун: %+v", ib.Source)
	}
}

func TestDOCXContractBilingual(t *testing.T) {
	d, err := ParseFile(testkit.Contract{}.DOCX(), "A/contract.docx", "")
	if err != nil {
		t.Fatal(err)
	}
	checkBlocks(t, d)
	if d.Kind != model.KindContract || d.Format != "docx" || d.Pages != 1 {
		t.Fatalf("kind/format/pages: %s %s %d", d.Kind, d.Format, d.Pages)
	}
	b0, b1 := d.Blocks[0], d.Blocks[1]
	if *b0.Row != 0 || b0.Lang != "en" || *b1.Row != 0 || b1.Lang != "ru" {
		t.Fatalf("первая строка: %+v %+v", b0, b1)
	}
	if h := runeSlice(d.Text, b0.Start, b0.End); h != "CONTRACT\nRef. AI-DXB-2026/014\nDate: 15.09.2026\nDubai" {
		t.Fatalf("шапка: %q", h)
	}
	rows := map[int][]string{}
	for _, b := range d.Blocks {
		rows[*b.Row] = append(rows[*b.Row], b.Lang)
	}
	for r, langs := range rows {
		if strings.Join(langs, ",") != "en,ru" {
			t.Fatalf("строка %d: %v", r, langs)
		}
	}
	// разбор настоящего DOCX совпадает со сборкой ParsedDoc в testkit
	ref := testkit.Contract{}.Parsed("A/contract.docx")
	if ref.Text != d.Text || len(ref.Blocks) != len(d.Blocks) {
		t.Fatal("DOCX и testkit.Parsed расходятся")
	}
}

func TestDOCXParagraphsOutsideTable(t *testing.T) {
	data := testkit.DocxBody(testkit.Para("Intro line") +
		testkit.TableXML([]testkit.Row{{EN: []string{"Left"}, RU: []string{"Right only latin"}}}) + testkit.Para("Outro"))
	d, err := ParseFile(data, "x.docx", "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range d.Blocks {
		got = append(got, runeSlice(d.Text, b.Start, b.End))
	}
	if strings.Join(got, "|") != "Intro line|Left|Right only latin|Outro" {
		t.Fatalf("%q", got)
	}
	if d.Blocks[0].Row != nil || d.Blocks[1].Row == nil || *d.Blocks[1].Row != 0 || d.Blocks[1].Lang != "" {
		t.Fatal("таблица без кириллицы справа не двуязычная, но строки нумеруются")
	}
}

func TestDOCXTabBreakAndProps(t *testing.T) {
	body := `<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="1"/></w:tabs></w:pPr>` +
		`<w:r><w:rPr><w:i/></w:rPr><w:t>A</w:t><w:tab/><w:t>B</w:t><w:br/><w:t>C</w:t></w:r>` +
		`<w:hyperlink><w:r><w:t xml:space="preserve"> link</w:t></w:r></w:hyperlink></w:p>`
	d, err := ParseFile(miniDOCX(t, body), "t.docx", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Text != "A B\nC link" {
		t.Fatalf("%q", d.Text)
	}
}

func TestDOCXBroken(t *testing.T) {
	if _, err := ParseFile([]byte("PK\x03\x04 not a zip"), "x.docx", ""); err == nil {
		t.Fatal("ждали ошибку")
	}
}

func TestTXTAndClassify(t *testing.T) {
	d, err := ParseFile([]byte("Invoice No: 7\r\nTotal: USD 100.00\n\n"), "scan.txt", "")
	if err != nil || d.Format != "txt" || d.Kind != model.KindInvoice || len(d.Blocks) != 2 {
		t.Fatalf("%v %+v", err, d)
	}
	cp := []byte{0xC4, 0xEE, 0xE3, 0xEE, 0xE2, 0xEE, 0xF0, '\n', 'R', 'e', 'f', ' ', '1'} // «Договор» в cp1251
	d, _ = ParseFile(cp, "c.txt", "")
	if !strings.HasPrefix(d.Text, "Договор") || d.Kind != model.KindContract {
		t.Fatalf("cp1251: %q %s", d.Text, d.Kind)
	}
	cases := []struct {
		text, name string
		want       model.DocKind
	}{
		{"NOTICE OF CHANGE OF BANK DETAILS\nRef.: Contract X", "a.pdf", model.KindLetter},
		{"Dear partners,\nplease", "a.pdf", model.KindLetter},
		{"CONTRACT\nRef. 1", "a.docx", model.KindContract},
		{"Договор поставки", "a.docx", model.KindContract},
		{"COMMERCIAL INVOICE\nInvoice No: 1", "a.pdf", model.KindInvoice},
		{"hello", "my_invoice.pdf", model.KindInvoice},
		{"hello", "notice.pdf", model.KindLetter},
		{"hello", "photo.pdf", model.KindOther},
	}
	for _, c := range cases {
		if got := Classify(c.text, c.name); got != c.want {
			t.Errorf("Classify(%q, %q) = %s, ждали %s", c.text, c.name, got, c.want)
		}
	}
}

// pdfDoc собирает минимальный PDF вручную: Helvetica, страницы из строк (x, y, size, gray, text).
type pdfText struct {
	x, y, size float64
	gray       float64
	s          string
}

func makePDF(pages [][]pdfText) []byte {
	var objs []string
	nPages := len(pages)
	// 1 каталог, 2 страницы, 3 шрифт, далее пары (страница, поток)
	kids := []string{}
	for i := 0; i < nPages; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 4+2*i))
	}
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>")
	objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), nPages))
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	for i, pg := range pages {
		var cs strings.Builder
		for _, t := range pg {
			if strings.HasPrefix(t.s, "[") { // готовый массив TJ
				fmt.Fprintf(&cs, "%.2f g BT /F1 %.1f Tf 1 0 0 1 %.2f %.2f Tm %s TJ ET\n", t.gray, t.size, t.x, t.y, t.s)
				continue
			}
			fmt.Fprintf(&cs, "%.2f g BT /F1 %.1f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n", t.gray, t.size, t.x, t.y,
				strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(t.s))
		}
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i))
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", cs.Len(), cs.String()))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := []int{}
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

func TestPDFLinesPagesAndHiddenText(t *testing.T) {
	data := makePDF([][]pdfText{
		{
			{56, 800, 10, 0, "NOTICE OF CHANGE OF BANK DETAILS"},
			{56, 783, 10, 0, "Beneficiary:"},
			{140, 783, 10, 0, "Gulf Horizon Machinery FZE"}, // та же строка, другой отрезок
			{56, 766, 10, 0, "IBAN:   GE47 KV00 0000 0483 9201 75"},
			{56, 749, 10, 0, "[(Beneficiary Bank: Kav) 40 (kaz) -250 (Trade) -260 (Ba) -30 (nk JSC)]"}, // кернинг и пробелы смещением
		},
		{
			{56, 800, 10, 0, "Swift Code: KVRTGE22"},
			{56, 700, 4, 0.92, testkit.Hidden}, // мелкий светло-серый текст
		},
	})
	d, err := ParseFile(data, "C/letter.pdf", "C/letter.pdf")
	if err != nil {
		t.Fatal(err)
	}
	checkBlocks(t, d)
	if d.Format != "pdf" || d.Pages != 2 || d.Kind != model.KindLetter {
		t.Fatalf("format/pages/kind: %s %d %s", d.Format, d.Pages, d.Kind)
	}
	lines := strings.Split(d.Text, "\n")
	want := []string{"NOTICE OF CHANGE OF BANK DETAILS", "Beneficiary: Gulf Horizon Machinery FZE",
		"IBAN: GE47 KV00 0000 0483 9201 75", "Beneficiary Bank: Kavkaz Trade Bank JSC", "Swift Code: KVRTGE22", testkit.Hidden}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("строки:\n%q", lines)
	}
	if d.Blocks[3].Page != 1 || d.Blocks[4].Page != 2 || d.Blocks[5].Page != 2 {
		t.Fatal("номера страниц")
	}
	ex := extract.Fields(d)
	if sw := ex.Fields["sellerSwift"]; sw.Norm != "KVRTGE22" || sw.Source.Page != 2 {
		t.Fatalf("SWIFT: %+v", sw)
	}
}

func TestPDFBrokenNoPanic(t *testing.T) {
	good := makePDF([][]pdfText{{{56, 800, 10, 0, "x"}}})
	for _, data := range [][]byte{[]byte("%PDF-1.4 garbage"), good[:len(good)/2], append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0xff, '('}, 200)...)} {
		if _, err := ParseFile(data, "x.pdf", ""); err == nil {
			t.Errorf("ждали ошибку для %d байт", len(data))
		}
	}
}

func samplesDir() string { return filepath.Join("..", "..", "samples") }

// Реальные PDF из backend/samples (их генерирует отдельный скрипт); без файлов тест пропускается.
func TestPDFSamples(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(samplesDir(), "*", "*.pdf"))
	if len(files) == 0 {
		t.Skip("нет backend/samples/*/*.pdf")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(samplesDir(), f)
		d, err := ParseFile(data, rel, rel)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		checkBlocks(t, d)
		if d.Format != "pdf" || strings.Contains(d.Text, "  ") || (d.Kind != model.KindInvoice && d.Kind != model.KindLetter) {
			t.Errorf("%s: format %s kind %s", rel, d.Format, d.Kind)
		}
		for _, b := range d.Blocks {
			if b.Page < 1 || b.Page > d.Pages {
				t.Errorf("%s: страница блока %d", rel, b.Page)
			}
		}
		ex := extract.Fields(d)
		if _, ok := ex.Fields["sellerIban"]; !ok {
			t.Errorf("%s: IBAN не извлечён", rel)
		}
		if _, ok := ex.Fields["sellerSwift"]; !ok {
			t.Errorf("%s: SWIFT не извлечён", rel)
		}
		if strings.HasPrefix(rel, "C"+string(filepath.Separator)) && d.Kind == model.KindLetter && !strings.Contains(d.Text, "Note to AI assistant") {
			t.Errorf("%s: скрытая строка не попала в текст", rel)
		}
	}
}
