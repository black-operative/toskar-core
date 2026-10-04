package artifacts

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/mimir"
)

// Limits for spreadsheet.analyze, so a large workbook still answers quickly.
const (
	analyzeSheets  = 10
	analyzeColumns = 50
	analyzeRows    = 200_000
	sampleRows     = 5
	topValues      = 5
)

// AnalyzeTool is spreadsheet.analyze (Gungnir §22): it summarizes a
// spreadsheet attached to the chat or made in it, sheet by sheet and
// column by column, so the answer can work from the whole file rather than
// the part that fits in the prompt. It only reads.
type AnalyzeTool struct {
	Store *Store
}

func (t *AnalyzeTool) ID() string          { return "spreadsheet.analyze" }
func (t *AnalyzeTool) DisplayName() string { return "Analyze Spreadsheet" }
func (t *AnalyzeTool) Description() string {
	return "Summarize a spreadsheet in this chat"
}

// ColumnSummary describes one column.
type ColumnSummary struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // number, date, text, or empty
	Filled   int      `json:"filled"`
	Empty    int      `json:"empty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Mean     *float64 `json:"mean,omitempty"`
	Sum      *float64 `json:"sum,omitempty"`
	Distinct int      `json:"distinct,omitempty"`
	// Top are the most common values of a text column.
	Top []string `json:"top,omitempty"`
}

// SheetSummary describes one sheet.
type SheetSummary struct {
	Name    string          `json:"name"`
	Rows    int             `json:"rows"`
	Columns []ColumnSummary `json:"columns"`
	Sample  [][]string      `json:"sample"`
}

// Execute finds the file by id or name and summarizes it.
func (t *AnalyzeTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	ref, _ := args["file"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("file required: the name of a spreadsheet in this chat")
	}
	a, data, err := t.find(ctx, ref)
	if err != nil {
		return nil, err
	}
	sheets, err := readTable(a.Name, data)
	if err != nil {
		return nil, err
	}
	only, _ := args["sheet"].(string)
	var out []SheetSummary
	for _, sh := range sheets {
		if only != "" && !strings.EqualFold(sh.Name, strings.TrimSpace(only)) {
			continue
		}
		out = append(out, summarize(sh))
		if len(out) == analyzeSheets {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no sheet named %q", a.Name, only)
	}
	return map[string]any{"file": a.Name, "sheets": out}, nil
}

// find returns the artifact for an id, or the newest one in this chat
// with that name.
func (t *AnalyzeTool) find(ctx context.Context, ref string) (Artifact, []byte, error) {
	if a, data, err := t.Store.Read(ctx, ref); err == nil {
		return a, data, nil
	}
	conv := conversationFrom(ctx)
	if conv == "" {
		return Artifact{}, nil, fmt.Errorf("no spreadsheet named %q in this chat", ref)
	}
	list, err := t.Store.List(ctx, conv)
	if err != nil {
		return Artifact{}, nil, err
	}
	want := strings.ToLower(filepath.Base(ref))
	for i := len(list) - 1; i >= 0; i-- {
		a := list[i]
		name := strings.ToLower(a.Name)
		if name == want || strings.TrimSuffix(name, filepath.Ext(name)) == want {
			return t.Store.Read(ctx, a.ID)
		}
	}
	return Artifact{}, nil, fmt.Errorf("no spreadsheet named %q in this chat", ref)
}

func readTable(name string, data []byte) ([]mimir.Sheet, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx":
		return mimir.ReadWorkbook(data)
	case ".csv", ".tsv":
		r := csv.NewReader(strings.NewReader(string(data)))
		if strings.EqualFold(filepath.Ext(name), ".tsv") {
			r.Comma = '\t'
		}
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		rows, err := r.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("%s is not valid CSV: %w", name, err)
		}
		return []mimir.Sheet{{Name: strings.TrimSuffix(name, filepath.Ext(name)), Rows: rows}}, nil
	}
	return nil, fmt.Errorf("%s is not a spreadsheet (.xlsx, .csv, or .tsv)", name)
}

var dateLayouts = []string{"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00", "01/02/2006", "02.01.2006", "Jan 2, 2006"}

func isDate(s string) bool {
	for _, l := range dateLayouts {
		if _, err := time.Parse(l, s); err == nil {
			return true
		}
	}
	return false
}

// number reads a cell as a number, allowing thousands separators, a
// currency sign, and a trailing percent.
func number(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "$€£¥")
	s = strings.ReplaceAll(s, ",", "")
	pct := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(s, "%")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if pct {
		f /= 100
	}
	return f, true
}

func round(f float64) *float64 {
	r := math.Round(f*10000) / 10000
	return &r
}

func summarize(sh mimir.Sheet) SheetSummary {
	s := SheetSummary{Name: sh.Name, Columns: []ColumnSummary{}, Sample: [][]string{}}
	if len(sh.Rows) == 0 {
		return s
	}
	header := sh.Rows[0]
	body := sh.Rows[1:]
	if len(body) > analyzeRows {
		body = body[:analyzeRows]
	}
	s.Rows = len(body)
	cols := len(header)
	for _, r := range body {
		cols = max(cols, len(r))
	}
	cols = min(cols, analyzeColumns)
	for c := 0; c < cols; c++ {
		col := ColumnSummary{Name: fmt.Sprintf("Column %d", c+1)}
		if c < len(header) && strings.TrimSpace(header[c]) != "" {
			col.Name = strings.TrimSpace(header[c])
		}
		var nums []float64
		dates, texts := 0, map[string]int{}
		for _, r := range body {
			v := ""
			if c < len(r) {
				v = strings.TrimSpace(r[c])
			}
			if v == "" {
				col.Empty++
				continue
			}
			col.Filled++
			if f, ok := number(v); ok {
				nums = append(nums, f)
			} else if isDate(v) {
				dates++
			}
			texts[v]++
		}
		switch {
		case col.Filled == 0:
			col.Type = "empty"
		case len(nums) == col.Filled:
			col.Type = "number"
			lo, hi, sum := nums[0], nums[0], 0.0
			for _, f := range nums {
				lo, hi, sum = math.Min(lo, f), math.Max(hi, f), sum+f
			}
			col.Min, col.Max, col.Sum, col.Mean = round(lo), round(hi), round(sum), round(sum/float64(len(nums)))
		case dates == col.Filled:
			col.Type = "date"
		default:
			col.Type = "text"
		}
		if col.Type != "number" {
			col.Distinct = len(texts)
			type kv struct {
				v string
				n int
			}
			var all []kv
			for v, n := range texts {
				all = append(all, kv{v, n})
			}
			sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n || (all[i].n == all[j].n && all[i].v < all[j].v) })
			for _, x := range all[:min(topValues, len(all))] {
				col.Top = append(col.Top, fmt.Sprintf("%s (%d)", x.v, x.n))
			}
		}
		s.Columns = append(s.Columns, col)
	}
	for _, r := range body[:min(sampleRows, len(body))] {
		s.Sample = append(s.Sample, r[:min(len(r), cols)])
	}
	return s
}
