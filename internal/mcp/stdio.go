package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// stdioTransport runs a server as a child process and speaks newline
// delimited JSON over its standard input and output.
type stdioTransport struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	incoming chan []byte
	stderr   *tail
	writeMu  sync.Mutex
	exited   chan struct{}
	waitErr  error
	once     sync.Once
}

// StartProcess starts command with args and env added to this process's
// environment. Lines the server writes to stderr go to log.
func StartProcess(command string, args []string, env map[string]string, dir string, log func(string)) (Transport, error) {
	path := searchPath()
	bin, err := lookPath(command, path)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = mergeEnv(os.Environ(), path, env)
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	t := &stdioTransport{cmd: cmd, stdin: stdin, incoming: make(chan []byte, 16), stderr: &tail{max: 16 << 10, log: log}, exited: make(chan struct{})}
	cmd.Stderr = t.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start %s: %w", command, err)
	}
	go func() {
		defer close(t.incoming)
		r := bufio.NewReaderSize(stdout, 64<<10)
		for {
			line, err := r.ReadBytes('\n')
			line = bytes.TrimSpace(line)
			if len(line) > 0 && (line[0] == '{' || line[0] == '[') {
				t.incoming <- line
			} else if len(line) > 0 && log != nil {
				// Some servers print banners to stdout; they are not protocol.
				log(string(line))
			}
			if err != nil {
				break
			}
		}
		t.waitErr = cmd.Wait()
		close(t.exited)
	}()
	return t, nil
}

func (t *stdioTransport) Send(ctx context.Context, msg []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	select {
	case <-t.exited:
		return t.Err()
	default:
	}
	_, err := t.stdin.Write(append(msg, '\n'))
	if err != nil {
		return t.exitErr(err)
	}
	return nil
}

func (t *stdioTransport) Incoming() <-chan []byte { return t.incoming }

func (t *stdioTransport) Err() error {
	select {
	case <-t.exited:
		return t.exitErr(t.waitErr)
	default:
		return nil
	}
}

// exitErr explains a stopped server with the end of what it printed.
func (t *stdioTransport) exitErr(err error) error {
	msg := "the server stopped"
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			msg = fmt.Sprintf("the server stopped (exit code %d)", exit.ExitCode())
		}
	}
	if last := t.stderr.lastLine(); last != "" {
		msg += ": " + last
	}
	return errors.New(msg)
}

func (t *stdioTransport) Close() error {
	t.once.Do(func() {
		_ = t.stdin.Close()
		select {
		case <-t.exited:
		case <-time.After(2 * time.Second):
			terminate(t.cmd)
			select {
			case <-t.exited:
			case <-time.After(3 * time.Second):
				_ = t.cmd.Process.Kill()
			}
		}
		// The server is gone; end what it started that is still running, such
		// as the node process npx runs. A server that quits on its own when
		// stdin closes used to leave those behind (#231).
		reapGroup(t.cmd)
	})
	return nil
}

// Stderr returns the end of what the server printed.
func (t *stdioTransport) Stderr() string { return t.stderr.String() }

// tail keeps the end of a stream and passes each line on.
type tail struct {
	mu   sync.Mutex
	buf  []byte
	max  int
	part []byte
	log  func(string)
}

func (w *tail) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	if w.log != nil {
		w.part = append(w.part, p...)
		for {
			i := bytes.IndexByte(w.part, '\n')
			if i < 0 {
				break
			}
			if line := strings.TrimSpace(string(w.part[:i])); line != "" {
				w.log(line)
			}
			w.part = w.part[i+1:]
		}
	}
	return len(p), nil
}

func (w *tail) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

func (w *tail) lastLine() string {
	lines := strings.Split(strings.TrimSpace(w.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if len(l) > 300 {
				l = l[:300] + "…"
			}
			return l
		}
	}
	return ""
}

// runtimeHelp says how to get a command servers commonly need, in plain
// words, when it is missing.
var runtimeHelp = map[string]struct{ name, help string }{
	"npx":    {"Node.js", "Install Node.js from https://nodejs.org (or run `brew install node`), then try again."},
	"node":   {"Node.js", "Install Node.js from https://nodejs.org (or run `brew install node`), then try again."},
	"npm":    {"Node.js", "Install Node.js from https://nodejs.org (or run `brew install node`), then try again."},
	"bunx":   {"Bun", "Install Bun from https://bun.sh, then try again."},
	"uvx":    {"uv", "Install uv from https://docs.astral.sh/uv/ (or run `brew install uv`), then try again."},
	"uv":     {"uv", "Install uv from https://docs.astral.sh/uv/ (or run `brew install uv`), then try again."},
	"docker": {"Docker", "Install and start Docker Desktop from https://www.docker.com, then try again."},
	"python": {"Python", "Install Python from https://www.python.org, then try again."},
}

// MissingRuntime says which program a command needs, when it is not on
// this computer, such as "Node.js". It is empty when the command is found.
func MissingRuntime(command string) string {
	if command == "" {
		return ""
	}
	if _, err := lookPath(command, searchPath()); err == nil {
		return ""
	}
	if h, ok := runtimeHelp[baseCommand(command)]; ok {
		return h.name
	}
	return command
}

func baseCommand(command string) string {
	base := strings.ToLower(filepath.Base(command))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "python3" {
		return "python"
	}
	return base
}

func lookPath(command, path string) (string, error) {
	if strings.ContainsRune(command, os.PathSeparator) || filepath.IsAbs(command) {
		if _, err := os.Stat(command); err != nil {
			return "", fmt.Errorf("%s was not found on this computer", command)
		}
		return command, nil
	}
	for _, dir := range filepath.SplitList(path) {
		for _, name := range candidates(command) {
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0) {
				return p, nil
			}
		}
	}
	if h, ok := runtimeHelp[baseCommand(command)]; ok {
		return "", fmt.Errorf("this tool source needs %s, which is not on this computer. %s", h.name, h.help)
	}
	return "", fmt.Errorf("%s was not found on this computer. Install it, or give its full path", command)
}

func candidates(command string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(command) != "" {
		return []string{command}
	}
	return []string{command + ".exe", command + ".cmd", command + ".bat", command}
}

// searchPath is PATH plus the folders package managers install to. A
// daemon started by launchd or systemd often has a short PATH that leaves
// out Homebrew, nvm, or uv, so npx and uvx would not be found.
func searchPath() string {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	home, _ := os.UserHomeDir()
	extra := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/snap/bin"}
	if home != "" {
		extra = append(extra,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".deno", "bin"),
		)
		// The newest nvm-installed Node.
		if versions, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); len(versions) > 0 {
			extra = append(extra, versions[len(versions)-1])
		}
	}
	if runtime.GOOS == "windows" {
		extra = []string{filepath.Join(os.Getenv("ProgramFiles"), "nodejs"), filepath.Join(os.Getenv("APPDATA"), "npm")}
		if home != "" {
			extra = append(extra, filepath.Join(home, ".local", "bin"))
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range append(dirs, extra...) {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// inherited are the variables a server gets from Yggdrasil's own
// environment, as the MCP SDKs do: what programs need to run, find their
// files, and reach the network. Anything else in the environment, such as
// another app's API key, stays out; a server gets only the values it was
// given.
var inherited = map[string]bool{
	"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TERM": true, "TMPDIR": true, "TZ": true,
	"LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "http_proxy": true, "https_proxy": true, "no_proxy": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true, "REQUESTS_CA_BUNDLE": true,
	"NVM_DIR": true, "VOLTA_HOME": true, "DOCKER_HOST": true,
	// Windows.
	"APPDATA": true, "LOCALAPPDATA": true, "HOMEDRIVE": true, "HOMEPATH": true, "USERPROFILE": true, "USERNAME": true,
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "TEMP": true, "TMP": true,
	"PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "PROGRAMDATA": true, "PROCESSOR_ARCHITECTURE": true,
}

func mergeEnv(base []string, path string, add map[string]string) []string {
	if p := add["PATH"]; p != "" {
		path = p + string(os.PathListSeparator) + path
	}
	out := make([]string, 0, len(add)+16)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		key := k
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(k)
		}
		if !inherited[key] && !strings.HasPrefix(key, "XDG_") {
			continue
		}
		if _, ok := add[k]; ok {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "PATH="+path)
	for k, v := range add {
		if k != "PATH" {
			out = append(out, k+"="+v)
		}
	}
	return out
}
