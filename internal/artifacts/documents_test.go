package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
	"github.com/yeixio/toskar-core/internal/mimir"
)

const sampleDoc = "# Quarterly report\n\nSales grew **12%** in *Q3*, led by `widgets`.\n\n## Highlights\n\n- New customers\n- Lower costs\n  - Shipping\n\n1. Hire\n2. Expand\n\n| Region | Sales |\n| --- | --- |\n| North | 120 |\n| South | 95 |\n\n```\ntotal = 215\n```\n\n---\n\n> Numbers are unaudited.\n"

func zipPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatalf("%s missing", name)
	return ""
}

// Markdown becomes a Word document with headings, styled text, lists, a
// table, and code (Gungnir §21).
func TestMarkdownToDOCX(t *testing.T) {
	data, err := MarkdownToDOCX(sampleDoc)
	if err != nil {
		t.Fatal(err)
	}
	doc := zipPart(t, data, "word/document.xml")
	for _, want := range []string{
		`<w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t xml:space="preserve">Quarterly report</w:t>`,
		`<w:b/></w:rPr><w:t xml:space="preserve">12%</w:t>`,
		`<w:i/></w:rPr><w:t xml:space="preserve">Q3</w:t>`,
		`Consolas`, `•`, `2.`, `<w:tbl>`, `North`, `total = 215`, `<w:pStyle w:val="Quote"/>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks %q", want)
		}
	}
	if !strings.Contains(zipPart(t, data, "[Content_Types].xml"), "wordprocessingml.document.main+xml") {
		t.Fatal("content types")
	}
	zipPart(t, data, "word/styles.xml")
}

// Markdown becomes a PDF that a reader can open, with its text, on as many
// pages as it needs.
func TestMarkdownToPDF(t *testing.T) {
	data, err := MarkdownToPDF(sampleDoc + "\nCafé, naïve — and 日本 too.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) || !bytes.HasSuffix(data, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("a PDF reader cannot open it: %v", err)
	}
	var text strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		s, _ := r.Page(i).GetPlainText(nil)
		text.WriteString(s)
	}
	// The reader puts a line break between text objects; the spaces are real.
	got := strings.ReplaceAll(text.String(), "\n", "")
	for _, want := range []string{"Quarterly report", "Sales grew 12% in Q3, led by widgets.", "Highlights", "North", "total = 215", "Café"} {
		if !strings.Contains(got, want) {
			t.Errorf("PDF text lacks %q", want)
		}
	}

	long := strings.Repeat("A paragraph long enough to wrap across the width of the page several times over. ", 400)
	data, _ = MarkdownToPDF(long)
	r, err = pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if r.NumPage() < 3 {
		t.Fatalf("a long document has %d pages", r.NumPage())
	}
	// The cross-reference table points at each object.
	m := regexp.MustCompile(`startxref\n(\d+)`).FindSubmatch(data)
	off, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(data[off:], []byte("xref")) {
		t.Fatal("startxref does not point at the xref table")
	}
}

// A workbook can have several sheets and formulas (§22).
func TestWorkbookSheetsAndFormulas(t *testing.T) {
	data, err := CSVToXLSX("Budget", "month,amount\nJan,120\nFeb,95\nTotal,=SUM(B2:B3)\n## Sheet: Notes\nnote\nApproved\n## Sheet: Notes\nx\n1\n")
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := mimir.ReadWorkbook(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 3 || sheets[0].Name != "Budget" || sheets[1].Name != "Notes" || sheets[2].Name != "Notes 2" {
		t.Fatalf("sheets %+v", sheets)
	}
	if !strings.Contains(zipPart(t, data, "xl/worksheets/sheet1.xml"), `<f>SUM(B2:B3)</f>`) {
		t.Fatal("formula not written")
	}
	if sheets[1].Rows[1][0] != "Approved" {
		t.Fatalf("notes %+v", sheets[1].Rows)
	}
}

// pdf.create and document.create reach files.create with a format, so a
// name without an extension still becomes that kind of file.
func TestCreateToolFormats(t *testing.T) {
	s, _ := newStore(t)
	tool := &CreateTool{Store: s}
	ctx := WithConversation(context.Background(), "c1")
	res, err := tool.Execute(ctx, map[string]any{"name": "report", "content": sampleDoc, "format": "pdf"})
	if err != nil || res["name"] != "report.pdf" || res["kind"] != "pdf" {
		t.Fatalf("pdf = %v %v", res, err)
	}
	res, err = tool.Execute(ctx, map[string]any{"name": "Letter.md", "content": "Dear team,", "format": "docx"})
	if err != nil || res["name"] != "Letter.docx" || res["kind"] != "document" ||
		res["mime_type"] != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("docx = %v %v", res, err)
	}
}

// spreadsheet.analyze summarizes each column of a spreadsheet in the chat.
func TestAnalyzeSpreadsheet(t *testing.T) {
	s, _ := newStore(t)
	ctx := WithConversation(context.Background(), "c1")
	create := &CreateTool{Store: s}
	if _, err := create.Execute(ctx, map[string]any{"name": "Sales.xlsx",
		"content": "region,amount,date,owner\nNorth,\"1,200\",2026-01-05,Ana\nSouth,95,2026-02-01,Ben\nNorth,$300,2026-03-12,Ana\nEast,,2026-04-02,\n"}); err != nil {
		t.Fatal(err)
	}
	tool := &AnalyzeTool{Store: s}
	res, err := tool.Execute(ctx, map[string]any{"file": "sales"})
	if err != nil {
		t.Fatal(err)
	}
	sheet := res["sheets"].([]SheetSummary)[0]
	if sheet.Rows != 4 || len(sheet.Columns) != 4 || len(sheet.Sample) != 4 {
		t.Fatalf("sheet %+v", sheet)
	}
	amount := sheet.Columns[1]
	if amount.Type != "number" || amount.Filled != 3 || amount.Empty != 1 || *amount.Sum != 1595 || *amount.Max != 1200 || *amount.Min != 95 {
		t.Fatalf("amount %+v", amount)
	}
	if sheet.Columns[2].Type != "date" {
		t.Fatalf("date %+v", sheet.Columns[2])
	}
	region := sheet.Columns[0]
	if region.Type != "text" || region.Distinct != 3 || region.Top[0] != "North (2)" {
		t.Fatalf("region %+v", region)
	}
	if _, err := tool.Execute(ctx, map[string]any{"file": "missing.xlsx"}); err == nil {
		t.Fatal("a missing file was analyzed")
	}
	if _, err := tool.Execute(ctx, map[string]any{"file": "sales", "sheet": "Other"}); err == nil {
		t.Fatal("a missing sheet was analyzed")
	}
}
