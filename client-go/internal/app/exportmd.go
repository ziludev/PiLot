package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"pitta/internal/i18n"
)

// exportMarkdown hands the chat on screen to a Markdown file. The transcript
// is already good at being read inside the window; the file is for the other
// reader — the pull request, the issue, the person who was not here.
func (a *App) exportMarkdown() {
	a.exportTo(a.exportDir())
}

// exportTo is the same verb with the destination named, so a test can watch the
// file land without writing to the reader's Downloads.
func (a *App) exportTo(dir string) {
	th, ok := a.threads[a.active]
	if !ok || len(th.entries) == 0 {
		a.post(event{kind: evNotice, notice: i18n.T("exportNothing")})
		return
	}
	title := a.chatTitle(a.active)
	now := time.Now()
	rows, _ := projectEntries(a.active, th.entries)
	body := chatMarkdown(title, rows, now)
	path, err := writeExport(dir, exportName(title, now), body)
	if err != nil {
		a.post(event{kind: evNotice, notice: i18n.TF("exportFailed", map[string]any{"err": err.Error()})})
		return
	}
	a.post(event{kind: evNotice, ok: true, notice: i18n.TF("exportDone", map[string]any{"path": abbreviateHome(path)})})
}

// exportDir is where the file lands: the place every other download goes, with
// the temporary directory as the fallback for a machine without one.
func (a *App) exportDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		downloads := filepath.Join(home, "Downloads")
		if info, err := os.Stat(downloads); err == nil && info.IsDir() {
			return downloads
		}
	}
	return os.TempDir()
}

// chatMarkdown is the whole conversation as one document: what was asked, what
// came back, and the steps in between folded away so the reading is the words.
// Tool output stays behind — it is the largest part and the least quoted.
func chatMarkdown(title string, rows []threadRow, at time.Time) string {
	var b strings.Builder
	if strings.TrimSpace(title) == "" {
		title = i18n.T("newChat")
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString("> " + i18n.TF("exportStamp", map[string]any{"at": at.Format("2006-01-02 15:04")}) + "\n")

	for _, row := range rows {
		text := strings.TrimSpace(row.text)
		switch row.kind {
		case rowUser:
			if text == "" {
				continue
			}
			b.WriteString("\n## " + i18n.T("exportYou") + "\n\n" + text + "\n")
			if n := len(row.images); n > 0 {
				b.WriteString("\n" + i18n.TF("exportImages", map[string]any{"n": n}) + "\n")
			}
		case rowAssistant:
			if text == "" {
				continue
			}
			b.WriteString("\n## " + "PiLot" + "\n\n" + text + "\n")
		case rowThinking:
			if text == "" {
				continue
			}
			b.WriteString("\n<details><summary>" + i18n.T("thinking") + "</summary>\n\n" + text + "\n\n</details>\n")
		case rowSteps:
			if len(row.steps) == 0 {
				continue
			}
			b.WriteString("\n<details><summary>")
			b.WriteString(i18n.TF("exportSteps", map[string]any{"n": len(row.steps)}))
			b.WriteString("</summary>\n\n")
			for i, step := range row.steps {
				b.WriteString(itoa(i+1) + ". **" + oneLine(step.label) + "**")
				if body := oneLine(step.body); body != "" {
					b.WriteString(" `" + strings.ReplaceAll(body, "`", "'") + "`")
				}
				b.WriteString("\n")
			}
			b.WriteString("\n</details>\n")
		}
	}
	return b.String()
}

// exportName is the file's name: the chat's title, kept to the characters a file
// name can hold, plus when it was taken — two exports of one chat must not
// overwrite each other.
func exportName(title string, at time.Time) string {
	var slug []rune
	for _, r := range []rune(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			slug = append(slug, r)
		case unicode.IsSpace(r) || strings.ContainsRune("-_.:/", r):
			slug = append(slug, '-')
		}
		if len(slug) >= 40 {
			break
		}
	}
	name := strings.Trim(string(slug), "-")
	if name == "" {
		name = "chat"
	}
	return "PiLot-" + name + "-" + at.Format("20060102-1504") + ".md"
}

// writeExport writes the file and hands back where it went, so the window can
// say the whole path rather than guess at one.
func writeExport(dir, name, body string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// abbreviateHome shortens a path under the home directory to ~/…, which is the
// form a person reads fastest.
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}
