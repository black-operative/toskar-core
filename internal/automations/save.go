package automations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// An automation can also save each result as a Markdown file in a folder
// (#204), such as ~/Documents/Toskar/News, where other apps and backups
// find it. The folder must be in the person's home folder.

// homeDir is the home folder; tests point it elsewhere.
var homeDir = os.UserHomeDir

// SaveFolderPath is a save folder as an absolute path: ~ is the home
// folder. It refuses a relative path, or one outside the home folder.
func SaveFolderPath(folder string) (string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return "", nil
	}
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("save_folder: no home folder: %w", err)
	}
	if folder == "~" || strings.HasPrefix(folder, "~/") || strings.HasPrefix(folder, `~\`) {
		folder = filepath.Join(home, folder[1:])
	}
	if !filepath.IsAbs(folder) {
		return "", errors.New("save_folder must be a full path, such as ~/Documents/Toskar")
	}
	folder = filepath.Clean(folder)
	rel, err := filepath.Rel(filepath.Clean(home), folder)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("save_folder must be in your home folder")
	}
	return folder, nil
}

// SaveResult writes a result to a new Markdown file in the automation's
// folder, named for when it ran and the automation, and returns its path.
func SaveResult(automation Automation, at time.Time, text string) (string, error) {
	folder, err := SaveFolderPath(automation.SaveFolder)
	if err != nil || folder == "" {
		return "", err
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	loc, err := time.LoadLocation(automation.Schedule.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	local := at.In(loc)
	base := local.Format("2006-01-02 15.04") + " " + fileSafe(automation.Name)
	body := "# " + automation.Name + "\n\n" + local.Format("2006-01-02 15:04 MST") + "\n\n" + strings.TrimSpace(text) + "\n"
	// A second result in the same minute, such as Run now, gets a number.
	for n := 1; n < 100; n++ {
		name := base + ".md"
		if n > 1 {
			name = fmt.Sprintf("%s (%d).md", base, n)
		}
		path := filepath.Join(folder, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.WriteString(body); err != nil {
			_ = f.Close()
			return "", err
		}
		return path, f.Close()
	}
	return "", errors.New("too many results saved this minute")
}

// fileSafe is a name that works as a file name everywhere.
func fileSafe(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case strings.ContainsRune(`<>:"/\|?*`, r) || unicode.IsControl(r):
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), ". ")
	if r := []rune(out); len(r) > 80 {
		out = strings.TrimSpace(string(r[:80]))
	}
	if out == "" {
		return "Automation"
	}
	return out
}
