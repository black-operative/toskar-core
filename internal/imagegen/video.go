package imagegen

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/remotetools"
)

// Video limits. A clip is a few seconds: each frame costs as much as an
// image, many times over.
const (
	defaultSeconds = 2
	maxSeconds     = 5
	videoRunLimit  = 75 * time.Minute
	// videoNegative steers Wan away from the usual faults.
	videoNegative = "blurry, low quality, distorted, deformed, static, frozen frame, watermark, text, subtitles, extra limbs, bad hands, bad faces"
)

// VideoRequest is one clip to make, from a description or from an image.
type VideoRequest struct {
	Prompt  string
	Width   int
	Height  int
	Seconds float64
	Seed    int64
	// Image, when set, is the first frame to bring to life.
	Image     []byte
	ImageName string
}

// VideoResult is a made clip.
type VideoResult struct {
	WebM    []byte
	Width   int
	Height  int
	Frames  int
	Length  float64
	Seed    int64
	Seconds float64
	Model   string
}

// videoSize picks a size: the request's, or the image's shape, rounded to
// multiples of 32 and kept to about 832×480 pixels or fewer.
func videoSize(w, h int, img []byte) (int, int) {
	if w <= 0 || h <= 0 {
		w, h = 832, 480
		if img != nil {
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(img)); err == nil {
				switch {
				case cfg.Height > cfg.Width*11/10:
					w, h = 480, 832
				case cfg.Width > cfg.Height*11/10:
					w, h = 832, 480
				default:
					w, h = 640, 640
				}
			}
		}
	}
	const budget = 832 * 480
	if w*h > budget {
		scale := math.Sqrt(float64(budget) / float64(w*h))
		w, h = int(float64(w)*scale), int(float64(h)*scale)
	}
	round := func(n int) int { return min(max((n+16)/32*32, 256), 1280) }
	return round(w), round(h)
}

// frames is the frame count for a length: Wan takes 4k+1 frames.
func frames(seconds float64, fps int) int {
	if seconds <= 0 {
		seconds = defaultSeconds
	}
	seconds = math.Min(seconds, maxSeconds)
	n := int(math.Round(seconds * float64(fps) / 4))
	return max(n, 2)*4 + 1
}

// GenerateVideo makes a clip.
func (e *Engine) GenerateVideo(ctx context.Context, req VideoRequest) (VideoResult, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return VideoResult{}, fmt.Errorf("prompt required: describe the clip")
	}
	if utf8.RuneCountInString(prompt) > maxPrompt {
		return VideoResult{}, fmt.Errorf("the prompt is longer than %d characters", maxPrompt)
	}
	if ok, why := e.Available(); !ok {
		return VideoResult{}, fmt.Errorf("%s", why)
	}
	cli := e.Setup.CLI()
	model, files, err := e.Setup.paths()
	if err != nil {
		return VideoResult{}, err
	}
	if model.Kind != "video" {
		return VideoResult{}, fmt.Errorf("%s does not make video", model.Name)
	}
	if req.Image != nil {
		if _, _, err := image.DecodeConfig(bytes.NewReader(req.Image)); err != nil {
			return VideoResult{}, fmt.Errorf("%s is not a PNG or JPEG image", req.ImageName)
		}
	}
	w, h := videoSize(req.Width, req.Height, req.Image)
	n := frames(req.Seconds, model.FPS)
	seed := req.Seed
	if seed <= 0 {
		seed = rand.Int64N(1 << 31)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := os.MkdirAll(e.WorkDir, 0o700); err != nil {
		return VideoResult{}, err
	}
	dir, err := os.MkdirTemp(e.WorkDir, "video-")
	if err != nil {
		return VideoResult{}, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.webm")
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	args := []string{"-M", "vid_gen",
		"--diffusion-model", files["diffusion"], "--vae", files["vae"], "--t5xxl", files["t5xxl"],
		"-p", prompt, "-n", videoNegative, "-W", strconv.Itoa(w), "-H", strconv.Itoa(h),
		"--video-frames", strconv.Itoa(n), "--fps", strconv.Itoa(model.FPS),
		"--steps", strconv.Itoa(model.Steps), "--cfg-scale", f(model.CFG), "--flow-shift", f(model.FlowShift),
		"--sampling-method", "euler", "-s", strconv.FormatInt(seed, 10), "-o", out,
		"--offload-to-cpu", "--diffusion-fa",
	}
	if tae := files["tae"]; tae != "" {
		// The tiny decoder turns the clip into frames in seconds.
		args = append(args, "--tae", tae)
	} else {
		// The full VAE decodes every frame at once unless tiled, which
		// needs more memory than most computers have.
		args = append(args, "--vae-tiling", "--temporal-tiling")
	}
	if req.Image != nil {
		ext := strings.ToLower(filepath.Ext(req.ImageName))
		if ext != ".jpg" && ext != ".jpeg" {
			ext = ".png"
		}
		first := filepath.Join(dir, "first"+ext)
		if err := os.WriteFile(first, req.Image, 0o600); err != nil {
			return VideoResult{}, err
		}
		args = append(args, "-i", first)
	}
	start := time.Now()
	data, err := runCLI(ctx, cli, dir, args, out, "clip", videoRunLimit)
	if err != nil {
		return VideoResult{}, err
	}
	return VideoResult{WebM: data, Width: w, Height: h, Frames: n, Length: math.Round(float64(n-1)/float64(model.FPS)*10) / 10,
		Seed: seed, Seconds: time.Since(start).Seconds(), Model: model.Name}, nil
}

// webmMagic starts a WebM (EBML) file.
var webmMagic = []byte{0x1A, 0x45, 0xDF, 0xA3}

// VideoTool is video.generate: a short clip from a description, or from an
// image in the chat.
type VideoTool struct {
	Engine *Engine
	Store  *artifacts.Store
}

func (t *VideoTool) ID() string                     { return "video.generate" }
func (t *VideoTool) DisplayName() string            { return "Generate Video" }
func (t *VideoTool) Description() string            { return "Make a short clip from a description or an image" }
func (t *VideoTool) Available() (bool, string)      { return t.Engine.Available() }
func (t *VideoTool) Provider() remotetools.Provider { return t.Engine.provider(t.ID()) }
func (t *VideoTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return remotetools.Execute(ctx, t, args)
}

// Prepare reads the image to bring to life, if there is one.
func (t *VideoTool) Prepare(ctx context.Context, args map[string]any) (remotetools.Job, error) {
	ref, _ := args["image"].(string)
	if ref = strings.TrimSpace(ref); ref == "" {
		return remotetools.Job{Args: args}, nil
	}
	a, data, err := findImage(ctx, t.Store, ref)
	if err != nil {
		return remotetools.Job{}, err
	}
	return remotetools.Job{Args: args, Files: []remotetools.File{{Name: a.Name, Data: data}}}, nil
}

// Run makes the clip.
func (t *VideoTool) Run(ctx context.Context, job remotetools.Job) (remotetools.Output, error) {
	prompt, _ := job.Args["prompt"].(string)
	seconds, _ := job.Args["seconds"].(float64)
	req := VideoRequest{Prompt: prompt, Width: intArg(job.Args["width"]), Height: intArg(job.Args["height"]), Seconds: seconds, Seed: int64(intArg(job.Args["seed"]))}
	if len(job.Files) == 1 {
		req.Image, req.ImageName = job.Files[0].Data, job.Files[0].Name
	}
	res, err := t.Engine.GenerateVideo(ctx, req)
	if err != nil {
		return remotetools.Output{}, err
	}
	name, _ := job.Args["name"].(string)
	name = artifacts.CleanName(strings.TrimSpace(name))
	if name == "" || name == "file" {
		name = nameFrom(prompt)
	}
	name = strings.TrimSuffix(name, filepath.Ext(name)) + ".webm"
	return remotetools.Output{
		Result: map[string]any{"width": res.Width, "height": res.Height, "frames": res.Frames, "length_seconds": res.Length,
			"seed": res.Seed, "model": res.Model, "seconds": math.Round(res.Seconds)},
		Files: []remotetools.File{{Name: name, Data: res.WebM}},
	}, nil
}

// Finish attaches the clip to the chat.
func (t *VideoTool) Finish(ctx context.Context, out remotetools.Output) (map[string]any, error) {
	if len(out.Files) != 1 || !bytes.HasPrefix(out.Files[0].Data, webmMagic) {
		return nil, fmt.Errorf("no clip came back")
	}
	f := out.Files[0]
	a, err := t.Store.Save(ctx, artifacts.Input{ConversationID: artifacts.ConversationFrom(ctx), Name: artifacts.CleanName(f.Name), Producer: artifacts.ProducerAssistant, Data: f.Data})
	if err != nil {
		return nil, err
	}
	res := map[string]any{}
	for k, v := range out.Result {
		res[k] = v
	}
	res["id"], res["name"], res["kind"] = a.ID, a.Name, a.Kind
	res["note"] = "The clip is attached to your answer, where it plays. Do not describe it as if you can see it; the same seed with the same prompt makes it again."
	return res, nil
}
