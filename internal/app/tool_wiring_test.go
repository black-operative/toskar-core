package app

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/codeexec"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/speech"
)

// Tools that read or attach chat files get the file store. They were once
// registered before it existed and got none, so code's files were never
// attached and transcription failed.
func TestFileToolsHaveTheStore(t *testing.T) {
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.Artifacts == nil {
		t.Fatal("no file store")
	}
	get := func(id string) any {
		tool, err := a.Tools.Get(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return tool
	}
	if tool := get("code.execute").(*codeexec.Tool); tool.Store != a.Artifacts {
		t.Error("code.execute has no file store")
	}
	local := func(id string) any { return get(id).(*remotetools.Proxy).Local() }
	if tool := local("speech.transcribe").(*speech.TranscribeTool); tool.Store != a.Artifacts {
		t.Error("speech.transcribe has no file store")
	}
	if tool := local("speech.synthesize").(*speech.SynthesizeTool); tool.Store != a.Artifacts {
		t.Error("speech.synthesize has no file store")
	}
}
