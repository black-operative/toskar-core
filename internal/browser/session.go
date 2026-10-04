package browser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Limits.
const (
	maxSessions = 3
	idleAfter   = 10 * time.Minute
)

// startLimit is how long a browser may take to start. Tests raise it: on a
// busy machine a first start can be slow.
var startLimit = 30 * time.Second

// Manager runs one isolated browser per chat.
type Manager struct {
	// ExecPath is the browser program; empty finds one.
	ExecPath string
	// WorkDir holds each session's temporary profile.
	WorkDir string
	Guard   *Guard
	// Opened is called for each page the browser is sent to, for What
	// left this computer.
	Opened func(ctx context.Context, host, url string)
	// Sandboxed refuses: the Mac App Store build cannot start another
	// program.
	Sandboxed bool

	mu       sync.Mutex
	sessions map[string]*session
	reaping  bool
	// stopReap ends the reaper when the last session closes, rather than
	// at its next minute's check (#231).
	stopReap chan struct{}
	cleaned  bool
}

type session struct {
	ctx      context.Context
	cancel   context.CancelFunc
	dir      string
	mu       sync.Mutex // one action at a time
	lastUsed time.Time
}

// candidates are where Chromium-based browsers usually are.
func candidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	case "windows":
		var out []string
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if base == "" {
				continue
			}
			out = append(out, filepath.Join(base, `Google\Chrome\Application\chrome.exe`), filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(base, `BraveSoftware\Brave-Browser\Application\brave.exe`), filepath.Join(base, `Chromium\Application\chrome.exe`))
		}
		return out
	default:
		var out []string
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
			if p, err := exec.LookPath(name); err == nil {
				out = append(out, p)
			}
		}
		return out
	}
}

// program returns the browser to run, or "".
func (m *Manager) program() string {
	if m.ExecPath != "" {
		return m.ExecPath
	}
	for _, p := range candidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Available reports whether a browser can run here, and why not.
func (m *Manager) Available() (bool, string) {
	if m.Sandboxed {
		return false, "this copy of Toskar runs in the macOS App Sandbox, which cannot start a browser"
	}
	if m.program() == "" {
		return false, "no browser was found; install Google Chrome, Microsoft Edge, Chromium, or Brave to let Toskar use web pages"
	}
	return true, ""
}

// get returns the chat's browser, starting one when needed.
func (m *Manager) get(key string) (*session, error) {
	if ok, why := m.Available(); !ok {
		return nil, errors.New(why)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = map[string]*session{}
	}
	if s, ok := m.sessions[key]; ok && s.ctx.Err() == nil {
		s.lastUsed = time.Now()
		return s, nil
	}
	if len(m.sessions) >= maxSessions {
		// Make room: close the one used longest ago.
		var oldest string
		for k, s := range m.sessions {
			if oldest == "" || s.lastUsed.Before(m.sessions[oldest].lastUsed) {
				oldest = k
			}
		}
		m.closeLocked(oldest)
	}
	if err := os.MkdirAll(m.WorkDir, 0o700); err != nil {
		return nil, err
	}
	if !m.cleaned {
		// Profiles left by a browser that did not close, such as after a
		// crash, are removed before the first new one.
		m.cleaned = true
		if old, _ := filepath.Glob(filepath.Join(m.WorkDir, "profile-*")); len(old) > 0 {
			for _, p := range old {
				_ = os.RemoveAll(p)
			}
		}
	}
	dir, err := os.MkdirTemp(m.WorkDir, "profile-")
	if err != nil {
		return nil, err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(m.program()),
		chromedp.UserDataDir(dir),
		chromedp.WindowSize(1280, 900),
		// chromedp gives up waiting for the browser's address after 20
		// seconds unless told otherwise, before the timer below.
		chromedp.WSURLReadTimeout(startLimit),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("disable-features", "PasswordManager,AutofillServerCommunication,MediaRouter"),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	s := &session{ctx: ctx, dir: dir, lastUsed: time.Now()}
	s.cancel = func() {
		// Cancel closes the browser and waits for it, so it writes nothing
		// to the profile after it is removed.
		_ = chromedp.Cancel(ctx)
		cancel()
		allocCancel()
		if os.RemoveAll(dir) != nil {
			time.Sleep(500 * time.Millisecond)
			_ = os.RemoveAll(dir)
		}
	}
	m.listen(ctx)
	// The first Run starts the browser and ties it to ctx, so it runs on
	// ctx itself; a timer stops a browser that does not start.
	slow := time.AfterFunc(startLimit, s.cancel)
	defer slow.Stop()
	if err := chromedp.Run(ctx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}}),
		// The browser saves nothing itself; browser.download brings a file
		// into the chat.
		cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorDeny),
	); err != nil {
		s.cancel()
		return nil, errors.New("the browser could not be started: " + err.Error())
	}
	m.sessions[key] = s
	if !m.reaping {
		m.reaping = true
		m.stopReap = make(chan struct{})
		go m.reap(m.stopReap)
	}
	return s, nil
}

// listen checks every request the page makes, including redirects and
// resources, and stops those for this computer or the local network.
func (m *Manager) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			c := chromedp.FromContext(ctx)
			if c == nil || c.Target == nil {
				return
			}
			ectx := cdp.WithExecutor(ctx, c.Target)
			if allowedRequest(ctx, m.Guard, e.Request.URL) {
				_ = fetch.ContinueRequest(e.RequestID).Do(ectx)
				return
			}
			_ = fetch.FailRequest(e.RequestID, network.ErrorReasonAccessDenied).Do(ectx)
		}()
	})
}

// allowedRequest lets through page-internal addresses (data:, blob:) and
// http(s) to public hosts.
func allowedRequest(ctx context.Context, g *Guard, raw string) bool {
	switch {
	case len(raw) >= 5 && (raw[:5] == "data:" || raw[:5] == "blob:"):
		return true
	case len(raw) >= 4 && raw[:4] == "http":
		return g.CheckURL(ctx, raw) == nil
	}
	return false
}

// Close ends a chat's browser.
func (m *Manager) Close(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[key]
	m.closeLocked(key)
	m.stopReapIfIdleLocked()
	return ok
}

func (m *Manager) closeLocked(key string) {
	if s, ok := m.sessions[key]; ok {
		s.cancel()
		delete(m.sessions, key)
	}
}

// CloseAll ends every browser, such as when the daemon stops.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.sessions {
		m.closeLocked(k)
	}
	m.stopReapIfIdleLocked()
}

// stopReapIfIdleLocked ends the reaper once no browser is open.
func (m *Manager) stopReapIfIdleLocked() {
	if m.reaping && len(m.sessions) == 0 {
		close(m.stopReap)
		m.reaping = false
	}
}

// reap closes browsers left idle, checking each minute, until stop closes
// or no browser is left.
func (m *Manager) reap(stop <-chan struct{}) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		m.mu.Lock()
		for k, s := range m.sessions {
			if time.Since(s.lastUsed) > idleAfter || s.ctx.Err() != nil {
				m.closeLocked(k)
			}
		}
		empty := len(m.sessions) == 0
		if empty && m.reaping {
			close(m.stopReap)
			m.reaping = false
		}
		m.mu.Unlock()
		if empty {
			return
		}
	}
}
