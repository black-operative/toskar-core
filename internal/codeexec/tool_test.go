package codeexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/store"
)

// systemPython stands in for the managed environment, so tests need no
// download.
type systemPython struct{ path string }

func (s systemPython) Ensure(context.Context, pyenv.Spec, pyenv.Progress) (string, error) {
	return s.path, nil
}
func (s systemPython) Env() []string                 { return nil }
func (s systemPython) Unavailable(pyenv.Spec) string { return "" }

func newTool(t *testing.T) (*Tool, *artifacts.Store, context.Context) {
	t.Helper()
	sb := Detect()
	if ok, why := sb.Available(); !ok {
		t.Skip("no sandbox here: " + why)
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL.Exec(`INSERT INTO conversations (id, title, created_at, updated_at) VALUES ('c1', 't', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	st := artifacts.NewStore(db.SQL, filepath.Join(t.TempDir(), "artifacts"))
	root := filepath.Dir(filepath.Dir(realPath(py)))
	tool := &Tool{Python: systemPython{py}, Sandbox: sb, Store: st, WorkDir: filepath.Join(t.TempDir(), "runs"), PythonRoot: root}
	return tool, st, artifacts.WithConversation(context.Background(), "c1")
}

// Code runs in the sandbox: it prints, reads a file from the chat, and
// writes files that come back as attachments (Gungnir §20).
func TestCodeRunsAndReturnsFiles(t *testing.T) {
	tool, st, ctx := newTool(t)
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "sales.csv", Producer: artifacts.ProducerUser, Data: []byte("region,amount\nNorth,120\nSouth,95\n")}); err != nil {
		t.Fatal(err)
	}
	res, err := tool.Execute(ctx, map[string]any{"files": []any{"sales.csv"}, "code": `
import csv
rows = list(csv.DictReader(open("sales.csv")))
total = sum(int(r["amount"]) for r in rows)
print("total", total)
with open("summary.csv", "w") as f:
    f.write("total\n%d\n" % total)
open("ignored.bin", "wb").write(b"x")
`})
	if err != nil {
		t.Fatal(err, res)
	}
	if !strings.Contains(res["stdout"].(string), "total 215") || res["exit_code"] != 0 {
		t.Fatalf("result %v", res)
	}
	files, _ := res["files"].([]map[string]any)
	if len(files) != 1 || files[0]["name"] != "summary.csv" {
		t.Fatalf("files %v", res["files"])
	}
	if skipped, _ := res["skipped_files"].([]string); len(skipped) != 1 || skipped[0] != "ignored.bin" {
		t.Fatalf("skipped %v", res["skipped_files"])
	}
	list, _ := st.List(ctx, "c1")
	if len(list) != 2 {
		t.Fatalf("chat files %d", len(list))
	}
}

// The sandbox blocks the network, the user's files, and writing outside
// the run's folder; a failing script reports its error.
func TestSandboxBlocks(t *testing.T) {
	tool, _, ctx := newTool(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder")
	}
	outside := filepath.Join(os.TempDir(), "yggdrasil-sandbox-escape")
	defer os.Remove(outside)
	res, err := tool.Execute(ctx, map[string]any{"code": `
import socket
def attempt(name, f):
    try:
        f()
        print(name, "ALLOWED")
    except Exception as e:
        print(name, "blocked")
attempt("network", lambda: socket.create_connection(("1.1.1.1", 443), timeout=3))
import os
attempt("home", lambda: os.listdir(` + quote(home) + `))
attempt("write", lambda: open(` + quote(outside) + `, "w").write("x"))
raise SystemExit(3)
`})
	if err != nil {
		t.Fatal(err)
	}
	out := res["stdout"].(string)
	for _, what := range []string{"network", "home", "write"} {
		if !strings.Contains(out, what+" blocked") {
			t.Errorf("%s was not blocked:\n%s", what, out)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("the code wrote outside its folder")
	}
	if res["exit_code"] != 3 {
		t.Fatalf("exit code %v", res["exit_code"])
	}
}

// Code that runs too long is stopped.
func TestCodeTimeLimit(t *testing.T) {
	tool, _, ctx := newTool(t)
	tool.TimeLimit = 2 * time.Second
	started := time.Now()
	_, err := tool.Execute(ctx, map[string]any{"code": "while True:\n    pass\n"})
	if err == nil || !strings.Contains(err.Error(), "longer than 2 seconds") {
		t.Fatalf("err %v", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("not stopped promptly")
	}
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `\'`) + "'" }

// Without a sandbox, code does not run at all.
func TestNoSandboxNoCode(t *testing.T) {
	tool := &Tool{Python: systemPython{"python3"}, Sandbox: unsupported{reason: "no sandbox for plan9"}}
	if ok, why := tool.Available(); ok || !strings.Contains(why, "plan9") {
		t.Fatalf("available %v %q", ok, why)
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"code": "print(1)"}); err == nil || !strings.Contains(err.Error(), "cannot run") {
		t.Fatalf("err %v", err)
	}
}

func TestSandboxCommands(t *testing.T) {
	p := seatbeltProfile("/data/runs/run-1", []string{"/data/runtimes/python"})
	for _, want := range []string{"(deny network*)", "(deny file-write*)", `(subpath "/Users")`, `(allow file-read* (subpath "/data/runtimes/python"))`, `(allow file-read* file-write* (subpath "/data/runs/run-1"))`} {
		if !strings.Contains(p, want) {
			t.Errorf("profile lacks %s", want)
		}
	}
	if strings.Index(p, "(deny file-read*") > strings.Index(p, `(allow file-read* (subpath "/data/runtimes/python"))`) {
		t.Error("allow rules must come after the deny they override")
	}
	args := strings.Join(bubblewrapArgs("/data/runs/run-1", []string{"/data/py"}, []string{"/data/py/bin/python", "-c", "x"}), " ")
	for _, want := range []string{"--unshare-all", "--die-with-parent", "--clearenv", "--ro-bind /data/py /data/py", "--bind /data/runs/run-1 /work", "--chdir /work", "-- /data/py/bin/python -c x"} {
		if !strings.Contains(args, want) {
			t.Errorf("bwrap args lack %q: %s", want, args)
		}
	}
	if runtime.GOOS == "windows" {
		if ok, _ := Detect().Available(); ok {
			t.Error("a sandbox was reported on Windows")
		}
	}
}
