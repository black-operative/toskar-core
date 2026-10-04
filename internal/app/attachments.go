package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Shares of the model's window, in characters, for attached files. Files
// attached to this message get most of it; files from earlier in the chat
// contribute the passages that match the question.
const (
	attachedShare = 0.35
	earlierShare  = 0.10
	maxAttachRune = 24000
)

// resolveAttachments checks the files attached to a message and files any
// that were uploaded before the chat existed under it.
func (a *App) resolveAttachments(ctx context.Context, conversationID string, ids []string) ([]artifacts.Artifact, error) {
	if len(ids) == 0 || a.Artifacts == nil {
		return nil, nil
	}
	var out []artifacts.Artifact
	for _, id := range ids {
		art, err := a.Artifacts.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("an attached file is no longer available; attach it again")
		}
		if art.ConversationID == "" && conversationID != "" {
			if err := a.Artifacts.Attach(ctx, id, conversationID); err != nil {
				return nil, err
			}
			art.ConversationID = conversationID
		}
		if art.ConversationID != conversationID {
			return nil, fmt.Errorf("%s belongs to another chat; attach it again here", art.Name)
		}
		out = append(out, art)
	}
	return out, nil
}

func fileRef(a artifacts.Artifact) contracts.FileRef {
	return contracts.FileRef{ID: a.ID, Name: a.Name, MimeType: a.MimeType, Kind: a.Kind, Size: a.Size, Producer: a.Producer}
}

func fileRefs(list []artifacts.Artifact) []contracts.FileRef {
	out := make([]contracts.FileRef, 0, len(list))
	for _, a := range list {
		out = append(out, fileRef(a))
	}
	return out
}

// attachmentBlock reads the files attached to this message, and the ones
// attached or made earlier in the chat, as reference material, so "add a
// column to that spreadsheet" works on a file the assistant produced. It
// returns "" when the chat has no files.
// AudioToolID is the tool an audio attachment's note points to.
const AudioToolID = "speech.transcribe"

// ImageEditToolID is the tool an image attachment's note points to, when
// images can be edited here.
const ImageEditToolID = "image.edit"

// toolReady reports a registered tool that can run on this computer now.
func (a *App) toolReady(id string) bool {
	if a.Tools == nil {
		return false
	}
	t, err := a.Tools.Get(id)
	if err != nil {
		return false
	}
	if av, ok := t.(interface{ Available() (bool, string) }); ok {
		ready, _ := av.Available()
		return ready
	}
	return true
}

func (e *chatExecEnv) attachmentBlock(ctx context.Context, prompt string) string {
	if e.app == nil || e.app.Artifacts == nil || e.conversationID == "" {
		return ""
	}
	current := map[string]bool{}
	for _, a := range e.attachments {
		current[a.ID] = true
	}
	all, err := e.app.Artifacts.List(ctx, e.conversationID)
	if err != nil {
		return ""
	}
	var earlier []artifacts.Artifact
	for _, a := range all {
		if !current[a.ID] {
			earlier = append(earlier, a)
		}
	}
	if len(e.attachments) == 0 && len(earlier) == 0 {
		return ""
	}
	window := e.ContextLimit() * 4
	budget := func(share float64, files int) int {
		n := int(float64(window) * share)
		if n > maxAttachRune {
			n = maxAttachRune
		}
		if files > 1 {
			n /= files
		}
		return n
	}
	var b strings.Builder
	read := func(list []artifacts.Artifact, share float64, question string, now, onlyMatching bool) {
		for _, art := range list {
			_, data, err := e.app.Artifacts.Read(ctx, art.ID)
			if err != nil {
				continue
			}
			label := "attached to this message by the user"
			switch {
			case now:
			case art.Producer == artifacts.ProducerAssistant:
				label = "you made earlier in this chat"
			default:
				label = "attached earlier in this chat by the user"
			}
			if artifacts.IsAudio(art.Name) {
				// Audio is not text; the model transcribes it when it needs to.
				fmt.Fprintf(&b, "\nAudio file %s: %s. To know what it says, call %s with {\"file\": %q}.\n", label, art.Name, AudioToolID, art.Name)
				continue
			}
			if art.Kind == "image" {
				// Images are not text either; they can be changed when image
				// editing is set up.
				fmt.Fprintf(&b, "\nImage %s: %s. You cannot see it.", label, art.Name)
				if imagegen.IsEditable(art.Name) && e.app.toolReady(ImageEditToolID) {
					fmt.Fprintf(&b, " To change it, call %s with {\"file\": %q}.", ImageEditToolID, art.Name)
				}
				b.WriteString("\n")
				continue
			}
			passages, err := mimir.FilePassages(art.Name, data)
			if err != nil {
				fmt.Fprintf(&b, "\nA file %s, %s, could not be read: %s\n", label, art.Name, err.Error())
				continue
			}
			picked := mimir.SelectPassages(passages, question, budget(share, len(list)), onlyMatching)
			if len(picked) == 0 {
				continue
			}
			if e.trace != nil {
				e.trace.attachment(art, len(picked), len(passages))
			}
			fmt.Fprintf(&b, "\nFile %s: %s", label, art.Name)
			if len(picked) < len(passages) {
				fmt.Fprintf(&b, " (the %d of %d parts that best match the question)", len(picked), len(passages))
			}
			b.WriteString("\n")
			for _, p := range picked {
				fmt.Fprintf(&b, "[%s]\n%s\n", p.Title, strings.TrimSpace(p.Body))
			}
		}
	}
	// A message that only says "summarize this" still gets the whole file,
	// or its beginning; earlier files only add what matches.
	read(e.attachments, attachedShare, prompt, true, false)
	read(earlier, earlierShare, prompt, false, true)
	return strings.TrimSpace(b.String())
}
