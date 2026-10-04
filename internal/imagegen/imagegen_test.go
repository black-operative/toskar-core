package imagegen

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/store"
)

// fakeCLI stands in for sd-cli: it records its arguments and writes a small
// PNG where -o says.
const fakeCLI = `#!/bin/sh
printf '%s\n' "$@" > "$(dirname "$0")/args.txt"
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; fi
  if [ "$1" = "-p" ] && [ "$2" = "fail" ]; then echo "  |=====>    | 2/4" >&2; echo "error: out of memory" >&2; exit 1; fi
  shift
done
case "$out" in
  *.webm) printf '\032\105\337\243webm' > "$out" ;;
  *) cp "$(dirname "$0")/sample.png" "$out" ;;
esac
`

func samplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fixture is a download server with a stable-diffusion.cpp archive and the
// files of a small model, and a Setup pointed at it.
type fixture struct {
	setup    *Setup
	requests atomic.Int32
	files    map[string][]byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake sd-cli is a shell script")
	}
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	add := func(name string, data []byte, mode os.FileMode) {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	add("bin/sd-cli", []byte(fakeCLI), 0o755)
	add("bin/sample.png", samplePNG(t, 64, 64), 0o644)
	add("bin/libsd.1.dylib", []byte("lib"), 0o644)
	link := &zip.FileHeader{Name: "bin/libsd.dylib"}
	link.SetMode(os.ModeSymlink | 0o777)
	lw, _ := zw.CreateHeader(link)
	_, _ = lw.Write([]byte("libsd.1.dylib"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{files: map[string][]byte{
		"sd.zip":          zipBuf.Bytes(),
		"diffusion.gguf":  bytes.Repeat([]byte("d"), 5000),
		"encoder.gguf":    bytes.Repeat([]byte("e"), 3000),
		"vae.safetensors": bytes.Repeat([]byte("v"), 1000),
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		data, ok := f.files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	f.setup = &Setup{
		ProgramDir: filepath.Join(dir, "runtimes", "sdcpp"),
		ModelsDir:  filepath.Join(dir, "models", "images"),
		archive:    &Archive{Name: "sd.zip", Size: int64(zipBuf.Len()), SHA256: sum(zipBuf.Bytes())},
		fileURL:    func(file File) string { return srv.URL + "/" + file.Path },
	}
	return f
}

// model is the catalog's first model with the fixture's small files in
// place of the real ones.
func (f *fixture) model(t *testing.T) Model {
	t.Helper()
	m := Catalog()[0]
	byRole := map[string]string{"diffusion": "diffusion.gguf", "llm": "encoder.gguf", "vae": "vae.safetensors"}
	for i, file := range m.Files {
		data := f.files[byRole[file.Role]]
		f.files[file.Path] = data
		m.Files[i].Size, m.Files[i].SHA256 = int64(len(data)), sum(data)
	}
	return m
}

func waitJob(t *testing.T, s *Setup) Job {
	t.Helper()
	for i := 0; i < 200; i++ {
		if j := s.Status().Job; j != nil && !j.Running {
			return *j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("setup did not finish")
	return Job{}
}

func TestSetupInstallsProgramAndModel(t *testing.T) {
	f := newFixture(t)
	m := f.model(t)
	ctx := context.Background()
	st := f.setup.Status()
	if !st.Supported || st.Ready || st.Program || st.Active != "" {
		t.Fatalf("fresh status %+v", st)
	}
	j := &job{model: m.ID, running: true}
	archive, _ := f.setup.platform()
	if err := f.setup.install(ctx, j, m, true, archive); err != nil {
		t.Fatal(err)
	}
	if want := archive.Size + 9000; j.done.Load() != want {
		t.Errorf("progress %d, want %d", j.done.Load(), want)
	}
	st = f.setup.Status()
	if !st.Program || st.Active != m.ID || !st.Ready {
		t.Fatalf("after setup %+v", st)
	}
	cli := f.setup.CLI()
	if filepath.Base(cli) != "sd-cli" {
		t.Fatalf("cli %q", cli)
	}
	if target, err := os.Readlink(filepath.Join(filepath.Dir(cli), "libsd.dylib")); err != nil || target != "libsd.1.dylib" {
		t.Errorf("library link %q %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(f.setup.ProgramDir, "sd.zip")); !os.IsNotExist(err) {
		t.Error("the archive was kept")
	}
	// Installing again downloads nothing.
	before := f.requests.Load()
	if err := f.setup.install(ctx, &job{}, m, false, archive); err != nil || f.requests.Load() != before {
		t.Fatalf("reinstall: %v, %d requests", err, f.requests.Load()-before)
	}
	if err := f.setup.Remove(m.ID); err != nil || f.setup.Status().Ready {
		t.Fatalf("remove: %v", err)
	}
}

// A download picks up where it stopped, and a corrupt one is refused.
func TestDownloadResumesAndVerifies(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	data := f.files["diffusion.gguf"]
	url := f.setup.fileURL(File{Path: "diffusion.gguf"})
	path := filepath.Join(dir, "diffusion.gguf")
	if err := os.WriteFile(path+".part", data[:1200], 0o644); err != nil {
		t.Fatal(err)
	}
	var added int64
	if err := download(context.Background(), http.DefaultClient, url, path, int64(len(data)), sum(data), func(n int64) { added += n }); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) || added != int64(len(data)) {
		t.Fatalf("resumed download: %d bytes, progress %d", len(got), added)
	}
	err := download(context.Background(), http.DefaultClient, url, filepath.Join(dir, "bad"), int64(len(data)), sum([]byte("other")), func(int64) {})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("corrupt download: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.part")); !os.IsNotExist(err) {
		t.Error("the corrupt part was kept")
	}
}

func TestStartRefusesWithoutSpaceOrSupport(t *testing.T) {
	f := newFixture(t)
	f.setup.FreeBytes = func() (int64, error) { return 1 << 30, nil }
	if err := f.setup.Start("flux2-klein-4b"); err == nil || !strings.Contains(err.Error(), "free space") {
		t.Fatalf("start without space: %v", err)
	}
	f.setup.Sandboxed = true
	if st := f.setup.Status(); st.Supported || !strings.Contains(st.Unsupported, "App Sandbox") {
		t.Fatalf("sandboxed status %+v", st)
	}
	if err := f.setup.Start("nope"); err == nil {
		t.Fatal("unknown model accepted")
	}
}

func TestRecommendBySize(t *testing.T) {
	if got := Recommend(8 << 30).ID; got != "flux2-klein-4b" {
		t.Errorf("8 GB: %s", got)
	}
	if got := Recommend(32 << 30).ID; got != "flux2-klein-4b-q8" {
		t.Errorf("32 GB: %s", got)
	}
	for _, m := range Catalog() {
		for _, file := range m.Files {
			if revisions[file.Repo] == "" || len(file.SHA256) != 64 || file.Size == 0 {
				t.Errorf("%s: %s is not pinned", m.ID, file.Path)
			}
		}
	}
}

func TestFit(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{
		{0, 0, 1024, 1024}, {1000, 600, 1024, 576}, {4000, 4000, 1216, 1216}, {100, 3000, 256, 1536}, {1536, 1536, 1216, 1216},
	} {
		if w, h := fit(c.w, c.h); w != c.ww || h != c.wh {
			t.Errorf("fit(%d, %d) = %d×%d, want %d×%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
}

func setupTools(t *testing.T) (*fixture, *Engine, *artifacts.Store, context.Context) {
	t.Helper()
	f := newFixture(t)
	m := f.model(t)
	archive, _ := f.setup.platform()
	if err := f.setup.install(context.Background(), &job{}, m, true, archive); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL.Exec(`INSERT INTO conversations (id, title, created_at, updated_at) VALUES ('c1', 't', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	st := artifacts.NewStore(db.SQL, filepath.Join(dir, "artifacts"))
	eng := &Engine{Setup: f.setup, WorkDir: filepath.Join(dir, "jobs")}
	return f, eng, st, artifacts.WithConversation(context.Background(), "c1")
}

func args(t *testing.T, f *fixture) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(f.setup.CLI()), "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func flag(list []string, name string) string {
	for i, a := range list {
		if a == name && i+1 < len(list) {
			return list[i+1]
		}
	}
	return ""
}

// An image is made from a prompt and attached to the chat (Gungnir §17).
func TestGenerateTool(t *testing.T) {
	f, eng, st, ctx := setupTools(t)
	tool := &GenerateTool{Engine: eng, Store: st}
	if ok, why := tool.Available(); !ok {
		t.Fatalf("unavailable: %s", why)
	}
	res, err := tool.Execute(ctx, map[string]any{"prompt": "A red fox in the snow, at dawn.", "width": float64(1000), "height": float64(600), "seed": float64(42)})
	if err != nil {
		t.Fatal(err)
	}
	if res["name"] != "a-red-fox-in-the.png" || res["kind"] != "image" || res["seed"] != int64(42) || res["width"] != 1024 || res["height"] != 576 {
		t.Fatalf("result %v", res)
	}
	a, data, err := st.Read(ctx, res["id"].(string))
	if err != nil || a.MimeType != "image/png" || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("image %+v %v", a, err)
	}
	got := args(t, f)
	if flag(got, "-p") != "A red fox in the snow, at dawn." || flag(got, "--steps") != "4" || flag(got, "-s") != "42" ||
		flag(got, "-W") != "1024" || flag(got, "-H") != "576" || flag(got, "-r") != "" ||
		filepath.Base(flag(got, "--diffusion-model")) != "flux-2-klein-4b-Q4_0.gguf" || filepath.Base(flag(got, "--llm")) != "Qwen3-4B-Q4_K_M.gguf" {
		t.Fatalf("sd-cli args %q", got)
	}
	if _, err := tool.Execute(ctx, map[string]any{"prompt": "fail"}); err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Fatalf("failure: %v", err)
	}
	if _, err := tool.Execute(ctx, map[string]any{"prompt": " "}); err == nil {
		t.Fatal("empty prompt accepted")
	}
	entries, _ := os.ReadDir(eng.WorkDir)
	if len(entries) != 0 {
		t.Errorf("job folders left: %d", len(entries))
	}
}

// An image in the chat is changed from an instruction, keeping its shape.
func TestEditTool(t *testing.T) {
	f, eng, st, ctx := setupTools(t)
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "Photo.PNG", Producer: artifacts.ProducerUser, Data: samplePNG(t, 1600, 1200)}); err != nil {
		t.Fatal(err)
	}
	tool := &EditTool{Engine: eng, Store: st}
	res, err := tool.Execute(ctx, map[string]any{"file": "photo.png", "prompt": "Make it night"})
	if err != nil {
		t.Fatal(err)
	}
	if res["name"] != "Photo-edited.png" || res["width"] != 1024 || res["height"] != 768 {
		t.Fatalf("result %v", res)
	}
	got := args(t, f)
	if ref := flag(got, "-r"); filepath.Base(ref) != "reference.png" {
		t.Fatalf("reference %q in %q", ref, got)
	}
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "notes.txt", Producer: artifacts.ProducerUser, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{{"file": "notes.txt", "prompt": "x"}, {"file": "", "prompt": "x"}, {"file": "missing.png", "prompt": "x"}} {
		if _, err := tool.Execute(ctx, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestNotSetUp(t *testing.T) {
	f := newFixture(t)
	eng := &Engine{Setup: f.setup, WorkDir: t.TempDir()}
	ok, why := eng.Available()
	if ok || !strings.Contains(why, "isn't set up yet") || !strings.Contains(why, "FLUX.2 [klein] 4B, 5.2 GB to download") {
		t.Fatalf("available %v %q", ok, why)
	}
	if _, err := eng.Generate(context.Background(), Request{Prompt: "a cat"}); err == nil {
		t.Fatal("generated without setup")
	}
}

// Start runs in the background; a failed download is reported in Status
// and the program installed before it stays.
func TestStartReportsFailure(t *testing.T) {
	f := newFixture(t)
	if err := f.setup.Start("flux2-klein-4b"); err != nil {
		t.Fatal(err)
	}
	j := waitJob(t, f.setup)
	if j.Stage != "model" || !strings.Contains(j.Error, "could not be downloaded") || j.Total <= j.Done {
		t.Fatalf("job %+v", j)
	}
	if st := f.setup.Status(); !st.Program || st.Ready {
		t.Fatalf("status %+v", st)
	}
}
