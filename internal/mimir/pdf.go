package mimir

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// maxPDFPages caps the pages read from one file.
const maxPDFPages = 2000

// ErrNoText means a PDF has no text layer: it is scanned, and needs text
// recognition (OCR) before it can be read.
var ErrNoText = contracts.NewError("NO_TEXT_FOUND", nil, errors.New("no text found. Scanned PDFs need text recognition (OCR) before Toskar can read them"))

// Recognizer reads text from scanned PDF pages. *ocr.Recognizer implements it.
type Recognizer interface {
	// RecognizePDF returns the text of the given 1-based pages, keyed by page.
	RecognizePDF(ctx context.Context, raw []byte, pages []int) (map[int]string, error)
}

// SetRecognizer lets Mimir read scanned pages of connected PDFs. Without it,
// a PDF with no text layer fails with ErrNoText.
func (s *Store) SetRecognizer(r Recognizer) { s.recognizer = r }

// PDFText returns a PDF's text, pages separated by blank lines, for the
// training classifier.
func PDFText(name string, raw []byte) (string, error) {
	docs, err := parsePDF(name, raw)
	if err != nil {
		return "", err
	}
	parts := make([]string, len(docs))
	for i, d := range docs {
		parts[i] = d.Text
	}
	return strings.Join(parts, "\n\n"), nil
}

// pdfPages returns each page's text layer, "" for a page without one.
func pdfPages(raw []byte) (texts []string, err error) {
	// The parser panics on some malformed files; report those as errors.
	defer func() {
		if r := recover(); r != nil {
			texts, err = nil, fmt.Errorf("could not read the PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("not a readable PDF: %w", err)
	}
	pages := r.NumPage()
	if pages > maxPDFPages {
		return nil, fmt.Errorf("more than %d pages", maxPDFPages)
	}
	texts = make([]string, pages)
	for i := 1; i <= pages; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", i, err)
		}
		texts[i-1] = text
	}
	return texts, nil
}

// pageDocs makes a document per page with text, so retrieved passages cite
// the page they came from.
func pageDocs(name string, texts []string) []document {
	var docs []document
	for i, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		docs = append(docs, document{Name: fmt.Sprintf("%s p.%d", name, i+1), Text: text})
	}
	return docs
}

// parsePDF reads the text layer of each page. A scanned PDF has nothing to
// read and returns ErrNoText.
func parsePDF(name string, raw []byte) ([]document, error) {
	texts, err := pdfPages(raw)
	if err != nil {
		return nil, err
	}
	docs := pageDocs(name, texts)
	if len(docs) == 0 {
		return nil, ErrNoText
	}
	return docs, nil
}

// pdfReader reads a PDF into page documents.
type pdfReader func(name string, raw []byte) ([]document, error)

// readPDF reads a PDF's text layer and, with a recognizer, recognizes the
// pages that have none, such as scanned pages in an otherwise digital file.
func (s *Store) readPDF(ctx context.Context) pdfReader {
	if s.recognizer == nil {
		return parsePDF
	}
	return func(name string, raw []byte) ([]document, error) {
		texts, err := pdfPages(raw)
		if err != nil {
			return nil, err
		}
		var blank []int
		for i, t := range texts {
			if strings.TrimSpace(t) == "" {
				blank = append(blank, i+1)
			}
		}
		if len(blank) > 0 {
			recognized, err := s.recognize(ctx, raw, blank)
			if err != nil {
				// Fail rather than index part of the file without saying so.
				if len(blank) < len(texts) {
					return nil, fmt.Errorf("%d scanned pages could not be read: %w", len(blank), err)
				}
				return nil, err
			}
			for page, text := range recognized {
				if page >= 1 && page <= len(texts) {
					texts[page-1] = text
				}
			}
		}
		docs := pageDocs(name, texts)
		if len(docs) == 0 {
			return nil, fmt.Errorf("no text found, even with text recognition")
		}
		return docs, nil
	}
}

// recognize runs text recognition, remembering the result by the file's
// content, so a folder source does not recognize every scanned PDF again
// whenever one of its files changes.
func (s *Store) recognize(ctx context.Context, raw []byte, pages []int) (map[int]string, error) {
	sum := sha256.Sum256(raw)
	cache := filepath.Join(s.dir, "ocr-cache", hex.EncodeToString(sum[:])+".json")
	if b, err := os.ReadFile(cache); err == nil {
		var texts map[int]string
		if json.Unmarshal(b, &texts) == nil && hasPages(texts, pages) {
			return texts, nil
		}
	}
	texts, err := s.recognizer.RecognizePDF(ctx, raw, pages)
	if err != nil {
		return nil, err
	}
	if b, err := json.Marshal(texts); err == nil && os.MkdirAll(filepath.Dir(cache), 0o755) == nil {
		_ = os.WriteFile(cache, b, 0o600)
	}
	return texts, nil
}

func hasPages(texts map[int]string, pages []int) bool {
	for _, p := range pages {
		if _, ok := texts[p]; !ok {
			return false
		}
	}
	return true
}
