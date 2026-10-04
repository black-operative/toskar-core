package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Element is something on the page the assistant can act on, by its ref.
type Element struct {
	Ref   int    `json:"ref"`
	Role  string `json:"role"`
	Label string `json:"label"`
	Type  string `json:"type,omitempty"`
	Href  string `json:"href,omitempty"`
	Value string `json:"value,omitempty"`
	// Sensitive marks a password, payment, or one-time-code field, which
	// the assistant never types into.
	Sensitive bool `json:"sensitive,omitempty"`
}

// Snapshot is what a page shows.
type Snapshot struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Text     string    `json:"text"`
	Elements []Element `json:"elements"`
}

// snapshotJS numbers the visible things to act on and reads the page's
// text. Refs are renewed on every snapshot.
const snapshotJS = `(() => {
  document.querySelectorAll('[data-ygg-ref]').forEach(e => e.removeAttribute('data-ygg-ref'));
  const sel = 'a[href],button,input:not([type=hidden]),select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=option],[contenteditable=true],[onclick]';
  const seen = new Set(); const out = []; let n = 0;
  const sensitive = e => {
    const t = (e.getAttribute('type') || '').toLowerCase();
    const ac = (e.getAttribute('autocomplete') || '').toLowerCase();
    const id = ((e.name || '') + ' ' + (e.id || '') + ' ' + (e.getAttribute('aria-label') || '') + ' ' + (e.placeholder || '')).toLowerCase();
    return t === 'password' || /cc-|one-time-code|current-password|new-password/.test(ac) ||
      /pass(word|code)?|card ?(number|no)|cvv|cvc|security code|iban|routing|ssn|social security|otp|verification code/.test(id);
  };
  for (const e of document.querySelectorAll(sel)) {
    if (seen.has(e) || n >= 80) continue; seen.add(e);
    const r = e.getBoundingClientRect(); const st = getComputedStyle(e);
    if (r.width < 2 || r.height < 2 || st.visibility === 'hidden' || st.display === 'none' || e.disabled) continue;
    n++; e.setAttribute('data-ygg-ref', String(n));
    const tag = e.tagName.toLowerCase();
    const role = e.getAttribute('role') || (tag === 'a' ? 'link' : tag === 'input' ? 'input' : tag);
    let label = (e.getAttribute('aria-label') || e.innerText || e.value || e.placeholder || e.name || e.title || e.alt || '').trim().replace(/\s+/g, ' ');
    if (!label && tag === 'a') { const img = e.querySelector('img[alt]'); if (img) label = img.alt; }
    const el = { ref: n, role, label: label.slice(0, 80) };
    if (tag === 'input') { el.type = (e.getAttribute('type') || 'text').toLowerCase(); if (!sensitive(e) && e.value && el.type !== 'submit') el.value = String(e.value).slice(0, 60); }
    if (tag === 'select' && e.selectedOptions.length) el.value = e.selectedOptions[0].text.slice(0, 60);
    if (tag === 'a') el.href = e.href;
    if ((tag === 'input' || tag === 'textarea' || e.isContentEditable) && sensitive(e)) el.sensitive = true;
    out.push(el);
  }
  const main = document.querySelector('main, article, [role=main]') || document.body;
  return { url: location.href, title: document.title, text: (main ? main.innerText : '').trim(), elements: out };
})()`

const maxSnapshotText = 12000

func (m *Manager) snapshot(ctx context.Context) (Snapshot, error) {
	var s Snapshot
	if err := chromedp.Run(ctx, chromedp.Evaluate(snapshotJS, &s)); err != nil {
		return Snapshot{}, fmt.Errorf("the page could not be read: %w", err)
	}
	if r := []rune(s.Text); len(r) > maxSnapshotText {
		s.Text = string(r[:maxSnapshotText]) + "…"
	}
	return s, nil
}

// settle waits for the page to finish what an action started.
func settle(ctx context.Context) {
	wait, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_ = chromedp.Run(wait, chromedp.WaitReady("body", chromedp.ByQuery))
	select {
	case <-time.After(700 * time.Millisecond):
	case <-ctx.Done():
	}
}

func refSelector(ref int) string { return fmt.Sprintf(`[data-ygg-ref="%d"]`, ref) }

// run does an action in a chat's browser, one at a time, within a time
// limit.
func (m *Manager) run(ctx context.Context, key string, limit time.Duration, fn func(ctx context.Context) error) error {
	s, err := m.get(key)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	actx, cancel := context.WithTimeout(s.ctx, limit)
	defer cancel()
	// Stop with the turn.
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	err = fn(actx)
	s.lastUsed = time.Now()
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("the page took too long")
	}
	return err
}

// Open goes to an address.
func (m *Manager) Open(ctx context.Context, key, raw string) (Snapshot, error) {
	if err := m.Guard.CheckURL(ctx, raw); err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	err := m.run(ctx, key, 45*time.Second, func(ctx context.Context) error {
		m.opened(ctx, raw)
		if err := chromedp.Run(ctx, chromedp.Navigate(raw)); err != nil {
			return fmt.Errorf("the page could not be opened: %w", err)
		}
		settle(ctx)
		var err error
		snap, err = m.snapshot(ctx)
		return err
	})
	return snap, err
}

func (m *Manager) opened(ctx context.Context, raw string) {
	if m.Opened == nil {
		return
	}
	if u, err := url.Parse(raw); err == nil {
		m.Opened(ctx, u.Hostname(), raw)
	}
}

// current is the page's address, or "".
func current(ctx context.Context) string {
	var loc string
	_ = chromedp.Run(ctx, chromedp.Location(&loc))
	return loc
}

// element reads one element's details by ref.
func element(ctx context.Context, ref int) (Element, error) {
	var el Element
	js := fmt.Sprintf(`(() => { const e = document.querySelector('%s'); if (!e) return null; e.scrollIntoView({block: 'center'});
	  return { ref: %d, role: e.tagName.toLowerCase(), label: (e.innerText || e.value || '').trim().slice(0, 80), type: (e.getAttribute('type') || '').toLowerCase(), href: e.href || '' }; })()`, refSelector(ref), ref)
	var found *Element
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &found)); err != nil {
		return el, err
	}
	if found == nil {
		return el, fmt.Errorf("nothing with ref %d is on the page now; open or read the page again for current refs", ref)
	}
	return *found, nil
}

// Click clicks an element and returns the page after.
func (m *Manager) Click(ctx context.Context, key string, ref int) (Snapshot, error) {
	var snap Snapshot
	err := m.run(ctx, key, 45*time.Second, func(ctx context.Context) error {
		if _, err := element(ctx, ref); err != nil {
			return err
		}
		before := current(ctx)
		if err := chromedp.Run(ctx, chromedp.Click(refSelector(ref), chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
			return fmt.Errorf("the click did not work: %w", err)
		}
		settle(ctx)
		if after := current(ctx); after != before {
			m.opened(ctx, after)
		}
		var err error
		snap, err = m.snapshot(ctx)
		return err
	})
	return snap, err
}

// ErrSensitive is typing into a password, payment, or one-time-code field.
var ErrSensitive = errors.New("that field asks for a password, payment details, or a code; Toskar never types those. Ask the user to enter it themselves")

// Type types into a field; submit presses Enter after.
func (m *Manager) Type(ctx context.Context, key string, ref int, text string, submit bool) (Snapshot, error) {
	var snap Snapshot
	err := m.run(ctx, key, 45*time.Second, func(ctx context.Context) error {
		var sensitive bool
		js := fmt.Sprintf(`(() => { const e = document.querySelector('%s'); if (!e) return null;
		  const t = (e.getAttribute('type') || '').toLowerCase(); const ac = (e.getAttribute('autocomplete') || '').toLowerCase();
		  const id = ((e.name || '') + ' ' + (e.id || '') + ' ' + (e.getAttribute('aria-label') || '') + ' ' + (e.placeholder || '')).toLowerCase();
		  return t === 'password' || /cc-|one-time-code|current-password|new-password/.test(ac) || /pass(word|code)?|card ?(number|no)|cvv|cvc|security code|iban|routing|ssn|social security|otp|verification code/.test(id); })()`, refSelector(ref))
		var res *bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &res)); err != nil {
			return err
		}
		if res == nil {
			return fmt.Errorf("nothing with ref %d is on the page now; open or read the page again for current refs", ref)
		}
		sensitive = *res
		if sensitive {
			return ErrSensitive
		}
		sel := refSelector(ref)
		before := current(ctx)
		actions := []chromedp.Action{chromedp.Focus(sel, chromedp.ByQuery),
			chromedp.Evaluate(fmt.Sprintf(`(() => { const e = document.querySelector('%s'); if (e && 'value' in e) { e.value = ''; e.dispatchEvent(new Event('input', {bubbles: true})); } })()`, sel), nil),
			chromedp.SendKeys(sel, text, chromedp.ByQuery)}
		if submit {
			actions = append(actions, chromedp.SendKeys(sel, kb.Enter, chromedp.ByQuery))
		}
		if err := chromedp.Run(ctx, actions...); err != nil {
			return fmt.Errorf("typing did not work: %w", err)
		}
		settle(ctx)
		if after := current(ctx); after != before {
			m.opened(ctx, after)
		}
		var err error
		snap, err = m.snapshot(ctx)
		return err
	})
	return snap, err
}

// Extract reads more of the page: its whole text, its links, or its tables.
func (m *Manager) Extract(ctx context.Context, key, what string) (map[string]any, error) {
	var out map[string]any
	err := m.run(ctx, key, 30*time.Second, func(ctx context.Context) error {
		js := `(() => ({ url: location.href, title: document.title, text: document.body ? document.body.innerText.trim() : '' }))()`
		switch what {
		case "links":
			js = `(() => ({ url: location.href, links: [...document.querySelectorAll('a[href]')].slice(0, 200).map(a => ({ text: (a.innerText || a.title || '').trim().replace(/\s+/g, ' ').slice(0, 100), href: a.href })) }))()`
		case "tables":
			js = `(() => ({ url: location.href, tables: [...document.querySelectorAll('table')].slice(0, 10).map(t => [...t.rows].slice(0, 50).map(r => [...r.cells].map(c => c.innerText.trim().replace(/\s+/g, ' ').slice(0, 120)))) }))()`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &out)); err != nil {
			return fmt.Errorf("the page could not be read: %w", err)
		}
		if text, ok := out["text"].(string); ok {
			if r := []rune(text); len(r) > 40000 {
				out["text"], out["truncated"] = string(r[:40000]), true
			}
		}
		return nil
	})
	if err == nil && out == nil {
		return nil, errors.New("nothing is open; use browser.open first")
	}
	return out, err
}

// Screenshot captures what the page shows.
func (m *Manager) Screenshot(ctx context.Context, key string) ([]byte, string, error) {
	var buf []byte
	var loc string
	err := m.run(ctx, key, 30*time.Second, func(ctx context.Context) error {
		loc = current(ctx)
		return chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf))
	})
	return buf, loc, err
}

// Link returns the address of a link by ref, and the page's cookies for
// it, so a download keeps the session.
func (m *Manager) Link(ctx context.Context, key string, ref int) (string, error) {
	var href string
	err := m.run(ctx, key, 15*time.Second, func(ctx context.Context) error {
		el, err := element(ctx, ref)
		if err != nil {
			return err
		}
		if el.Href == "" {
			return fmt.Errorf("ref %d is not a link", ref)
		}
		href = el.Href
		return nil
	})
	return href, err
}
