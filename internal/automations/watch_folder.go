package automations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// A folder trigger watches the files in a folder, or one file, in the home
// folder (#204). A check compares each file's size and modification time
// with the last check's, so it reads no file that didn't change. The run
// can't open files outside Toskar's workspace, so the check gives it the
// changed text files' contents, a little of each.
const (
	maxWatchedFiles = 5000
	maxWatchDepth   = 8
	// excerptBytes is how much of each changed text file the run is
	// given, and excerptTotal how much in all.
	excerptBytes = 4000
	excerptTotal = 20000
)

type folderState struct {
	Files map[string]string `json:"files"`
}

// CheckFolder compares a folder or file with the last check.
func CheckFolder(path string, state []byte) (Found, []byte, error) {
	root, err := SaveFolderPath(path)
	if err != nil {
		return Found{}, nil, err
	}
	if root == "" {
		return Found{}, nil, fmt.Errorf("no folder to watch")
	}
	files, err := scanFolder(root)
	if err != nil {
		return Found{}, nil, err
	}
	raw, err := json.Marshal(folderState{Files: files})
	if err != nil {
		return Found{}, nil, err
	}
	var prev folderState
	if len(state) == 0 || json.Unmarshal(state, &prev) != nil || prev.Files == nil {
		return Found{}, raw, nil
	}
	var added, changed, removed []string
	for name, stamp := range files {
		old, ok := prev.Files[name]
		switch {
		case !ok:
			added = append(added, name)
		case old != stamp:
			changed = append(changed, name)
		}
	}
	for name := range prev.Files {
		if _, ok := files[name]; !ok {
			removed = append(removed, name)
		}
	}
	if len(added)+len(changed)+len(removed) == 0 {
		return Found{}, raw, nil
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	var b strings.Builder
	fmt.Fprintf(&b, "In %s:\n", root)
	n := 0
	list := func(label string, names []string) {
		for _, name := range names {
			if n == maxSummaryLines {
				fmt.Fprintf(&b, "(and more)\n")
			}
			if n < maxSummaryLines {
				fmt.Fprintf(&b, "%s: %s\n", label, name)
			}
			n++
		}
	}
	list("Added", added)
	list("Changed", changed)
	list("Removed", removed)
	if excerpts := textExcerpts(root, append(added, changed...)); excerpts != "" {
		b.WriteString("\nContents of the new and changed text files:\n" + excerpts)
	}
	return Found{Changed: true, Summary: strings.TrimSpace(b.String())}, raw, nil
}

// scanFolder stamps each file under root, or root itself when it's a
// file, skipping hidden files and folders.
func scanFolder(root string) (map[string]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	stamp := func(fi fs.FileInfo) string { return fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano()) }
	if !info.IsDir() {
		files[filepath.Base(root)] = stamp(info)
		return files, nil
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A folder it can't read is left out, not a failed check.
			if d != nil && d.IsDir() && path != root {
				return fs.SkipDir
			}
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.Count(rel, string(filepath.Separator)) >= maxWatchDepth {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if len(files) >= maxWatchedFiles {
			return fs.SkipAll
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		files[filepath.ToSlash(rel)] = stamp(fi)
		return nil
	})
	return files, err
}

// textExcerpts is the start of each text file among names, as long as the
// total stays small.
func textExcerpts(root string, names []string) string {
	info, err := os.Stat(root)
	if err != nil {
		return ""
	}
	var b strings.Builder
	total := 0
	for _, name := range names {
		if total >= excerptTotal {
			break
		}
		path := root
		if info.IsDir() {
			path = filepath.Join(root, filepath.FromSlash(name))
		}
		text, ok := readText(path, min(excerptBytes, excerptTotal-total))
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", name, text)
		total += len(text)
	}
	return b.String()
}

// readText reads up to limit bytes of a file that looks like text.
func readText(path string, limit int) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, limit+1)
	n, _ := io.ReadFull(f, buf)
	data := buf[:n]
	if bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	cut := len(data) > limit
	if cut {
		data = data[:limit]
		// Not in the middle of a character.
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) {
		return "", false
	}
	text := string(data)
	if cut {
		text += "\n…"
	}
	return text, true
}
