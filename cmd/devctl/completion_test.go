package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionPrintsEachShell(t *testing.T) {
	for shell, path := range completionPaths {
		var out bytes.Buffer
		if err := completionCommand([]string{shell}, &out); err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out.Bytes(), want) {
			t.Fatalf("%s: printed script does not match %s", shell, path)
		}
		for _, cmd := range []string{"version", "about", "paths", "automations", "mcp", "join", "join-token", "network", "leave", "completion"} {
			if !strings.Contains(out.String(), cmd) {
				t.Fatalf("%s script does not offer %q", shell, cmd)
			}
		}
		// Status, nodes, and models are HTTP routes, not toskarctl commands.
		for _, word := range []string{"status", "nodes", "models"} {
			if strings.Contains(out.String(), word) {
				t.Fatalf("%s script offers %q, which is not a toskarctl command", shell, word)
			}
		}
	}
}

func TestCompletionRejectsUnknownShell(t *testing.T) {
	for _, args := range [][]string{nil, {"powershell"}, {"bash", "zsh"}} {
		err := completionCommand(args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "usage: toskarctl completion <bash|zsh|fish>") {
			t.Fatalf("args %v: want usage error, got %v", args, err)
		}
	}
}

func TestCompletionScriptsParse(t *testing.T) {
	for shell, path := range completionPaths {
		bin, err := exec.LookPath(shell)
		if err != nil {
			t.Logf("%s is not installed; skipping its syntax check", shell)
			continue
		}
		if out, err := exec.Command(bin, "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("%s -n %s: %v\n%s", shell, path, err, out)
		}
	}
}

func TestBashCompletionCandidates(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	script, err := filepath.Abs(completionPaths["bash"])
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		line string
		want string
	}{
		{"toskarctl ''", "version about paths automations mcp join join-token network leave completion"},
		{"toskarctl j", "join join-token"},
		{"toskarctl join-token ''", "create list revoke"},
		{"toskarctl join --f", "--fingerprint"},
		{"toskarctl a", "about automations"},
		{"toskarctl completion ''", "bash zsh fish"},
		{"toskarctl automations p", "pause"},
		{"toskarctl automations create --schedule ''", "once daily weekly monthly interval cron manual"},
		{"toskarctl automations update abc --noti", "--notify"},
		{"toskarctl paths ''", ""},
	}
	// yggctl, the name from before the rename, completes the same way (#237).
	cases = append(cases, struct {
		line string
		want string
	}{"yggctl j", "join join-token"})
	for _, tc := range cases {
		src := `source "$1"; eval "COMP_WORDS=(` + tc.line + `)"; COMP_CWORD=$((${#COMP_WORDS[@]}-1)); _toskarctl; echo "${COMPREPLY[*]}"`
		out, err := exec.Command(bash, "-c", src, "bash", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.line, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.line, got, tc.want)
		}
	}
}
