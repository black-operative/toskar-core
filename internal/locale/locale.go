// Package locale looks up text in the shared translation catalog
// (i18n/locales, the same files the web UI and apps read), for text core
// writes itself in the App language: notifications sent by email, push,
// webhook, or to the desktop (multilingual spec §22).
package locale

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"sync"

	toskar "github.com/yeixio/toskar-core"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
	"golang.org/x/text/message"
)

// Source is the catalog's source language, and the last fallback.
const Source = "en"

type catalog struct {
	// text is language → "namespace:dotted.key" → text.
	text map[string]map[string]string
	// languages are the tags that have a catalog.
	languages []string
}

var (
	loadOnce sync.Once
	loaded   catalog
)

func load() catalog {
	loadOnce.Do(func() {
		loaded = catalog{text: map[string]map[string]string{}}
		entries, err := fs.ReadDir(toskar.Catalog, "i18n/locales")
		if err != nil {
			return
		}
		for _, dir := range entries {
			if !dir.IsDir() {
				continue
			}
			lang := dir.Name()
			files, _ := fs.Glob(toskar.Catalog, path.Join("i18n/locales", lang, "*.json"))
			table := map[string]string{}
			for _, file := range files {
				raw, err := fs.ReadFile(toskar.Catalog, file)
				if err != nil {
					continue
				}
				var tree map[string]any
				if json.Unmarshal(raw, &tree) != nil {
					continue
				}
				ns := strings.TrimSuffix(path.Base(file), ".json")
				flatten(ns+":", tree, table)
			}
			loaded.text[lang] = table
			loaded.languages = append(loaded.languages, lang)
		}
	})
	return loaded
}

func flatten(prefix string, tree map[string]any, out map[string]string) {
	for k, v := range tree {
		switch v := v.(type) {
		case string:
			out[prefix+k] = v
		case map[string]any:
			flatten(prefix+k+".", v, out)
		}
	}
}

// Languages are the tags that have a catalog, such as en, de, and zh-Hant.
func Languages() []string { return append([]string(nil), load().languages...) }

// Resolve picks the catalog for a language tag as the web UI does: exactly,
// by base language (de-AT → de), by script (zh-TW → zh-Hant), or by another
// region in the same script (pt-PT → pt-BR); otherwise English. "" and the
// pseudo-locales are English.
func Resolve(tag string) string {
	tag = strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")
	if tag == "" {
		return Source
	}
	langs := load().languages
	for _, l := range langs {
		if strings.EqualFold(l, tag) {
			return l
		}
	}
	parsed, err := language.Parse(tag)
	if err != nil {
		return Source
	}
	base, _ := parsed.Base()
	script, _ := parsed.Script()
	for _, l := range langs {
		if strings.EqualFold(l, base.String()) {
			return l
		}
	}
	for _, l := range langs {
		if strings.EqualFold(l, base.String()+"-"+script.String()) {
			return l
		}
	}
	for _, l := range langs {
		other, err := language.Parse(l)
		if err != nil {
			continue
		}
		otherBase, _ := other.Base()
		otherScript, _ := other.Script()
		if otherBase == base && otherScript == script && l != Source {
			return l
		}
	}
	return Source
}

// fallbacks is the order a key is looked up in: the language, its base
// language (es-MX → es), then English.
func fallbacks(lang string) []string {
	out := []string{lang}
	if base, _, ok := strings.Cut(lang, "-"); ok {
		out = append(out, base)
	}
	if lang != Source {
		out = append(out, Source)
	}
	return out
}

var placeholder = regexp.MustCompile(`\{\{\s*([^},\s]+)[^}]*\}\}`)

// T returns key's text in lang, such as T("de", "notifications:notices.modelReady", nil),
// with {{placeholders}} filled from params. A "count" param picks the
// plural form the language uses (key_one, key_other, …). Numbers are
// written in the language's format. A key the catalog lacks comes back as
// the key itself, so a mistake shows instead of hiding.
func T(lang, key string, params map[string]any) string {
	c := load()
	lang = Resolve(lang)
	tag := language.Make(lang)
	for _, l := range fallbacks(lang) {
		table := c.text[l]
		if table == nil {
			continue
		}
		text, ok := "", false
		if count, isCount := number(params["count"]); isCount {
			// The plural form is the language's own, even when its text
			// falls back to English.
			text, ok = table[key+"_"+pluralForm(tag, count)]
			if !ok {
				text, ok = table[key+"_other"]
			}
		}
		if !ok {
			text, ok = table[key]
		}
		if ok {
			return fill(text, tag, params)
		}
	}
	return key
}

func fill(text string, tag language.Tag, params map[string]any) string {
	printer := message.NewPrinter(tag)
	return placeholder.ReplaceAllStringFunc(text, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		v, ok := params[name]
		if !ok {
			return m
		}
		if n, isNumber := number(v); isNumber && name != "port" && name != "year" {
			return printer.Sprint(n)
		}
		return fmt.Sprint(v)
	})
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func pluralForm(tag language.Tag, n float64) string {
	if n != float64(int64(n)) {
		return "other"
	}
	i := int64(n)
	if i < 0 {
		i = -i
	}
	switch plural.Cardinal.MatchPlural(tag, int(i), 0, 0, 0, 0) {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	}
	return "other"
}

// LanguageName is a language's name written in another language, such as
// "Spanisch" for es in de, or the tag when it has none.
func LanguageName(tag, in string) string {
	t, err := language.Parse(tag)
	if err != nil {
		return tag
	}
	if namer := display.Tags(language.Make(Resolve(in))); namer != nil {
		if name := namer.Name(t); name != "" {
			return name
		}
	}
	if name := display.English.Tags().Name(t); name != "" {
		return name
	}
	return tag
}
