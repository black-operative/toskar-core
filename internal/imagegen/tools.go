package imagegen

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/remotetools"
)

// GenerateTool is image.generate: make an image from a description.
type GenerateTool struct {
	Engine *Engine
	Store  *artifacts.Store
}

func (t *GenerateTool) ID() string                     { return "image.generate" }
func (t *GenerateTool) DisplayName() string            { return "Generate Image" }
func (t *GenerateTool) Description() string            { return "Make an image from a description" }
func (t *GenerateTool) Available() (bool, string)      { return t.Engine.Available() }
func (t *GenerateTool) Provider() remotetools.Provider { return t.Engine.provider(t.ID()) }
func (t *GenerateTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return remotetools.Execute(ctx, t, args)
}

// Prepare needs no chat files.
func (t *GenerateTool) Prepare(_ context.Context, args map[string]any) (remotetools.Job, error) {
	return remotetools.Job{Args: args}, nil
}

// Run makes the image.
func (t *GenerateTool) Run(ctx context.Context, job remotetools.Job) (remotetools.Output, error) {
	prompt, _ := job.Args["prompt"].(string)
	res, err := t.Engine.Generate(ctx, Request{Prompt: prompt, Width: intArg(job.Args["width"]), Height: intArg(job.Args["height"]), Seed: int64(intArg(job.Args["seed"]))})
	if err != nil {
		return remotetools.Output{}, err
	}
	name, _ := job.Args["name"].(string)
	return output(res, name, prompt), nil
}

// Finish attaches the image to the chat.
func (t *GenerateTool) Finish(ctx context.Context, out remotetools.Output) (map[string]any, error) {
	return finish(ctx, t.Store, out)
}

// EditTool is image.edit: change an image in this chat from an instruction.
type EditTool struct {
	Engine *Engine
	Store  *artifacts.Store
}

func (t *EditTool) ID() string                     { return "image.edit" }
func (t *EditTool) DisplayName() string            { return "Edit Image" }
func (t *EditTool) Description() string            { return "Change an image in this chat from an instruction" }
func (t *EditTool) Available() (bool, string)      { return t.Engine.Available() }
func (t *EditTool) Provider() remotetools.Provider { return t.Engine.provider(t.ID()) }
func (t *EditTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return remotetools.Execute(ctx, t, args)
}

// Prepare reads the image to change from the chat.
func (t *EditTool) Prepare(ctx context.Context, args map[string]any) (remotetools.Job, error) {
	ref, _ := args["file"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return remotetools.Job{}, fmt.Errorf("file required: the name of an image in this chat")
	}
	a, data, err := findImage(ctx, t.Store, ref)
	if err != nil {
		return remotetools.Job{}, err
	}
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	if name, _ := out["name"].(string); strings.TrimSpace(name) == "" {
		out["name"] = strings.TrimSuffix(a.Name, filepath.Ext(a.Name)) + "-edited"
	}
	return remotetools.Job{Args: out, Files: []remotetools.File{{Name: a.Name, Data: data}}}, nil
}

// Run changes the image.
func (t *EditTool) Run(ctx context.Context, job remotetools.Job) (remotetools.Output, error) {
	if len(job.Files) != 1 {
		return remotetools.Output{}, fmt.Errorf("an edit needs one image")
	}
	prompt, _ := job.Args["prompt"].(string)
	res, err := t.Engine.Generate(ctx, Request{Prompt: prompt, Seed: int64(intArg(job.Args["seed"])), Reference: job.Files[0].Data, RefName: job.Files[0].Name,
		Width: intArg(job.Args["width"]), Height: intArg(job.Args["height"])})
	if err != nil {
		return remotetools.Output{}, err
	}
	name, _ := job.Args["name"].(string)
	return output(res, name, prompt), nil
}

// Finish attaches the changed image to the chat.
func (t *EditTool) Finish(ctx context.Context, out remotetools.Output) (map[string]any, error) {
	return finish(ctx, t.Store, out)
}

// IsEditable reports an image image.edit can read.
func IsEditable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg":
		return true
	}
	return false
}

// findImage returns an image by id, or the newest one in this chat with that
// name.
func findImage(ctx context.Context, store *artifacts.Store, ref string) (artifacts.Artifact, []byte, error) {
	if a, data, err := store.Read(ctx, ref); err == nil && IsEditable(a.Name) {
		return a, data, nil
	}
	list, err := store.List(ctx, artifacts.ConversationFrom(ctx))
	if err != nil {
		return artifacts.Artifact{}, nil, err
	}
	for i := len(list) - 1; i >= 0; i-- {
		if strings.EqualFold(list[i].Name, ref) && IsEditable(list[i].Name) {
			return store.Read(ctx, list[i].ID)
		}
	}
	return artifacts.Artifact{}, nil, fmt.Errorf("no PNG or JPEG image named %q in this chat", ref)
}

// output is a made image as a file named for the request.
func output(res Result, name, prompt string) remotetools.Output {
	name = artifacts.CleanName(strings.TrimSpace(name))
	if name == "" || name == "file" {
		name = nameFrom(prompt)
	}
	if strings.ToLower(filepath.Ext(name)) != ".png" {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + ".png"
	}
	return remotetools.Output{
		Result: map[string]any{"width": res.Width, "height": res.Height, "seed": res.Seed, "model": res.Model,
			"seconds": math.Round(res.Seconds*10) / 10},
		Files: []remotetools.File{{Name: name, Data: res.PNG}},
	}
}

// finish saves the image to the chat and completes the result.
func finish(ctx context.Context, store *artifacts.Store, out remotetools.Output) (map[string]any, error) {
	if len(out.Files) != 1 || !bytes.HasPrefix(out.Files[0].Data, []byte("\x89PNG")) {
		return nil, fmt.Errorf("no image came back")
	}
	f := out.Files[0]
	a, err := store.Save(ctx, artifacts.Input{ConversationID: artifacts.ConversationFrom(ctx), Name: artifacts.CleanName(f.Name), Producer: artifacts.ProducerAssistant, Data: f.Data})
	if err != nil {
		return nil, err
	}
	res := map[string]any{}
	for k, v := range out.Result {
		res[k] = v
	}
	res["id"], res["name"], res["kind"] = a.ID, a.Name, a.Kind
	res["note"] = "The image is attached to your answer, where it is shown. Do not describe it as if you can see it; the same seed with the same prompt makes it again."
	return res, nil
}

// nameFrom makes a file name from the first words of a prompt.
func nameFrom(prompt string) string {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(prompt)) {
		w = strings.Trim(w, ".,;:!?\"'()[]")
		if w == "" {
			continue
		}
		words = append(words, w)
		if len(words) == 5 {
			break
		}
	}
	name := artifacts.CleanName(strings.Join(words, "-"))
	if name == "" || name == "file" {
		return "image"
	}
	return name
}

func intArg(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
