package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// srcSection is one entry of sections.json.
type srcSection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Nav   string `json:"nav,omitempty"`
	Open  bool   `json:"open,omitempty"`
	AsOf  string `json:"asof,omitempty"`
}

type buildResult struct {
	page     []byte
	problems []string
	warnings []string
	notes    []string
	sections int
	blocks   int
}

type builder struct {
	t       *reportTemplate
	o       buildOpts
	res     buildResult
	dummies []string
}

func (b *builder) problem(format string, args ...any) {
	b.res.problems = append(b.res.problems, fmt.Sprintf(format, args...))
}

func (b *builder) warn(format string, args ...any) {
	b.res.warnings = append(b.res.warnings, fmt.Sprintf(format, args...))
}

// dummy records a file that still holds the template's example content.
func (b *builder) dummy(format string, args ...any) {
	b.dummies = append(b.dummies, fmt.Sprintf(format, args...))
}

var (
	reISODate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reSectionID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	reDummyMark = regexp.MustCompile(`<!--\s*DUMMY`)
	reLogLine   = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+(\S.*)$`)
)

func lineCol(b []byte, off int) (int, int) {
	if off > len(b) {
		off = len(b)
	}
	if off < 0 {
		off = 0
	}
	line := bytes.Count(b[:off], []byte("\n")) + 1
	col := off - (bytes.LastIndexByte(b[:off], '\n') + 1) + 1
	return line, col
}

func buildReport(t *reportTemplate, o buildOpts) buildResult {
	b := &builder{t: t, o: o}
	b.run()
	if len(b.dummies) > 0 {
		msg := "still holding the template's example content: " + strings.Join(b.dummies, "; ")
		if o.draft {
			b.res.notes = append(b.res.notes, msg)
		} else {
			b.res.problems = append(b.res.problems, msg+" (replace it, or build with --draft)")
		}
	}
	if len(b.res.problems) > 0 {
		b.res.page = nil
	}
	return b.res
}

func (b *builder) run() {
	t := b.t
	files, err := listSourceFiles(b.o.src)
	if err != nil {
		b.problem("%v (start a folder with --init)", err)
		return
	}

	// ---- which file fills which slot ----
	var sectionFiles, dataFiles []string
	haveManifest, haveChangelog := false, false
	blockIDs := make([]string, 0, len(t.blocks))
	for _, blk := range t.blocks {
		blockIDs = append(blockIDs, blk.ID)
	}
	for _, f := range files {
		switch {
		case f == "sections.json":
			haveManifest = true
		case f == "changelog.txt":
			haveChangelog = true
		case f == "README.md":
		case strings.HasPrefix(f, "sections/") && strings.Count(f, "/") == 1 && strings.HasSuffix(f, ".html"):
			sectionFiles = append(sectionFiles, strings.TrimSuffix(strings.TrimPrefix(f, "sections/"), ".html"))
		case strings.HasPrefix(f, "data/") && strings.Count(f, "/") == 1 && strings.HasSuffix(f, ".json"):
			id := strings.TrimSuffix(strings.TrimPrefix(f, "data/"), ".json")
			if t.block(id) == nil {
				b.problem("%s: matches no slot; the template's data blocks are %s", f, strings.Join(blockIDs, ", "))
				continue
			}
			dataFiles = append(dataFiles, id)
		default:
			b.problem("%s: matches no slot (the folder holds sections.json, sections/<id>.html, data/<block>.json, changelog.txt and README.md); move it out of the source folder", f)
		}
	}

	// ---- sections.json and the section bodies ----
	var manifest []srcSection
	if !haveManifest {
		b.problem("sections.json: missing (it lists the sections in page order)")
	} else {
		manifest = b.readManifest()
	}
	inManifest := map[string]bool{}
	for _, s := range manifest {
		inManifest[s.ID] = true
	}
	for _, id := range sectionFiles {
		if !inManifest[id] && haveManifest {
			b.problem("sections/%s.html: not listed in sections.json (add an entry where it belongs in the page, or delete the file)", id)
		}
	}
	for _, ts := range t.sections {
		if haveManifest && !inManifest[ts.ID] {
			b.problem("sections.json: the template's section %q is missing (a report keeps every section the template has; its body can be a short note)", ts.ID)
		}
	}
	haveFile := map[string]bool{}
	for _, id := range sectionFiles {
		haveFile[id] = true
	}
	frags := map[string]*fragInfo{}
	bodies := map[string]string{}
	for _, s := range manifest {
		if !haveFile[s.ID] {
			b.problem("sections.json: section %q has no file sections/%s.html", s.ID, s.ID)
			continue
		}
		raw, err := os.ReadFile(filepath.Join(b.o.src, "sections", s.ID+".html"))
		if err != nil {
			b.problem("sections/%s.html: %v", s.ID, err)
			continue
		}
		bodies[s.ID] = string(raw)
		frags[s.ID] = b.checkFragment("sections/"+s.ID+".html", string(raw))
	}

	// ---- data blocks ----
	haveData := map[string]bool{}
	for _, id := range dataFiles {
		haveData[id] = true
	}
	blockText := map[string]string{}
	blockVal := map[string]any{}
	for _, blk := range t.blocks {
		if !haveData[blk.ID] {
			b.problem("data/%s.json: missing (the template has a data block %q)", blk.ID, blk.ID)
			continue
		}
		text, v, ok := b.loadData(blk)
		if ok {
			blockText[blk.ID] = text
			blockVal[blk.ID] = v
		}
	}

	// The scoreboard's line reads "Snapshot 1 of N" until its data has a note.
	if sv, ok := blockVal["scoreboard-data"].(map[string]any); ok {
		if n, _ := sv["note"].(string); n == "" {
			for _, s := range manifest {
				if strings.Contains(bodies[s.ID], "Snapshot 1 of N") {
					b.dummy("sections/%s.html (the \"Snapshot 1 of N\" line; give scoreboard-data a note)", s.ID)
				}
			}
		}
	}

	// ---- changelog ----
	var logOut []logEntry
	note := ""
	if !haveChangelog {
		b.problem("changelog.txt: missing (the footer's dated lines and its note: line)")
	} else {
		logOut, note = b.readChangelog()
	}

	// ---- page-level checks that need everything ----
	b.crossChecks(manifest, frags, blockVal, logOut)
	if len(b.res.problems) > 0 {
		return
	}

	// ---- assemble ----
	var blocksOut []string
	for _, blk := range t.blocks {
		blocksOut = append(blocksOut, "<script type=\"application/json\" id=\""+blk.ID+"\">\n"+blockText[blk.ID]+"\n</script>")
	}
	var navOut, secOut []string
	for _, s := range manifest {
		nav := s.Nav
		if nav == "" {
			nav = s.Title
		}
		navOut = append(navOut, fmt.Sprintf(`<li><a href="#%s">%s</a></li>`, s.ID, html.EscapeString(nav)))
		secOut = append(secOut, renderSection(s, bodies[s.ID]))
	}
	var foot strings.Builder
	foot.WriteString("\n  <ul class=\"log\">\n")
	for _, e := range logOut {
		fmt.Fprintf(&foot, "    <li><time datetime=\"%s\">%s</time><span>%s</span></li>\n", e.Date, e.Date, e.Text)
	}
	foot.WriteString("  </ul>\n  <p>" + note + "</p>\n")

	var out strings.Builder
	out.WriteString(t.src[:t.blocksLo])
	out.WriteString(strings.Join(blocksOut, "\n\n"))
	out.WriteString(t.src[t.blocksHi:t.navLo])
	out.WriteString("\n    " + strings.Join(navOut, "\n    ") + "\n  ")
	out.WriteString(t.src[t.navHi:t.secLo])
	out.WriteString(strings.Join(secOut, "\n\n"))
	out.WriteString(t.src[t.secHi:t.footLo])
	out.WriteString(foot.String())
	out.WriteString(t.src[t.footHi:])
	page := stripHTMLComments(out.String())
	if meta, ok := blockVal["meta-data"].(map[string]any); ok {
		if p, _ := meta["project"].(string); p != "" {
			page = reTmplTitle.ReplaceAllLiteralString(page, "<title>"+html.EscapeString(p)+" Baseline</title>")
		}
	}
	if len(page) > 1<<20 {
		b.warn("the page is %d KB; keep it under 1 MB (trim the treemap or the long tables)", len(page)/1024)
	}
	b.res.page = []byte(page)
	b.res.sections = len(manifest)
	b.res.blocks = len(t.blocks)
}

func renderSection(s srcSection, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<section id="%s"`, s.ID)
	if s.AsOf != "" {
		fmt.Fprintf(&b, ` data-asof="%s"`, s.AsOf)
	}
	b.WriteString(">\n<details")
	if s.Open {
		b.WriteString(" open")
	}
	fmt.Fprintf(&b, "><summary><h2>%s</h2></summary><div class=\"body\">\n", html.EscapeString(s.Title))
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n</div></details>\n</section>")
	return b.String()
}

func listSourceFiles(dir string) ([]string, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("source folder %s: %w", dir, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("source folder %s is not a directory", dir)
	}
	var out []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

func (b *builder) readManifest() []srcSection {
	path := filepath.Join(b.o.src, "sections.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		b.problem("sections.json: %v", err)
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		b.problem("sections.json: %s", jsonErrText(raw, err))
		return nil
	}
	var doc struct {
		Sections []srcSection `json:"sections"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		b.problem("sections.json: %v (an entry has id, title, nav, open and asof)", err)
		return nil
	}
	seen := map[string]bool{}
	var out []srcSection
	for i, s := range doc.Sections {
		where := fmt.Sprintf("sections.json: entry %d", i+1)
		if s.ID != "" {
			where = fmt.Sprintf("sections.json: section %q", s.ID)
		}
		ok := true
		switch {
		case !reSectionID.MatchString(s.ID):
			b.problem("%s: id must be lowercase letters, digits and dashes, starting with a letter", where)
			ok = false
		case seen[s.ID]:
			b.problem("%s: listed twice", where)
			ok = false
		}
		seen[s.ID] = true
		if strings.TrimSpace(s.Title) == "" {
			b.problem("%s: has no title", where)
			ok = false
		}
		if s.AsOf != "" && !reISODate.MatchString(s.AsOf) {
			b.problem("%s: asof %q is not an ISO date (YYYY-MM-DD)", where, s.AsOf)
			ok = false
		}
		if ok {
			out = append(out, s)
		}
	}
	return out
}

func jsonErrText(raw []byte, err error) string {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		l, c := lineCol(raw, int(se.Offset))
		return fmt.Sprintf("line %d, column %d: %v", l, c, err)
	}
	return err.Error()
}

// ---- section fragments ----

type fragRef struct {
	id   string
	line int
}

type fragInfo struct {
	ids   map[string]int
	hrefs []fragRef
	bars  []fragRef
}

var bannedTags = map[string]bool{"script": true, "style": true, "iframe": true, "object": true, "embed": true, "base": true, "link": true, "meta": true, "form": true}

// checkFragment looks at one section body: markup that would run, balance,
// and what the page's own script needs from it.
func (b *builder) checkFragment(file, src string) *fragInfo {
	fi := &fragInfo{ids: map[string]int{}}
	if reDummyMark.MatchString(src) {
		b.dummy("%s (a DUMMY comment)", file)
	}
	for _, p := range checkBalance(src) {
		b.problem("%s:%d: %s", file, p.line, p.msg)
	}
	root := parseHTML(src)
	var walk func(n *hnode)
	walk = func(n *hnode) {
		if n.tag != "" && n.tag != "#root" {
			if bannedTags[n.tag] {
				b.problem("%s:%d: <%s> is not allowed in a section (the page's own script draws charts from the data files)", file, n.line, n.tag)
			}
			for k, v := range n.attrs {
				if strings.HasPrefix(k, "on") {
					b.problem("%s:%d: event handler attribute %s= is not allowed", file, n.line, k)
				}
				if hasScriptScheme(v) {
					b.problem("%s:%d: %s=%q would run script", file, n.line, k, truncate(v, 40))
				}
			}
			if id := n.attrs["id"]; id != "" {
				if _, dup := fi.ids[id]; dup {
					b.problem("%s:%d: id %q appears twice in this section", file, n.line, id)
				}
				fi.ids[id] = n.line
				if n.hasClass("bars") {
					fi.bars = append(fi.bars, fragRef{id, n.line})
				}
			}
			if n.tag == "a" {
				if name := n.attrs["name"]; name != "" {
					fi.ids[name] = n.line
				}
				if h := n.attrs["href"]; strings.HasPrefix(h, "#") {
					fi.hrefs = append(fi.hrefs, fragRef{strings.TrimPrefix(h, "#"), n.line})
				}
			}
			if n.tag == "pre" && n.hasClass("mermaid") {
				for _, c := range n.children {
					if c.tag != "" {
						b.problem("%s:%d: a mermaid diagram holds a <%s> tag; write < as &lt; inside the diagram", file, c.line, c.tag)
						break
					}
				}
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	return fi
}

func hasScriptScheme(v string) bool {
	var sb strings.Builder
	for _, r := range strings.ToLower(v) {
		if r > ' ' && r != 0x7f {
			sb.WriteRune(r)
		}
	}
	s := sb.String()
	return strings.HasPrefix(s, "javascript:") || strings.HasPrefix(s, "vbscript:")
}

type balProblem struct {
	line int
	msg  string
}

var optionalEnd = map[string]bool{"p": true, "li": true, "dt": true, "dd": true, "tr": true, "td": true, "th": true, "thead": true, "tbody": true, "tfoot": true, "option": true}

// checkBalance finds tags that are never closed or closed without being
// opened, which in a fragment shifts everything after it out of place.
func checkBalance(src string) []balProblem {
	type open struct {
		name string
		line int
	}
	var stack []open
	var out []balProblem
	line := 1
	top := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1].name
	}
	popWhile := func(names ...string) {
		for len(stack) > 0 {
			hit := false
			for _, n := range names {
				if top() == n {
					hit = true
				}
			}
			if !hit {
				return
			}
			stack = stack[:len(stack)-1]
		}
	}
	popOne := func(names ...string) {
		for _, n := range names {
			if top() == n {
				stack = stack[:len(stack)-1]
				return
			}
		}
	}
	i := 0
	for i < len(src) {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			break
		}
		line += strings.Count(src[i:i+lt], "\n")
		i += lt
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest, "-->")
			if end < 0 {
				out = append(out, balProblem{line, "a comment is never closed"})
				return out
			}
			line += strings.Count(rest[:end], "\n")
			i += end + 3
			continue
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return out
			}
			i += end + 1
			continue
		}
		if len(rest) < 2 || !(rest[1] == '/' || isAlpha(rest[1])) {
			i++
			continue
		}
		end := tagEnd(rest)
		if end < 0 {
			out = append(out, balProblem{line, "a tag is never closed with >"})
			return out
		}
		raw := rest[1:end]
		startLine := line
		line += strings.Count(rest[:end+1], "\n")
		i += end + 1
		if raw[0] == '/' {
			name := strings.ToLower(strings.TrimSpace(raw[1:]))
			j := len(stack) - 1
			for j >= 0 && stack[j].name != name {
				j--
			}
			if j < 0 {
				if name != "p" {
					out = append(out, balProblem{startLine, fmt.Sprintf("</%s> closes nothing", name)})
				}
				continue
			}
			for k := len(stack) - 1; k > j; k-- {
				if !optionalEnd[stack[k].name] {
					out = append(out, balProblem{stack[k].line, fmt.Sprintf("<%s> is not closed before </%s> at line %d", stack[k].name, name, startLine)})
				}
			}
			stack = stack[:j]
			continue
		}
		selfClose := strings.HasSuffix(raw, "/")
		name, _ := parseTag(strings.TrimSuffix(raw, "/"))
		switch name {
		case "li":
			popWhile("p")
			popOne("li")
		case "dt", "dd":
			popWhile("p")
			popOne("dt", "dd")
		case "tr":
			popWhile("td", "th", "p")
			popOne("tr")
		case "td", "th":
			popWhile("p")
			popOne("td", "th")
		case "thead", "tbody", "tfoot":
			popWhile("td", "th", "tr", "p")
			popOne("thead", "tbody", "tfoot")
		}
		if closesP[name] {
			popOne("p")
		}
		if voidTags[name] || selfClose {
			continue
		}
		if rawTextTags[name] {
			cl := strings.Index(strings.ToLower(src[i:]), "</"+name)
			if cl < 0 {
				out = append(out, balProblem{startLine, fmt.Sprintf("<%s> is never closed", name)})
				return out
			}
			line += strings.Count(src[i:i+cl], "\n")
			i += cl
			if g := strings.IndexByte(src[i:], '>'); g >= 0 {
				i += g + 1
			}
			continue
		}
		stack = append(stack, open{name, startLine})
	}
	for _, o := range stack {
		if !optionalEnd[o.name] {
			out = append(out, balProblem{o.line, fmt.Sprintf("<%s> is never closed", o.name)})
		}
	}
	return out
}

// ---- changelog ----

func (b *builder) readChangelog() ([]logEntry, string) {
	raw, err := os.ReadFile(filepath.Join(b.o.src, "changelog.txt"))
	if err != nil {
		b.problem("changelog.txt: %v", err)
		return nil, ""
	}
	var entries []logEntry
	note, haveNote := "", false
	for i, ln := range strings.Split(string(raw), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(ln, "note:"); ok {
			if haveNote {
				b.problem("changelog.txt:%d: a second note: line (there is one closing note)", i+1)
			}
			note, haveNote = strings.TrimSpace(rest), true
			continue
		}
		m := reLogLine.FindStringSubmatch(ln)
		if m == nil {
			b.problem("changelog.txt:%d: a line is a date (YYYY-MM-DD), a space and the text, or note: and the text", i+1)
			continue
		}
		entries = append(entries, logEntry{m[1], m[2]})
	}
	if !haveNote || note == "" {
		b.problem("changelog.txt: has no note: line (the footer's closing sentence on what was read, run and left alone)")
	}
	if len(entries) == 0 {
		b.problem("changelog.txt: has no dated line (one per update, newest first)")
	}
	for i, e := range entries {
		if i > 0 && e.Date > entries[i-1].Date {
			b.problem("changelog.txt: %s is listed below %s; newest first", e.Date, entries[i-1].Date)
		}
	}
	// Same markup rules as a section.
	frag := strings.Join(logTexts(entries), "\n") + "\n" + note
	for _, p := range checkBalance(frag) {
		b.problem("changelog.txt: %s (in the text of a line)", p.msg)
	}
	root := parseHTML(frag)
	var walk func(n *hnode)
	walk = func(n *hnode) {
		if n.tag != "" && n.tag != "#root" {
			if bannedTags[n.tag] {
				b.problem("changelog.txt: <%s> is not allowed", n.tag)
			}
			for k, v := range n.attrs {
				if strings.HasPrefix(k, "on") || hasScriptScheme(v) {
					b.problem("changelog.txt: attribute %s= is not allowed", k)
				}
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	tl := map[string]bool{}
	for _, e := range b.t.log {
		tl[e.Date+"\x00"+e.Text] = true
	}
	for _, e := range entries {
		if tl[e.Date+"\x00"+e.Text] {
			b.dummy("changelog.txt (the line dated %s)", e.Date)
		}
	}
	if note != "" && note == b.t.note {
		b.dummy("changelog.txt (the note: line)")
	}
	return entries, note
}

func logTexts(es []logEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Text)
	}
	return out
}

// ---- crosschecks over the assembled page ----

// blockMounts are the elements the page's script fills from a data block.
var blockMounts = map[string][]string{
	"summary-data":    {"summary-body"},
	"scoreboard-data": {"scoreboard-body"},
	"treemap-data":    {"treemap", "tm-crumbs", "tm-ramp"},
	"quality-data":    {"axes"},
}

func (b *builder) crossChecks(manifest []srcSection, frags map[string]*fragInfo, blockVal map[string]any, log []logEntry) {
	t := b.t
	// ids: the page's own markup, the section ids and each fragment's.
	static := map[string]bool{"top": true}
	rest := t.src[:t.secLo] + t.src[t.secHi:]
	var collect func(n *hnode)
	collect = func(n *hnode) {
		if id := n.attrs["id"]; id != "" {
			static[id] = true
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	collect(parseHTML(rest))
	owner := map[string]string{}
	for _, s := range manifest {
		if static[s.ID] {
			b.problem("sections.json: section id %q is already used by the page's own markup", s.ID)
		}
		owner[s.ID] = "sections.json"
		static[s.ID] = true
	}
	for _, s := range manifest {
		fi := frags[s.ID]
		if fi == nil {
			continue
		}
		ids := make([]string, 0, len(fi.ids))
		for id := range fi.ids {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			file := "sections/" + s.ID + ".html"
			switch {
			case owner[id] != "":
				b.problem("%s:%d: id %q is already used by %s", file, fi.ids[id], id, owner[id])
			case static[id]:
				b.problem("%s:%d: id %q is already used by the page's own markup", file, fi.ids[id], id)
			default:
				owner[id] = file
			}
		}
	}
	known := func(id string) bool {
		if static[id] {
			return true
		}
		_, ok := owner[id]
		return ok
	}
	for _, s := range manifest {
		fi := frags[s.ID]
		if fi == nil {
			continue
		}
		for _, h := range fi.hrefs {
			if id := unescapeID(h.id); id != "" && !known(id) {
				b.problem("sections/%s.html:%d: link #%s points at no element", s.ID, h.line, h.id)
			}
		}
	}
	// links inside the data blocks
	for _, blk := range t.blocks {
		v, ok := blockVal[blk.ID]
		if !ok {
			continue
		}
		walkJSONStrings(v, "", func(p, s string) {
			if !strings.Contains(s, "href") {
				return
			}
			for _, a := range findLinks(s) {
				if id := unescapeID(strings.TrimPrefix(a, "#")); strings.HasPrefix(a, "#") && id != "" && !known(id) {
					b.problem("data/%s.json at %s: link %s points at no element", blk.ID, p, a)
				}
			}
		})
	}
	// links in the changelog text
	for _, e := range log {
		for _, a := range findLinks(e.Text) {
			if id := unescapeID(strings.TrimPrefix(a, "#")); strings.HasPrefix(a, "#") && id != "" && !known(id) {
				b.problem("changelog.txt: link %s points at no element", a)
			}
		}
	}

	// mounts: what the page's script needs in order to draw each block
	allIDs := func(id string) bool { _, ok := owner[id]; return ok || static[id] }
	for blk, mounts := range blockMounts {
		if t.block(blk) == nil || blockVal[blk] == nil {
			continue
		}
		for _, m := range mounts {
			if !allIDs(m) {
				b.problem("data/%s.json: no section has an element with id=%q, so the page cannot draw it (the template's section has <... id=%q>)", blk, m, m)
			}
		}
	}
	if bd, ok := blockVal["bars-data"].(map[string]any); ok {
		keys := make([]string, 0, len(bd))
		for k := range bd {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !allIDs(k) {
				b.problem("data/bars-data.json: key %q has no element with id=%q in any section", k, k)
			}
		}
		for _, s := range manifest {
			if fi := frags[s.ID]; fi != nil {
				for _, br := range fi.bars {
					if _, ok := bd[br.id]; !ok {
						b.problem("sections/%s.html:%d: bars chart #%s has no key %q in data/bars-data.json", s.ID, br.line, br.id, br.id)
					}
				}
			}
		}
	}
	for _, c := range chartList(blockVal["commits-data"]) {
		if id, _ := c["id"].(string); id != "" && !allIDs(id) {
			b.problem("data/commits-data.json: chart %q has no element with id=%q in any section", id, id)
		}
	}
}

func unescapeID(s string) string {
	return html.UnescapeString(s)
}

// findLinks returns the href values of the <a> elements in a text field.
func findLinks(s string) []string {
	var out []string
	var walk func(n *hnode)
	walk = func(n *hnode) {
		if n.tag == "a" {
			if h, ok := n.attrs["href"]; ok {
				out = append(out, h)
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(parseHTML(s))
	return out
}

func chartList(v any) []map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return []map[string]any{t}
	case []any:
		var out []map[string]any
		for _, e := range t {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
