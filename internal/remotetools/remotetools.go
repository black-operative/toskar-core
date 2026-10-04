// Package remotetools runs heavy tools, such as image generation and
// transcription, on whichever paired computer suits them (Gungnir §14–16).
// A tool is split in three: Prepare reads the chat files it needs where it
// was asked, Run does the work on any computer, and Finish saves what it
// made back to the chat. The computer that asked keeps the approval, the
// audit, and the files; the one that runs it only sees the work.
package remotetools

import (
	"context"
	"strings"

	"github.com/yeixio/toskar-core/internal/tools"

	"github.com/yeixio/toskar-core/internal/replylang"
)

// Protocol is the remote tool protocol version. A peer that does not
// serve it is treated as having no providers.
const Protocol = 1

// MaxBody caps a request or response between computers: a 25 MB file and
// its base64 encoding, with room for the arguments.
const MaxBody = 40 << 20

// File is a file that travels with a job, as bytes.
type File struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// Job is one call's work: its arguments and the chat files it reads.
type Job struct {
	Args  map[string]any `json:"args"`
	Files []File         `json:"files,omitempty"`
}

// Output is what the work made: the result and any files.
type Output struct {
	Result map[string]any `json:"result"`
	Files  []File         `json:"files,omitempty"`
}

// Portable is a tool that can run on another computer.
type Portable interface {
	tools.Tool
	// Available reports whether it can run on this computer, and why not.
	Available() (bool, string)
	// Prepare reads the chat files the call names, on the computer where
	// it was asked.
	Prepare(ctx context.Context, args map[string]any) (Job, error)
	// Run does the work. It reads and writes no chat files, so it can run
	// on any computer.
	Run(ctx context.Context, job Job) (Output, error)
	// Finish saves the files the work made to the chat and returns the
	// tool's result.
	Finish(ctx context.Context, out Output) (map[string]any, error)
	// Provider describes this computer's provider for the tool.
	Provider() Provider
}

// Provider states (Gungnir §16).
const (
	Healthy     = "healthy"
	Installing  = "installing"
	Failed      = "failed"
	Unavailable = "unavailable"
)

// Provider is one computer's ability to run one tool.
type Provider struct {
	Tool string `json:"tool"`
	// Name is what runs it, such as FLUX.2 [klein] 4B.
	Name  string `json:"name,omitempty"`
	State string `json:"state"`
	// Reason says why it is not healthy.
	Reason string `json:"reason,omitempty"`
	// Accelerated means a GPU does the work, which matters for images.
	Accelerated bool `json:"accelerated"`
	// Languages are the languages the provider works in, as base BCP 47
	// tags such as "de", when they matter, such as for speech (multilingual
	// spec §20). Empty means any language, or not known.
	Languages []string `json:"languages,omitempty"`
	// AutoDetect means the provider tells the language itself, such as
	// Whisper hearing which language is spoken.
	AutoDetect bool `json:"auto_detect,omitempty"`
}

// Speaks reports whether the provider works in lang ("" is any language).
// A provider that lists no languages is taken to work in any.
func (p Provider) Speaks(lang string) bool {
	if lang == "" || len(p.Languages) == 0 {
		return true
	}
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	for _, l := range p.Languages {
		if lb, _, _ := strings.Cut(strings.ToLower(l), "-"); lb == base {
			return true
		}
	}
	return false
}

// CallLanguage is the language a tool call works in: its "language"
// argument, or the language of its "text", detected on this computer.
func CallLanguage(args map[string]any) string {
	if l, _ := args["language"].(string); strings.TrimSpace(l) != "" {
		return strings.TrimSpace(l)
	}
	if text, _ := args["text"].(string); text != "" {
		if tag, ok := replylang.Detect(text); ok {
			return tag
		}
	}
	return ""
}

// Ready reports a provider that can run the tool now.
func (p Provider) Ready() bool { return p.State == Healthy }

// Execute runs a portable tool entirely on this computer.
func Execute(ctx context.Context, p Portable, args map[string]any) (map[string]any, error) {
	job, err := p.Prepare(ctx, args)
	if err != nil {
		return nil, err
	}
	out, err := p.Run(ctx, job)
	if err != nil {
		return nil, err
	}
	return p.Finish(ctx, out)
}
