package training

import (
	"encoding/csv"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Column names that mark data which changes after a model is trained.
var volatileColumns = []string{
	"price", "cost", "msrp", "sale", "discount", "stock", "qty", "quantity", "inventory",
	"available", "availability", "in_stock", "instock", "on_hand", "onhand", "updated",
	"updated_at", "date", "eta", "lead_time", "leadtime", "status", "rate", "fee",
}

// Column names that mark catalog or lookup data.
var catalogColumns = []string{"sku", "part", "part_number", "upc", "ean", "model", "brand", "size", "id", "item", "product", "name"}

var dateRe = regexp.MustCompile(`\b(19|20)\d{2}-\d{2}-\d{2}\b|\b\d{1,2}/\d{1,2}/(19|20)?\d{2}\b`)

// Classify recommends how material should feed a specialized AI.
func Classify(filename, text string) Recommendation {
	examples, parseErr := ParseExamples(filename, text)
	var rec Recommendation
	rec.ExampleCount = len(examples)
	rec.CanTrain = len(examples) > 0

	header, rows := tableShape(filename, text)
	if header != nil {
		var volatile, catalog []string
		for _, h := range header {
			col := strings.ToLower(strings.TrimSpace(h))
			col = strings.ReplaceAll(col, " ", "_")
			if matchesColumn(volatileColumns, col) {
				volatile = append(volatile, h)
			} else if matchesColumn(catalogColumns, col) {
				catalog = append(catalog, h)
			}
		}
		if len(volatile) > 0 {
			rec.Signals = append(rec.Signals, Signal{Kind: "changing_columns", Detail: strings.Join(volatile, ", ")})
		}
		if len(catalog) > 0 {
			rec.Signals = append(rec.Signals, Signal{Kind: "lookup_columns", Detail: strings.Join(catalog, ", ")})
		}
		if !rec.CanTrain {
			rec.Use = UseKnowledge
			rec.Reasons = append(rec.Reasons, fmt.Sprintf("This is a table of %d rows, not conversation examples.", rows))
			if len(volatile) > 0 {
				rec.Reasons = append(rec.Reasons, fmt.Sprintf("Columns like %s change over time. Connected knowledge picks up each change without retraining.", strings.Join(volatile, ", ")))
			} else {
				rec.Reasons = append(rec.Reasons, "The AI can look up rows when it needs them, and you can edit the file later.")
			}
			return rec
		}
	}

	if !rec.CanTrain {
		rec.Use = UseKnowledge
		if parseErr != nil {
			rec.Reasons = append(rec.Reasons, "Toskar could not read examples from this material ("+parseErr.Error()+").")
		}
		rec.Reasons = append(rec.Reasons,
			"This is reference material, not examples of conversations.",
			"Training does not reliably teach facts from documents. Connecting it lets the AI quote it and stay current.")
		if dateRe.MatchString(text) || ContainsVolatileFacts(text) {
			rec.Signals = append(rec.Signals, Signal{Kind: "changing_facts", Detail: "dates, prices, or stock levels"})
		}
		return rec
	}

	volatileAnswers := 0
	for _, msgs := range examples {
		for _, m := range msgs {
			if m.Role == "assistant" && ContainsVolatileFacts(m.Content) {
				volatileAnswers++
				break
			}
		}
	}
	rec.Reasons = append(rec.Reasons, fmt.Sprintf("Found %d example conversations. Examples teach the AI how to respond.", len(examples)))
	if volatileAnswers*5 >= len(examples) && volatileAnswers > 0 {
		rec.Use = UseBoth
		rec.Signals = append(rec.Signals, Signal{Kind: "changing_facts", Detail: answersStating(volatileAnswers)})
		rec.Reasons = append(rec.Reasons,
			answersStating(volatileAnswers)+". Train on the pattern, and keep the facts connected so they stay current.")
		return rec
	}
	rec.Use = UseTraining
	return rec
}

// ChoiceWarning explains a conflict between the user's choice and the
// recommendation. It returns an error when the choice is impossible.
func ChoiceWarning(rec Recommendation, chosen Use) (string, error) {
	if !chosen.Valid() {
		return "", fmt.Errorf("use must be training, knowledge, or both")
	}
	if chosen.trains() && !rec.CanTrain {
		return "", fmt.Errorf("no training examples were found in this material, so it can only be connected knowledge. Add question and answer pairs or chat transcripts to train")
	}
	switch {
	case chosen == UseTraining && rec.Use == UseKnowledge:
		return "This material looks like data that changes. If it is trained into the model, the AI keeps answering with the old values after the data changes. Connected knowledge stays current.", nil
	case chosen == UseTraining && rec.Use == UseBoth:
		return "Some answers contain prices, stock, or SKUs. Training alone bakes those values into the model. Connect the source data as knowledge too.", nil
	case chosen == UseKnowledge && rec.Use == UseTraining:
		return "Connected as knowledge, these examples are searchable text. They will not change how the AI responds.", nil
	}
	return "", nil
}

func matchesColumn(names []string, col string) bool {
	for _, n := range names {
		if col == n || strings.HasPrefix(col, n+"_") || strings.HasSuffix(col, "_"+n) {
			return true
		}
	}
	return false
}

// tableShape returns the header and row count of a CSV or TSV file.
func tableShape(filename, text string) ([]string, int) {
	sep := ','
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv":
	case ".tsv":
		sep = '\t'
	default:
		return nil, 0
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, 0
	}
	return rows[0], len(rows) - 1
}

// answersStating reads "1 answer states" or "3 answers state".
func answersStating(n int) string {
	if n == 1 {
		return "1 answer states prices, stock, or SKUs"
	}
	return fmt.Sprintf("%d answers state prices, stock, or SKUs", n)
}
