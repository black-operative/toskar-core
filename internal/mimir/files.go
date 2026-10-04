package mimir

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Passage is a piece of a file, with where it came from.
type Passage struct {
	Title string
	Body  string
}

// AttachableExtensions are the file types chat can read: everything Mimir
// connects, plus code and configuration files read as text.
var AttachableExtensions = append(append([]string(nil), SupportedExtensions...),
	".py", ".js", ".ts", ".tsx", ".jsx", ".go", ".rs", ".java", ".kt", ".c", ".h", ".cpp", ".hpp", ".cs",
	".rb", ".php", ".swift", ".sh", ".sql", ".yaml", ".yml", ".toml", ".xml", ".css", ".ini", ".log")

// Attachable reports whether chat can read a file of this name.
func Attachable(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range AttachableExtensions {
		if e == ext {
			return true
		}
	}
	return false
}

// FilePassages reads one file the way Mimir reads a connected source: tables
// become one passage per row, PDFs are read page by page, and prose is split
// into passages of about chunkRunes.
func FilePassages(name string, raw []byte) ([]Passage, error) {
	if !Attachable(name) {
		return nil, fmt.Errorf("%s is not a file type Toskar can read", filepath.Base(name))
	}
	if len(raw) > maxFileBytes {
		return nil, fmt.Errorf("%s is larger than %d MB", filepath.Base(name), maxFileBytes>>20)
	}
	var docs []document
	var err error
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx":
		docs, err = parseXLSX(name, raw)
	case ".pdf":
		docs, err = parsePDF(name, raw)
		if errors.Is(err, ErrNoText) {
			// Chat rereads attachments every turn, which is too slow for text
			// recognition; a knowledge source recognizes the pages once.
			err = fmt.Errorf("%s is a scanned PDF with no text to read. Connect it on the Knowledge page, which reads scanned pages with text recognition", filepath.Base(name))
		}
	default:
		var d document
		d, err = parseDocument(name, raw)
		docs = []document{d}
	}
	if err != nil {
		return nil, err
	}
	chunks := chunkDocuments(docs)
	out := make([]Passage, len(chunks))
	for i, c := range chunks {
		out[i] = Passage(c)
	}
	return out, nil
}

// SelectPassages keeps passages within budget runes. When everything fits, it
// keeps them all in order. Otherwise it keeps the ones that share the most
// words with the question, still in file order, so a long file contributes
// the parts that matter. With no question it keeps the beginning, unless
// onlyMatching is set, in which case passages that share no word with the
// question are left out.
func SelectPassages(passages []Passage, question string, budget int, onlyMatching bool) []Passage {
	total := 0
	for _, p := range passages {
		total += utf8.RuneCountInString(p.Body)
	}
	if total <= budget {
		return passages
	}
	terms := map[string]bool{}
	for _, t := range strings.Split(matchQuery(question), " OR ") {
		if t = strings.Trim(t, `"`); t != "" {
			terms[t] = true
		}
	}
	type scored struct {
		i     int
		score int
	}
	order := make([]scored, len(passages))
	for i, p := range passages {
		s := 0
		if len(terms) > 0 {
			words := termRe.FindAllString(strings.ToLower(p.Title+" "+p.Body), -1)
			for _, w := range words {
				if terms[strings.Trim(w, "/-")] {
					s++
				}
			}
		}
		order[i] = scored{i, s}
	}
	// Highest score first; earlier passages win ties, so no question keeps
	// the start of the file.
	sort.SliceStable(order, func(a, b int) bool { return order[a].score > order[b].score })
	keep := make([]bool, len(passages))
	used := 0
	for _, o := range order {
		if onlyMatching && o.score == 0 {
			break
		}
		n := utf8.RuneCountInString(passages[o.i].Body)
		if used+n > budget {
			continue
		}
		keep[o.i] = true
		used += n
	}
	var out []Passage
	for i, p := range passages {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}
