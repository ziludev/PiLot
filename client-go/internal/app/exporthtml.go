package app

// The chat as one HTML file.
//
// Markdown is for the pull request; this is for the person who is not going to
// read Markdown. One file with its styles inlined, so it opens by double click,
// reads in either appearance, and can be mailed.

import (
	"html"
	"strings"
	"time"

	"pitta/internal/i18n"
)

// exportHTML writes the chat on screen as a stand-alone page.
func (a *App) exportHTML() {
	a.exportHTMLTo(a.exportDir())
}

// exportHTMLTo is exportHTML with the destination named, so a test can watch the
// file land without writing to the reader's Downloads.
func (a *App) exportHTMLTo(dir string) {
	th, ok := a.threads[a.active]
	if !ok || len(th.entries) == 0 {
		a.post(event{kind: evNotice, notice: i18n.T("exportNothing")})
		return
	}
	title := a.chatTitle(a.active)
	now := time.Now()
	rows, _ := projectEntries(a.active, th.entries)
	path, err := writeExport(dir, exportHTMLName(title, now), chatHTML(title, rows, now))
	if err != nil {
		a.post(event{kind: evNotice, notice: i18n.TF("exportFailed", map[string]any{"err": err.Error()})})
		return
	}
	a.post(event{kind: evNotice, ok: true, notice: i18n.TF("exportDone", map[string]any{"path": abbreviateHome(path)})})
}

// exportHTMLName is the Markdown name with the other extension: one chat, two
// files, taken at different moments.
func exportHTMLName(title string, at time.Time) string {
	return strings.TrimSuffix(exportName(title, at), ".md") + ".html"
}

// chatHTML is the conversation as a page: what was asked and what came back as
// prose, the steps folded under each answer, the tool output left behind.
func chatHTML(title string, rows []threadRow, at time.Time) string {
	if strings.TrimSpace(title) == "" {
		title = i18n.T("newChat")
	}
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"zh\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>" + html.EscapeString(title) + "</title>\n<style>\n" + exportCSS + "</style>\n</head>\n<body>\n<main>\n")
	b.WriteString("<h1>" + html.EscapeString(title) + "</h1>\n")
	b.WriteString("<p class=\"stamp\">" + html.EscapeString(i18n.TF("exportStamp", map[string]any{"at": at.Format("2006-01-02 15:04")})) + "</p>\n")

	for _, row := range rows {
		text := strings.TrimSpace(row.text)
		switch row.kind {
		case rowUser:
			if text == "" && len(row.images) == 0 {
				continue
			}
			b.WriteString("<section class=\"turn you\">\n<h2>" + html.EscapeString(i18n.T("exportYou")) + "</h2>\n")
			b.WriteString(paragraphs(text))
			if n := len(row.images); n > 0 {
				b.WriteString("<p class=\"meta\">" + html.EscapeString(i18n.TF("exportImages", map[string]any{"n": n})) + "</p>\n")
			}
			b.WriteString("</section>\n")
		case rowAssistant:
			if text == "" {
				continue
			}
			b.WriteString("<section class=\"turn pil\">\n<h2>PiLot</h2>\n")
			b.WriteString(paragraphs(text))
			b.WriteString("</section>\n")
		case rowThinking:
			if text == "" {
				continue
			}
			b.WriteString("<details>\n<summary>" + html.EscapeString(i18n.T("thinking")) + "</summary>\n")
			b.WriteString(paragraphs(text))
			b.WriteString("</details>\n")
		case rowSteps:
			if len(row.steps) == 0 {
				continue
			}
			b.WriteString("<details>\n<summary>" +
				html.EscapeString(i18n.TF("exportSteps", map[string]any{"n": len(row.steps)})) +
				"</summary>\n<ol>\n")
			for _, step := range row.steps {
				b.WriteString("<li><b>" + html.EscapeString(oneLine(step.label)) + "</b>")
				if body := oneLine(step.body); body != "" {
					b.WriteString(" <code>" + html.EscapeString(body) + "</code>")
				}
				b.WriteString("</li>\n")
			}
			b.WriteString("</ol>\n</details>\n")
		}
	}
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

// paragraphs keeps the shape of what was written: a blank line starts a new
// paragraph, and line breaks inside one are kept as written.
func paragraphs(text string) string {
	var b strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		b.WriteString("<p>" + html.EscapeString(strings.TrimSpace(para)) + "</p>\n")
	}
	return b.String()
}

// exportCSS is the page's whole appearance: system type, a readable measure, and
// the reader's own light or dark preference.
const exportCSS = `:root{color-scheme:light dark}
body{margin:0;padding:48px 24px;background:#fbfbfa;color:#1c1c1a;
font:16px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif}
main{max-width:720px;margin:0 auto}
h1{font-size:26px;line-height:1.3;margin:0 0 4px}
h2{font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:#8a8a84;margin:0 0 6px}
.stamp{color:#8a8a84;font-size:13px;margin:0 0 32px}
.turn{margin:0 0 28px;padding-left:14px;border-left:2px solid #e4e4de}
.turn.you{border-left-color:#c9c9c2}
.turn.pil{border-left-color:#7c9cff}
p{margin:0 0 12px;white-space:pre-wrap}
.meta{color:#8a8a84;font-size:13px}
details{margin:0 0 28px;color:#5c5c56;font-size:14px}
summary{cursor:pointer;color:#8a8a84}
ol{margin:8px 0 0;padding-left:22px}
code{font:12.5px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;background:#f0f0ec;
padding:1px 5px;border-radius:4px;word-break:break-all}
@media (prefers-color-scheme:dark){
body{background:#17171a;color:#e8e8e4}
h1{color:#f4f4f0}
.turn{border-left-color:#2c2c31}
.turn.you{border-left-color:#3a3a41}
code{background:#24242a}
}`
