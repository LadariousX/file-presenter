// userguide turns USER_GUIDE.md into a PDF with a headless Chrome/Chromium/Edge (used by build.sh).
//
//	go run ./tools/userguide USER_GUIDE.md "dist/File Presenter User Guide.pdf"
//
// It understands the Markdown the guide uses: headings, paragraphs, lists (one level of nesting),
// tables, **bold**, `code` and [links](url).
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const style = `
@page{size:letter;margin:.6in}
body{font-family:-apple-system,"Segoe UI",Helvetica,Arial,sans-serif;font-size:11.5pt;line-height:1.5;color:#222}
header{display:flex;align-items:center;gap:14px;margin-bottom:6px}
header img{width:56px;height:56px}
h1{font-size:24pt;margin:0}
h2{font-size:14pt;color:#2b8a68;border-bottom:1px solid #ddd;padding-bottom:3px;margin:22px 0 8px}
table{width:100%;border-collapse:collapse;table-layout:fixed}
th,td{width:50%;vertical-align:top;text-align:left;padding:8px 10px;border:1px solid #ddd}
th{background:#f2f7f5;font-size:12.5pt}
ul,ol{padding-left:22px;margin:6px 0}
li{margin:3px 0}
a{color:#2b8a68}
code{font-family:Menlo,Consolas,monospace;font-size:10pt}
`

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: userguide GUIDE.md OUT.pdf")
	}
	md, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	body := toHTML(string(md))
	// Put the app icon next to the title.
	if png, err := os.ReadFile("assets/icon.png"); err == nil {
		img := `<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(png) + `">`
		body = regexp.MustCompile(`^<h1>(.*?)</h1>`).ReplaceAllString(body, "<header>"+img+"<h1>$1</h1></header>")
	}
	page := "<!doctype html><meta charset=utf-8><style>" + style + "</style>" + body

	tmp, err := os.MkdirTemp("", "userguide")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	in := filepath.Join(tmp, "guide.html")
	if err := os.WriteFile(in, []byte(page), 0o644); err != nil {
		log.Fatal(err)
	}
	out, _ := filepath.Abs(os.Args[2])
	browser := findBrowser()
	if browser == "" {
		log.Fatal("no Chrome, Chromium or Edge found to print the PDF with")
	}
	os.Remove(out)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, browser, "--headless", "--disable-gpu", "--no-pdf-header-footer",
		"--print-to-pdf-no-header", "--user-data-dir="+filepath.Join(tmp, "profile"),
		"--print-to-pdf="+out, "file://"+in)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	// Some browsers stay running after printing, so stop it once the PDF stops growing.
	var last int64 = -1
	for {
		select {
		case <-exited:
			if _, err := os.Stat(out); err != nil {
				log.Fatal("the browser didn't write the PDF")
			}
			return
		case <-ctx.Done():
			log.Fatal("timed out printing the PDF")
		case <-time.After(500 * time.Millisecond):
		}
		if st, err := os.Stat(out); err == nil && st.Size() > 0 {
			if st.Size() == last {
				cmd.Process.Kill()
				<-exited
				return
			}
			last = st.Size()
		}
	}
}

func findBrowser() string {
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chrome", "msedge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

var (
	heading = regexp.MustCompile(`^(#{1,6}) (.*)`)
	bullet  = regexp.MustCompile(`^( *)- (.*)`)
	ordered = regexp.MustCompile(`^( *)\d+\. (.*)`)
	code    = regexp.MustCompile("`([^`]+)`")
	bold    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	link    = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

func inline(s string) string {
	s = html.EscapeString(s)
	s = code.ReplaceAllString(s, "<code>$1</code>")
	s = bold.ReplaceAllString(s, "<b>$1</b>")
	return link.ReplaceAllString(s, `<a href="$2">$1</a>`)
}

func cells(row string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func toHTML(md string) string {
	var out strings.Builder
	var para []string
	var lists []string // open list tags, outermost first
	flush := func() {
		if len(para) > 0 {
			out.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	closeLists := func(depth int) {
		for len(lists) > depth {
			out.WriteString("</li></" + lists[len(lists)-1] + ">\n")
			lists = lists[:len(lists)-1]
		}
	}
	item := func(tag string, indent int, text string) {
		flush()
		depth := indent/2 + 1
		closeLists(depth)
		if len(lists) == depth && lists[depth-1] != tag {
			closeLists(depth - 1)
		}
		if len(lists) < depth {
			out.WriteString("<" + tag + ">")
			lists = append(lists, tag)
		} else {
			out.WriteString("</li>")
		}
		out.WriteString("<li>" + inline(text))
	}

	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], " \r")
		if m := bullet.FindStringSubmatch(l); m != nil {
			item("ul", len(m[1]), m[2])
			continue
		}
		if m := ordered.FindStringSubmatch(l); m != nil {
			item("ol", len(m[1]), m[2])
			continue
		}
		switch {
		case l == "":
			flush()
			closeLists(0)
		case heading.MatchString(l):
			flush()
			closeLists(0)
			m := heading.FindStringSubmatch(l)
			fmt.Fprintf(&out, "<h%d>%s</h%d>\n", len(m[1]), inline(m[2]), len(m[1]))
		case strings.HasPrefix(l, "|"):
			flush()
			closeLists(0)
			out.WriteString("<table><tr>")
			for _, c := range cells(l) {
				out.WriteString("<th>" + inline(c) + "</th>")
			}
			out.WriteString("</tr>\n")
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
				i++
				row := cells(lines[i])
				if strings.Trim(strings.Join(row, ""), "-: ") == "" {
					continue // the |---|---| line
				}
				out.WriteString("<tr>")
				for _, c := range row {
					out.WriteString("<td>" + inline(c) + "</td>")
				}
				out.WriteString("</tr>\n")
			}
			out.WriteString("</table>\n")
		default:
			para = append(para, strings.TrimSpace(l))
		}
	}
	flush()
	closeLists(0)
	return out.String()
}
