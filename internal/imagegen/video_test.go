package imagegen

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
)

// videoFixture installs a video "model" made of the fixture's files.
func videoFixture(t *testing.T) (*fixture, *Engine, *artifacts.Store, context.Context) {
	t.Helper()
	f, _, st, ctx := setupTools(t)
	m := VideoCatalog()[0]
	byRole := map[string]string{"diffusion": "diffusion.gguf", "t5xxl": "encoder.gguf", "vae": "vae.safetensors", "tae": "vae.safetensors"}
	for i, file := range m.Files {
		data := f.files[byRole[file.Role]]
		f.files[file.Path] = data
		m.Files[i].Size, m.Files[i].SHA256 = int64(len(data)), sum(data)
	}
	vs := &Setup{ProgramDir: f.setup.ProgramDir, ModelsDir: filepath.Join(filepath.Dir(f.setup.ModelsDir), "video"),
		archive: f.setup.archive, fileURL: f.setup.fileURL, Catalog: func() []Model { return []Model{m} }}
	if err := vs.install(context.Background(), &job{}, m, false, *vs.archive); err != nil {
		t.Fatal(err)
	}
	return f, &Engine{Setup: vs, WorkDir: filepath.Join(t.TempDir(), "jobs"), What: "video generation"}, st, ctx
}

// A clip is made from a description and attached to the chat as a video
// that plays (Gungnir §27).
func TestVideoTool(t *testing.T) {
	f, eng, st, ctx := videoFixture(t)
	tool := &VideoTool{Engine: eng, Store: st}
	res, err := tool.Execute(ctx, map[string]any{"prompt": "Waves rolling onto a black sand beach at sunset", "seconds": float64(3), "seed": float64(9)})
	if err != nil {
		t.Fatal(err)
	}
	if res["name"] != "waves-rolling-onto-a-black.webm" || res["kind"] != "video" || res["frames"] != 49 || res["length_seconds"] != 3.0 || res["width"] != 832 || res["height"] != 480 {
		t.Fatalf("result %v", res)
	}
	a, _, err := st.Read(ctx, res["id"].(string))
	if err != nil || a.MimeType != "video/webm" {
		t.Fatalf("clip %+v %v", a, err)
	}
	got := args(t, f)
	if flag(got, "-M") != "vid_gen" || flag(got, "--video-frames") != "49" || flag(got, "--fps") != "16" || flag(got, "--flow-shift") != "3" ||
		filepath.Base(flag(got, "--t5xxl")) != "umt5-xxl-encoder-Q4_K_M.gguf" || filepath.Base(flag(got, "--vae")) != "wan2.2_vae.safetensors" ||
		!strings.HasSuffix(flag(got, "-o"), "out.webm") || flag(got, "-i") != "" || flag(got, "-n") == "" ||
		filepath.Base(flag(got, "--tae")) != "taew2_2.safetensors" {
		t.Fatalf("sd-cli args %q", got)
	}
}

// An image in the chat becomes the first frame, keeping its shape.
func TestVideoFromImage(t *testing.T) {
	f, eng, st, ctx := videoFixture(t)
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "tall.png", Producer: artifacts.ProducerUser, Data: samplePNG(t, 600, 1000)}); err != nil {
		t.Fatal(err)
	}
	res, err := (&VideoTool{Engine: eng, Store: st}).Execute(ctx, map[string]any{"prompt": "Make the leaves move in the wind", "image": "tall.png"})
	if err != nil {
		t.Fatal(err)
	}
	if res["width"] != 480 || res["height"] != 832 || res["frames"] != 33 {
		t.Fatalf("result %v", res)
	}
	if first := flag(args(t, f), "-i"); filepath.Base(first) != "first.png" {
		t.Fatalf("first frame %q", first)
	}
	if _, err := (&VideoTool{Engine: eng, Store: st}).Execute(ctx, map[string]any{"prompt": "x", "image": "missing.png"}); err == nil {
		t.Fatal("a missing image was accepted")
	}
}

func TestVideoSizes(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{{0, 0, 832, 480}, {1920, 1080, 832, 480}, {480, 832, 480, 832}, {100, 100, 256, 256}} {
		if w, h := videoSize(c.w, c.h, nil); w != c.ww || h != c.wh {
			t.Errorf("videoSize(%d, %d) = %d×%d, want %d×%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
	for secs, want := range map[float64]int{0: 33, 1: 17, 2: 33, 5: 81, 30: 81, 0.1: 9} {
		if got := frames(secs, 16); got != want {
			t.Errorf("frames(%v) = %d, want %d", secs, got, want)
		}
	}
}

func TestVideoNotSetUp(t *testing.T) {
	f := newFixture(t)
	vs := &Setup{ProgramDir: f.setup.ProgramDir, ModelsDir: t.TempDir(), archive: f.setup.archive, Catalog: VideoCatalog}
	ok, why := (&Engine{Setup: vs, What: "video generation"}).Available()
	if ok || !strings.HasPrefix(why, "video generation isn't set up yet") || !strings.Contains(why, "Wan 2.2 TI2V 5B, 8.5 GB to download") {
		t.Fatalf("available %v %q", ok, why)
	}
}
