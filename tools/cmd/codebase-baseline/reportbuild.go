package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// build-report turns a folder of small source files into the report page.
// The template (assets/report-template.html) says which slots exist: its
// sections, its JSON data blocks and its footer. The source folder fills them,
// one file per slot, so a later version of a report changes one file and
// rebuilds. The tool does the mechanical work an agent got wrong when it
// pasted into the HTML by hand: JSON that parses and is safe inside a
// <script>, balanced markup, links that resolve, data the page can draw.

const defaultTemplateRel = ".claude/skills/codebase-baseline/assets/report-template.html"

var (
	reTmplBlock   = regexp.MustCompile(`(?s)<script type="application/json" id="([^"]+)">(.*?)</script>`)
	reTmplSection = regexp.MustCompile(`(?s)<section id="([^"]+)" data-asof="([^"]*)">\s*<details( open)?><summary><h2>(.*?)</h2></summary><div class="body">(.*?)</div></details>\s*</section>`)
	reTmplNav     = regexp.MustCompile(`(?s)(<nav class="toc"[^>]*>.*?<ol>)(.*?)(</ol>)`)
	reTmplNavItem = regexp.MustCompile(`<li><a href="#([^"]+)">(.*?)</a></li>`)
	reTmplFooter  = regexp.MustCompile(`(?s)(<footer>\s*<h3>Changelog</h3>)(.*?)(</footer>)`)
	reTmplLogItem = regexp.MustCompile(`(?s)<li><time datetime="([^"]*)">[^<]*</time><span>(.*?)</span></li>`)
	reTmplNote    = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
	reTmplTitle   = regexp.MustCompile(`(?s)<title>.*?</title>`)
)

type tmplBlock struct {
	ID, JSON, Help string
	lo, hi         int
}

type tmplSection struct {
	ID, AsOf, Title, Nav, Body string
	Open                       bool
	lo, hi                     int
}

type logEntry struct{ Date, Text string }

type reportTemplate struct {
	src      string
	blocks   []tmplBlock
	sections []tmplSection
	log      []logEntry
	note     string

	blocksLo, blocksHi int
	navLo, navHi       int
	secLo, secHi       int
	footLo, footHi     int
}

// commentChainStart moves lo back over the HTML comments that end right before it.
func commentChainStart(src string, lo int) int {
	for {
		pre := strings.TrimRight(src[:lo], " \t\r\n")
		if !strings.HasSuffix(pre, "-->") {
			return lo
		}
		c := strings.LastIndex(pre, "<!--")
		if c < 0 {
			return lo
		}
		lo = c
	}
}

// onlyBlankOrComments reports whether s holds nothing but whitespace and comments.
func onlyBlankOrComments(s string) bool {
	return strings.TrimSpace(stripHTMLComments(s)) == ""
}

func parseReportTemplate(src string) (*reportTemplate, error) {
	t := &reportTemplate{src: src}
	for _, m := range reTmplBlock.FindAllStringSubmatchIndex(src, -1) {
		b := tmplBlock{ID: src[m[2]:m[3]], JSON: strings.TrimSpace(src[m[4]:m[5]]), lo: m[0], hi: m[1]}
		pre := strings.TrimRight(src[:m[0]], " \t\r\n")
		if strings.HasSuffix(pre, "-->") {
			if c := strings.LastIndex(pre, "<!--"); c >= 0 {
				h := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(pre[c:], "<!--"), "-->"))
				h = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(h, "DUMMY: replace"), "."))
				b.Help = h
			}
		}
		t.blocks = append(t.blocks, b)
	}
	if len(t.blocks) == 0 {
		return nil, errors.New("template has no <script type=\"application/json\" id=\"...\"> data blocks")
	}
	for i := 1; i < len(t.blocks); i++ {
		if !onlyBlankOrComments(src[t.blocks[i-1].hi:t.blocks[i].lo]) {
			return nil, fmt.Errorf("template has markup between its data blocks %s and %s; the blocks must be together", t.blocks[i-1].ID, t.blocks[i].ID)
		}
	}
	t.blocksLo, t.blocksHi = commentChainStart(src, t.blocks[0].lo), t.blocks[len(t.blocks)-1].hi

	nm := reTmplNav.FindStringSubmatchIndex(src)
	if nm == nil {
		return nil, errors.New(`template has no <nav class="toc"> with an <ol>`)
	}
	t.navLo, t.navHi = nm[4], nm[5]
	navLabel := map[string]string{}
	for _, m := range reTmplNavItem.FindAllStringSubmatch(src[t.navLo:t.navHi], -1) {
		navLabel[m[1]] = html.UnescapeString(m[2])
	}

	for _, m := range reTmplSection.FindAllStringSubmatchIndex(src, -1) {
		s := tmplSection{ID: src[m[2]:m[3]], AsOf: src[m[4]:m[5]], Open: m[6] >= 0,
			Title: html.UnescapeString(strings.TrimSpace(src[m[8]:m[9]])), Body: src[m[10]:m[11]], lo: m[0], hi: m[1]}
		s.Nav = navLabel[s.ID]
		if s.Nav == s.Title {
			s.Nav = ""
		}
		t.sections = append(t.sections, s)
	}
	if len(t.sections) == 0 {
		return nil, errors.New(`template has no <section id data-asof><details><summary><h2>...</h2></summary><div class="body"> sections`)
	}
	for i := 1; i < len(t.sections); i++ {
		if !onlyBlankOrComments(src[t.sections[i-1].hi:t.sections[i].lo]) {
			return nil, fmt.Errorf("template has markup between sections %s and %s", t.sections[i-1].ID, t.sections[i].ID)
		}
	}
	t.secLo, t.secHi = commentChainStart(src, t.sections[0].lo), t.sections[len(t.sections)-1].hi

	fm := reTmplFooter.FindStringSubmatchIndex(src)
	if fm == nil {
		return nil, errors.New("template has no <footer><h3>Changelog</h3> ... </footer>")
	}
	t.footLo, t.footHi = fm[4], fm[5]
	foot := src[t.footLo:t.footHi]
	for _, m := range reTmplLogItem.FindAllStringSubmatch(foot, -1) {
		t.log = append(t.log, logEntry{Date: m[1], Text: strings.TrimSpace(m[2])})
	}
	if i := strings.Index(foot, "</ul>"); i >= 0 {
		if m := reTmplNote.FindStringSubmatch(foot[i:]); m != nil {
			t.note = strings.TrimSpace(m[1])
		}
	}
	if !(t.blocksHi <= t.navLo && t.navHi <= t.secLo && t.secHi <= t.footLo) {
		return nil, errors.New("template parts are not in the order data blocks, nav, sections, footer")
	}
	return t, nil
}

func (t *reportTemplate) block(id string) *tmplBlock {
	for i := range t.blocks {
		if t.blocks[i].ID == id {
			return &t.blocks[i]
		}
	}
	return nil
}

func (t *reportTemplate) section(id string) *tmplSection {
	for i := range t.sections {
		if t.sections[i].ID == id {
			return &t.sections[i]
		}
	}
	return nil
}

// stripHTMLComments removes <!-- --> comments outside <script> and <style>.
// A comment that fills its own line takes the line with it.
func stripHTMLComments(s string) string {
	var b bytes.Buffer
	lower := strings.ToLower(s)
	i := 0
	for i < len(s) {
		pos, kind := -1, ""
		for _, k := range []string{"<!--", "<script", "<style"} {
			if p := strings.Index(lower[i:], k); p >= 0 && (pos < 0 || p < pos) {
				pos, kind = p, k
			}
		}
		if pos < 0 {
			b.WriteString(s[i:])
			break
		}
		pos += i
		b.WriteString(s[i:pos])
		switch kind {
		case "<!--":
			end := strings.Index(s[pos:], "-->")
			if end < 0 {
				i = len(s)
				continue
			}
			end += pos + 3
			// the line the comment starts on, up to the comment
			cur := b.Bytes()
			ls := bytes.LastIndexByte(cur, '\n') + 1
			if len(bytes.TrimLeft(cur[ls:], " \t")) == 0 {
				rest := end
				for rest < len(s) && (s[rest] == ' ' || s[rest] == '\t') {
					rest++
				}
				if rest >= len(s) || s[rest] == '\n' || s[rest] == '\r' {
					b.Truncate(ls)
					if rest < len(s) && s[rest] == '\r' {
						rest++
					}
					if rest < len(s) && s[rest] == '\n' {
						rest++
					}
					i = rest
					continue
				}
			}
			i = end
		default:
			closeTag := "</" + kind[1:]
			end := strings.Index(lower[pos:], closeTag)
			if end < 0 {
				b.WriteString(s[pos:])
				i = len(s)
				continue
			}
			end += pos
			if g := strings.IndexByte(s[end:], '>'); g >= 0 {
				end += g + 1
			} else {
				end = len(s)
			}
			b.WriteString(s[pos:end])
			i = end
		}
	}
	return b.String()
}

// ---- the command ----

type buildOpts struct {
	template, src, out string
	draft              bool
}

func (a *app) cmdBuildReport(args []string) error {
	fl := flag.NewFlagSet("build-report", flag.ContinueOnError)
	var o buildOpts
	fl.StringVar(&o.template, "template", "", "report template (default ~/"+defaultTemplateRel+")")
	fl.StringVar(&o.src, "src", "", "source folder (required)")
	fl.StringVar(&o.out, "out", "", "page to write, usually <ws>/report/index.html (required unless --init)")
	initFlag := fl.Bool("init", false, "write a starter source folder at --src from the template, then stop")
	fl.BoolVar(&o.draft, "draft", false, "build even when files still hold the template's example content (listed as notes)")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline build-report --src <ws>/report-src --out <ws>/report/index.html [--template <file>] [--draft]")
		fmt.Fprintln(a.stderr, "       codebase-baseline build-report --init --src <ws>/report-src [--template <file>]")
		fl.PrintDefaults()
	}
	if _, err := parseFlags(fl, args, a.stderr); err != nil {
		return err
	}
	if o.src == "" || (o.out == "" && !*initFlag) {
		fl.Usage()
		return errors.New("build-report needs --src and --out (or --init and --src)")
	}
	if o.template == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		o.template = filepath.Join(home, defaultTemplateRel)
	}
	tb, err := os.ReadFile(o.template)
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}
	tmpl, err := parseReportTemplate(string(tb))
	if err != nil {
		return fmt.Errorf("template %s: %w", o.template, err)
	}
	if *initFlag {
		files, err := initFiles(tmpl)
		if err != nil {
			return err
		}
		if err := writeStarter(o.src, files); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %d files to %s; fill them (README.md says how), then run build-report --src %s --out <page>\n", len(files), o.src, o.src)
		return nil
	}
	res := buildReport(tmpl, o)
	for _, n := range res.notes {
		fmt.Fprintln(a.stderr, "note:", n)
	}
	if len(res.problems) > 0 {
		fmt.Fprintf(a.stderr, "build-report: %d problems in %s; nothing written\n", len(res.problems), o.src)
		for _, p := range res.problems {
			fmt.Fprintln(a.stderr, "  -", p)
		}
		return errFailed
	}
	if err := writeFileAtomic(o.out, res.page); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "built %s from %s: %d sections, %d data blocks, %d KB\n", o.out, o.src, res.sections, res.blocks, (len(res.page)+1023)/1024)
	for _, w := range res.warnings {
		fmt.Fprintln(a.stderr, "warning:", w)
	}
	return nil
}

func writeFileAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".build-report-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ---- init ----

func writeStarter(dir string, files map[string]string) error {
	if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
		return fmt.Errorf("%s already has files; --init only fills an empty folder (build-report never overwrites sources)", dir)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(files[n]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func initFiles(t *reportTemplate) (map[string]string, error) {
	files := map[string]string{}
	var man strings.Builder
	man.WriteString("{\n  \"sections\": [\n")
	for i, s := range t.sections {
		e := srcSection{ID: s.ID, Title: s.Title, Nav: s.Nav, Open: s.Open}
		var eb bytes.Buffer
		enc := json.NewEncoder(&eb)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(e); err != nil {
			return nil, err
		}
		b := bytes.TrimRight(eb.Bytes(), "\n")
		sep := ","
		if i == len(t.sections)-1 {
			sep = ""
		}
		fmt.Fprintf(&man, "    %s%s\n", b, sep)
		files["sections/"+s.ID+".html"] = strings.TrimSpace(s.Body) + "\n"
	}
	man.WriteString("  ]\n}\n")
	files["sections.json"] = man.String()
	for _, b := range t.blocks {
		files["data/"+b.ID+".json"] = b.JSON + "\n"
	}
	var cl strings.Builder
	cl.WriteString("# One line per update, newest first: date, then what changed (inline HTML allowed).\n")
	cl.WriteString("# The line starting with note: closes the footer.\n")
	for _, e := range t.log {
		fmt.Fprintf(&cl, "%s %s\n", e.Date, e.Text)
	}
	fmt.Fprintf(&cl, "note: %s\n", t.note)
	files["changelog.txt"] = cl.String()
	files["README.md"] = starterReadme(t)
	return files, nil
}

func starterReadme(t *reportTemplate) string {
	var b strings.Builder
	b.WriteString(`# Report sources

These files are the report. Build the page with

    codebase-baseline build-report --src <this folder> --out <ws>/report/index.html

Change a file and build again; never edit the built page. A build that finds a
problem lists every one by file and writes nothing.

- sections.json: the sections in page order. Each has "id", "title" (the
  heading), "nav" (the index label, default the title), "open" (starts
  expanded) and "asof" (the date shown on the section, default the page's
  asOf). To add a section, add an entry where it goes and write its file.
- sections/<id>.html: the body of one section, an HTML fragment. The tool
  adds the heading, the wrapper and the index entry. HTML comments are
  dropped from the page. A section still holding a "DUMMY: replace" comment
  is the template's example text, and the build refuses it (--draft allows it).
- data/<id>.json: the template's JSON data blocks, one file each. Plain JSON,
  no comments. A file equal to the template's example is refused the same way.
  data/scoreboard-data.json can be {"from": "<path>", "note": "<text>"}: the
  rows then come from the file codebase-baseline scoreboard wrote (the path is
  relative to this folder), and the note replaces the one in that file.
  data/treemap-data.json can be {"from": "<path>"} the same way, for the file
  codebase-baseline hotspots wrote.
- changelog.txt: one "YYYY-MM-DD text" line per update, newest first, and one
  "note: text" line for the footer's closing sentence.

Anything else in this folder is an error. README.md is ignored.

Data block shapes, from the template:
`)
	for _, blk := range t.blocks {
		h := blk.Help
		if h == "" {
			h = "(see report-components.md)"
		}
		fmt.Fprintf(&b, "\n- data/%s.json: %s\n", blk.ID, strings.Join(strings.Fields(h), " "))
	}
	return b.String()
}
