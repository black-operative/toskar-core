package webfixtures

import (
	"context"
	"fmt"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/tools"
)

// RegisterMedia replaces the image and video tools with ones that save a
// tiny picture or clip at once, so the quality set checks that a request
// for one gets one, without an image model (#204 follow-up). Like the real
// tools, each saves its file to the chat and returns its id, name, and
// kind.
func RegisterMedia(r *tools.Registry, store *artifacts.Store) {
	save := func(ctx context.Context, name string, data []byte) (map[string]any, error) {
		if store == nil {
			return nil, fmt.Errorf("no file store")
		}
		a, err := store.Save(ctx, artifacts.Input{ConversationID: artifacts.ConversationFrom(ctx), Name: name, Producer: artifacts.ProducerAssistant, Data: data})
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind, "seconds": 0.1}, nil
	}
	r.Register(mediaTool{id: "image.generate", run: func(ctx context.Context, _ map[string]any) (map[string]any, error) {
		return save(ctx, "picture.png", tinyPNG)
	}})
	r.Register(mediaTool{id: "image.edit", run: func(ctx context.Context, _ map[string]any) (map[string]any, error) {
		return save(ctx, "changed.png", tinyPNG)
	}})
	r.Register(mediaTool{id: "video.generate", run: func(ctx context.Context, _ map[string]any) (map[string]any, error) {
		return save(ctx, "clip.mp4", tinyMP4)
	}})
}

type mediaTool struct {
	id  string
	run func(ctx context.Context, args map[string]any) (map[string]any, error)
}

func (t mediaTool) ID() string          { return t.id }
func (t mediaTool) DisplayName() string { return t.id }
func (t mediaTool) Description() string { return t.id }
func (t mediaTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.run(ctx, args)
}

// tinyPNG is a 1×1 picture, and tinyMP4 the start of an MP4 file.
var (
	tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	tinyMP4 = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
)
