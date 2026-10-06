package simple

import (
	"context"
	"encoding/csv"
	"fmt"
	"regexp"
	"strings"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

var (
	// fileAskRe matches a request to produce a file: a verb that makes
	// something, then the kind of file, close together.
	fileAskRe = regexp.MustCompile(`(?i)\b(create|make|generate|export|build|produce|write|save|put|give me|turn)\b[^.?!\n]{0,60}?\b(spreadsheet|excel|xlsx|csv|workbook|file|document|markdown|report|json|pdf|docx|word document|word file)\b`)
	// fileAsRe matches asking for something in a file format, such as "can I
	// have that as a PDF?" (#281).
	fileAsRe = regexp.MustCompile(`(?i)\b(as|into|in) an? (spreadsheet|excel file|xlsx|csv|pdf|docx|word document|word file|markdown file|json file)\b`)
	// fileNamedRe matches a request to make a document with a name given,
	// such as "save it as recipes.pdf".
	fileNamedRe = regexp.MustCompile(`(?i)\b(create|make|generate|export|build|produce|write|save|put|give me|turn)\b[^.?!\n]{0,60}?\b[\w][\w-]*\.(pdf|docx|xlsx|csv|tsv|md|txt)\b`)
	// howToRe matches a question about how to do it, which isn't a request.
	howToRe = regexp.MustCompile(`(?i)^\s*(how|what|why|where|when)\b`)
	// fileNameRe finds a file name the user gave.
	fileNameRe = regexp.MustCompile(`(?i)\b([\w][\w .-]{0,60}?\.(xlsx|csv|tsv|md|txt|json|html|py|js|ts|go|sql|yaml|yml|pdf|docx))\b`)
)

// fileRequest is a file the user asked for.
type fileRequest struct {
	Name string
	// Table is true for .xlsx, .csv, and .tsv, which the model writes as CSV.
	Table bool
}

// askedForFile reports whether a message asks for a file, and which. A name
// in the message wins; otherwise the kind of file decides the extension.
func askedForFile(prompt string) (fileRequest, bool) {
	if howToRe.MatchString(prompt) {
		return fileRequest{}, false
	}
	m := fileAskRe.FindStringSubmatch(prompt)
	if m == nil {
		m = fileAsRe.FindStringSubmatch(prompt)
	}
	if m == nil && fileNamedRe.MatchString(prompt) {
		m = []string{"", "", "file"}
	}
	if m == nil {
		return fileRequest{}, false
	}
	name := ""
	if n := fileNameRe.FindStringSubmatch(prompt); n != nil {
		name = strings.TrimSpace(n[1])
		// "named tires.xlsx" — keep only the last word as the name.
		if i := strings.LastIndexAny(name, " "); i >= 0 {
			name = name[i+1:]
		}
	}
	if name == "" {
		switch strings.ToLower(m[2]) {
		case "spreadsheet", "excel", "excel file", "xlsx", "workbook":
			name = "spreadsheet.xlsx"
		case "csv":
			name = "data.csv"
		case "json", "json file":
			name = "data.json"
		case "pdf":
			name = "document.pdf"
		case "docx", "word document", "word file":
			name = "document.docx"
		default:
			name = "document.md"
		}
	}
	lower := strings.ToLower(name)
	table := strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".tsv")
	return fileRequest{Name: name, Table: table}, true
}

var fenceRe = regexp.MustCompile("(?s)^\\s*```[\\w-]*\\s*\\n(.*?)\\n?```\\s*$")

// fileBody removes a code fence or a leading sentence a model may add around
// the contents.
func fileBody(content string, table bool) string {
	content = strings.TrimSpace(content)
	if m := fenceRe.FindStringSubmatch(content); m != nil {
		content = strings.TrimSpace(m[1])
	} else if i := strings.Index(content, "```"); i >= 0 {
		rest := content[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			content = strings.TrimSpace(rest[:end])
		}
	}
	if table {
		// Drop prose lines before the header: the first line with a comma
		// starts the table.
		lines := strings.Split(content, "\n")
		for i, l := range lines {
			if strings.Contains(l, ",") {
				content = strings.Join(lines[i:], "\n")
				break
			}
		}
	}
	return strings.TrimSpace(content)
}

func rowCount(csvText string) int {
	r := csv.NewReader(strings.NewReader(csvText))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		return 0
	}
	return len(rows) - 1
}

// makeFileFirst writes a file the user asked for (spec §28). The model writes
// only the contents, as plain text, and Yggdrasil saves the file, so even a
// small model that cannot call tools produces it. It runs only when the
// profile allows files.create without asking. It returns the reply to show.
func makeFileFirst(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, role string, messages []pluginapi.ChatMessage, prompt string) (string, *pluginapi.GenerationMetrics, bool) {
	req, ok := askedForFile(prompt)
	if !ok || !strings.EqualFold(tools.PolicyForProfile(profile, "files.create"), tools.PolicyAllow) {
		return "", nil, false
	}
	format := "Write only the contents of the file " + req.Name + ", with no explanation and no code fence."
	if req.Table {
		format = "Write only the contents of the spreadsheet " + req.Name + " as CSV: a header row, then one row per item. No explanation, no code fence."
	}
	// Add the instruction to the user's turn, so roles keep alternating.
	ask := append([]pluginapi.ChatMessage(nil), messages...)
	last := &ask[len(ask)-1]
	last.Content += "\n\n" + format + " Use the conversation and any reference material above."
	env.Emit(EventMakingFile, map[string]any{"name": req.Name})
	content, metrics, err := generateText(ctx, env, role, ask)
	if err != nil {
		return "", nil, false
	}
	body := fileBody(content, req.Table)
	if body == "" {
		return "", nil, false
	}
	result, err := env.ExecuteTool(ctx, "files.create", map[string]any{"name": req.Name, "content": body})
	if err != nil {
		return fmt.Sprintf("I couldn't save %s: %s", req.Name, publicToolError(err)), metrics, true
	}
	saved, _ := result["name"].(string)
	if saved == "" {
		saved = req.Name
	}
	reply := fmt.Sprintf("Here is **%s**. It's attached below.", saved)
	if req.Table {
		if n := rowCount(body); n > 0 {
			reply = fmt.Sprintf("Here is **%s**, with %d %s. It's attached below.", saved, n, map[bool]string{true: "row", false: "rows"}[n == 1])
		}
	}
	return reply, metrics, true
}

// makeTheFile asks a model that told the person to make a file to make it.
const makeTheFile = "Don't tell the user to make the file themselves: you can make it. Call files.create with a file name, such as recipe.pdf or notes.docx, and the contents as Markdown (CSV for a spreadsheet). Then tell the user briefly that it's attached."

// EventMakingFile tells the UI that Yggdrasil is writing a file.
const EventMakingFile = "chat.making_file"
