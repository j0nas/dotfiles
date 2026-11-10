package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type reportFailure struct {
	Check   string `json:"check"`
	Line    int    `json:"line,omitempty"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message"`
	Snippet string `json:"snippet,omitempty"`
}

type reportResult struct {
	SchemaVersion int             `json:"schema_version"`
	Tool          toolInfo        `json:"tool"`
	Report        string          `json:"report"`
	CheckedAt     string          `json:"checked_at"`
	Passed        bool            `json:"passed"`
	Counts        map[string]int  `json:"failure_counts"`
	Failures      []reportFailure `json:"failures"`
	Harvested     *harvested      `json:"harvested,omitempty"`
}

func (a *app) cmdCheckReport(args []string) error {
	fl := flag.NewFlagSet("check-report", flag.ContinueOnError)
	repo := fl.String("repo", "", "harvest the repo's phase, release and ticket labels and codenames and fail when one appears undefined")
	allowFile := fl.String("allow", "", "file of extra well-known terms, one per line")
	format := fl.String("format", "text", "text or json")
	strict := fl.Bool("strict-each", false, "require a definition at every occurrence, not just the first")
	list := fl.Bool("list-harvested", false, "with --repo: print what was harvested and exit")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline check-report <report.html> [--repo <repo>] [--allow <file>] [--format text|json] [--strict-each]")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 1 && !(*list && *repo != "") {
		fl.Usage()
		return fmt.Errorf("check-report needs <report.html>")
	}
	allow := map[string]bool{}
	for _, t := range defaultAllow {
		allow[t] = true
	}
	if *allowFile != "" {
		extra, err := readAllowFile(*allowFile)
		if err != nil {
			return err
		}
		for _, t := range extra {
			allow[t] = true
		}
	}
	var h *harvested
	if *repo != "" {
		hv := harvestRepo(*repo, allow)
		h = &hv
		if *list {
			b, _ := json.MarshalIndent(h, "", "  ")
			fmt.Fprintln(a.stdout, string(b))
			return nil
		}
	}
	src, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}
	res := checkReport(string(src), allow, h, *strict)
	res.Report = pos[0]
	res.CheckedAt = a.now().UTC().Format(time.RFC3339)
	if *format == "json" {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Fprintln(a.stdout, string(b))
	} else {
		printReportText(a.stdout, res)
	}
	if !res.Passed {
		return errFailed
	}
	return nil
}

func printReportText(w mdWriter, r reportResult) {
	if r.Passed {
		fmt.Fprintf(w, "check-report: %s passes all checks\n", r.Report)
		return
	}
	byCheck := map[string][]reportFailure{}
	var order []string
	for _, f := range r.Failures {
		if _, ok := byCheck[f.Check]; !ok {
			order = append(order, f.Check)
		}
		byCheck[f.Check] = append(byCheck[f.Check], f)
	}
	fmt.Fprintf(w, "check-report: %s has %d problems\n", r.Report, len(r.Failures))
	for _, c := range order {
		fmt.Fprintf(w, "\n%s (%d)\n", c, len(byCheck[c]))
		for _, f := range byCheck[c] {
			loc := ""
			if f.Line > 0 {
				loc = fmt.Sprintf("line %d: ", f.Line)
			}
			fmt.Fprintf(w, "  - %s%s\n", loc, f.Message)
			if f.Snippet != "" {
				fmt.Fprintf(w, "      ...%s...\n", f.Snippet)
			}
		}
	}
}

var skipText = map[string]bool{"code": true, "pre": true, "kbd": true, "samp": true, "script": true, "style": true, "noscript": true, "template": true, "head": true, "textarea": true}

var blockTags = map[string]bool{
	"p": true, "div": true, "li": true, "ul": true, "ol": true, "section": true, "article": true, "td": true, "th": true,
	"tr": true, "table": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "dt": true, "dd": true,
	"blockquote": true, "figure": true, "figcaption": true, "body": true, "main": true, "aside": true, "header": true, "footer": true, "nav": true,
}

var glossaryRe = regexp.MustCompile(`(?i)gloss`)

func isSupG(n *hnode) bool { return n != nil && n.tag == "sup" && n.hasClass("g") }

// nextSibling is the next sibling that is an element or non-blank text.
func nextSibling(n *hnode) *hnode {
	if n.parent == nil {
		return nil
	}
	sibs := n.parent.children
	for i, s := range sibs {
		if s == n {
			for _, nx := range sibs[i+1:] {
				if nx.tag == "" && strings.TrimSpace(nx.text) == "" {
					continue
				}
				return nx
			}
			return nil
		}
	}
	return nil
}

// followedBySupG reports whether a glossary link marker comes right after the
// node, climbing out of inline wrappers such as <a> or <strong>.
func followedBySupG(n *hnode) bool {
	for cur := n; cur != nil && cur.parent != nil; cur = cur.parent {
		if nx := nextSibling(cur); nx != nil {
			return isSupG(nx)
		}
		if blockTags[cur.parent.tag] {
			return false
		}
	}
	return false
}

type ctx struct {
	skip, inGlossary, defined, inAnchor bool
}

type occurrence struct {
	Text    string
	Line    int
	Snippet string
	Defined bool
}

func snippetAround(text string, start, end int) string {
	s, e := start-45, end+45
	if s < 0 {
		s = 0
	}
	if e > len(text) {
		e = len(text)
	}
	return strings.Join(strings.Fields(text[s:e]), " ")
}

func checkReport(src string, allow map[string]bool, h *harvested, strict bool) reportResult {
	res := reportResult{SchemaVersion: schemaVersion, Tool: currentTool(), Failures: []reportFailure{}, Counts: map[string]int{}}
	root := parseHTML(src)

	ids := map[string]*hnode{}
	var hrefs []*hnode
	var supLinks []*hnode
	var jsonScripts []*hnode
	var glossaryRoots []*hnode
	var collect func(n *hnode)
	collect = func(n *hnode) {
		if n.tag != "" {
			if id := n.attrs["id"]; id != "" {
				if _, dup := ids[id]; !dup {
					ids[id] = n
				}
			}
			if n.tag == "a" {
				if name := n.attrs["name"]; name != "" {
					ids[name] = n
				}
				if strings.HasPrefix(n.attrs["href"], "#") {
					hrefs = append(hrefs, n)
				}
			}
			if isSupG(n) {
				supLinks = append(supLinks, n)
			}
			if n.tag == "script" && strings.EqualFold(strings.TrimSpace(n.attrs["type"]), "application/json") {
				jsonScripts = append(jsonScripts, n)
			}
			if n.tag != "#root" && (glossaryRe.MatchString(n.attrs["id"]) || glossaryRe.MatchString(n.attrs["class"])) && n.tag != "a" && n.tag != "sup" {
				glossaryRoots = append(glossaryRoots, n)
			}
			if len(n.tag) == 2 && n.tag[0] == 'h' && n.tag[1] >= '1' && n.tag[1] <= '6' && strings.EqualFold(strings.TrimSpace(nodeText(n)), "glossary") && n.parent != nil && n.parent.tag != "#root" && n.parent.tag != "body" {
				glossaryRoots = append(glossaryRoots, n.parent)
			}
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	collect(root)
	inGlossary := map[*hnode]bool{}
	var mark func(n *hnode)
	mark = func(n *hnode) {
		inGlossary[n] = true
		for _, c := range n.children {
			mark(c)
		}
	}
	for _, g := range glossaryRoots {
		mark(g)
	}

	allowed := func(tok token) bool {
		return allow[tok.Norm] || allow[tok.Text] || (strings.HasSuffix(tok.Text, "s") && allow[strings.TrimSuffix(tok.Text, "s")])
	}
	definedTerms := map[string]bool{}
	type pending struct {
		check, key string
		occ        occurrence
		kind       string
	}
	var bare []pending
	counts := map[string]int{}
	var codenameRes map[string]*regexp.Regexp

	record := func(check, key, kind string, occ occurrence) {
		if occ.Defined {
			definedTerms[check+"|"+key] = true
			return
		}
		counts[check+"|"+key]++
		if strict || !definedTerms[check+"|"+key] {
			bare = append(bare, pending{check: check, key: key, occ: occ, kind: kind})
		}
	}

	harvestLabels := map[string]bool{}
	if h != nil {
		for _, l := range h.Labels {
			harvestLabels[l] = true
		}
		codenameRes = map[string]*regexp.Regexp{}
		for _, c := range h.Codenames {
			codenameRes[c] = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(c) + `($|[^A-Za-z0-9_-])`)
		}
	}
	labelSorted := []string{}
	for l := range harvestLabels {
		labelSorted = append(labelSorted, l)
	}
	sort.Slice(labelSorted, func(i, j int) bool { return len(labelSorted[i]) > len(labelSorted[j]) })

	var walk func(n *hnode, c ctx)
	walk = func(n *hnode, c ctx) {
		if n.tag == "" {
			if c.skip {
				return
			}
			text := n.text
			// (c) session leaks, anywhere in visible text, glossary included.
			for _, re := range sessionPhrases {
				for _, m := range re.FindAllStringIndex(text, -1) {
					res.Failures = append(res.Failures, reportFailure{Check: "session-leak phrases", Line: n.line, Token: text[m[0]:m[1]], Message: fmt.Sprintf("%q talks about the conversation, not the codebase", text[m[0]:m[1]]), Snippet: snippetAround(text, m[0], m[1])})
				}
			}
			if !c.inAnchor {
				for _, m := range rePlanBare.FindAllStringIndex(text, -1) {
					res.Failures = append(res.Failures, reportFailure{Check: "session-leak phrases", Line: n.line, Token: text[m[0]:m[1]], Message: `"the plan" without a link: say which plan, and link it`, Snippet: snippetAround(text, m[0], m[1])})
				}
			}
			if c.inGlossary {
				return
			}
			trailingOK := func(tok token) bool {
				return strings.TrimFunc(text[tok.End:], func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(".,;:)]}\"'’”–—", r) }) == ""
			}
			covered := make([]bool, len(text)+1)
			// (b) repo labels first, so they aren't reported twice as generic tokens.
			if h != nil {
				for _, l := range labelSorted {
					for _, at := range wordIndexes(text, l) {
						overlap := false
						for k := at; k < at+len(l); k++ {
							if covered[k] {
								overlap = true
							}
						}
						if overlap {
							continue
						}
						for k := at; k < at+len(l); k++ {
							covered[k] = true
						}
						tok := token{Text: l, End: at + len(l)}
						defined := c.defined || (trailingOK(tok) && followedBySupG(n))
						record("repo label or codename left undefined", l, "label", occurrence{Text: l, Line: n.line, Snippet: snippetAround(text, at, at+len(l)), Defined: defined})
					}
				}
				for name, re := range codenameRes {
					for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
						at, end := m[3], m[4]
						if covered[at] {
							continue
						}
						for k := at; k < end; k++ {
							covered[k] = true
						}
						tok := token{Text: name, End: end}
						defined := c.defined || (trailingOK(tok) && followedBySupG(n))
						record("repo label or codename left undefined", name, "codename", occurrence{Text: text[at:end], Line: n.line, Snippet: snippetAround(text, at, end), Defined: defined})
					}
				}
			}
			for _, tok := range findTokens(text) {
				if allowed(tok) {
					continue
				}
				if h != nil && covered[tok.Start] {
					continue
				}
				defined := c.defined || (trailingOK(tok) && followedBySupG(n))
				record("identifier-like term left undefined", tok.Norm, tok.Kind, occurrence{Text: tok.Text, Line: n.line, Snippet: snippetAround(text, tok.Start, tok.End), Defined: defined})
			}
			return
		}
		nc := c
		if skipText[n.tag] {
			nc.skip = true
		}
		if inGlossary[n] {
			nc.inGlossary = true
		}
		if (n.tag == "abbr" && n.attrs["title"] != "") || n.tag == "dfn" {
			nc.defined = true
		}
		if n.tag == "a" {
			nc.inAnchor = true
		}
		if isSupG(n) {
			nc.skip = true
		}
		for _, ch := range n.children {
			walk(ch, nc)
		}
	}
	walk(root, ctx{})

	reported := map[string]bool{}
	for _, p := range bare {
		key := p.check + "|" + p.key
		if !strict && reported[key] {
			continue
		}
		reported[key] = true
		msg := fmt.Sprintf("%s %q appears with no definition (wrap it in <abbr title>, <dfn>, or follow it with <sup class=\"g\"> linking the glossary)", p.kind, p.occ.Text)
		if !strict && counts[key] > 1 {
			msg = fmt.Sprintf("%s %q has no definition at first use (%d bare occurrences in all)", p.kind, p.occ.Text, counts[key])
		}
		if p.check == "identifier-like term left undefined" {
			msg += "; if any engineer knows it, add it to an --allow file instead"
		}
		res.Failures = append(res.Failures, reportFailure{Check: p.check, Line: p.occ.Line, Token: p.occ.Text, Message: msg, Snippet: p.occ.Snippet})
	}

	// (e) the data blocks the page is built from.
	for _, s := range jsonScripts {
		id := s.attrs["id"]
		name := "script"
		if id != "" {
			name = "script#" + id
		}
		text := nodeText(s)
		var v any
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			msg := err.Error()
			line := s.line
			if se, ok := err.(*json.SyntaxError); ok {
				at := int(se.Offset)
				if at > len(text) {
					at = len(text)
				}
				line += strings.Count(text[:at], "\n")
				msg = fmt.Sprintf("%s at line %d of the block", se.Error(), strings.Count(text[:at], "\n")+1)
			}
			if oddQuotes(text) {
				msg += "; the block may contain a literal </script inside a string, which ends the block early: write <\\/script"
			}
			res.Failures = append(res.Failures, reportFailure{Check: "embedded JSON data", Line: line, Token: id, Message: fmt.Sprintf("%s does not parse: %s", name, msg)})
			continue
		}
		// The page renders these strings as HTML, so markup that runs is a hole.
		blockLine := s.line
		walkJSONStrings(v, "", func(p, val string) {
			if why := scriptyString(val); why != "" {
				res.Failures = append(res.Failures, reportFailure{Check: "embedded JSON data", Line: blockLine, Token: id,
					Message: fmt.Sprintf("%s at %s contains %s, which would run when the report renders text fields as HTML", name, p, why), Snippet: truncate(val, 90)})
			}
		})
		if id == "summary-data" {
			var sm struct {
				Facts   []any `json:"facts"`
				Risks   []any `json:"risks"`
				Actions []any `json:"actions"`
			}
			_ = json.Unmarshal([]byte(text), &sm)
			for _, c := range []struct {
				what    string
				n, want int
			}{{"facts", len(sm.Facts), 5}, {"risks", len(sm.Risks), 5}, {"actions", len(sm.Actions), 3}} {
				if c.n != c.want {
					res.Failures = append(res.Failures, reportFailure{Check: "embedded JSON data", Line: s.line, Token: id, Message: fmt.Sprintf("summary-data must have exactly %d %s, has %d", c.want, c.what, c.n)})
				}
			}
		}
	}

	// (d) links and glossary.
	referenced := map[string]bool{}
	for _, a := range hrefs {
		id, _ := url.PathUnescape(strings.TrimPrefix(a.attrs["href"], "#"))
		if id == "" || id == "top" {
			continue
		}
		referenced[id] = true
		if _, ok := ids[id]; !ok {
			res.Failures = append(res.Failures, reportFailure{Check: "broken links and glossary", Line: a.line, Token: a.attrs["href"], Message: fmt.Sprintf("href=%q has no element with that id", a.attrs["href"]), Snippet: strings.Join(strings.Fields(nodeText(a)), " ")})
		}
	}
	entries := map[string]*hnode{}
	for _, g := range glossaryRoots {
		var find func(n *hnode)
		find = func(n *hnode) {
			if n != g && n.tag != "" && n.attrs["id"] != "" {
				switch n.tag {
				case "dt", "dd", "li", "tr", "dfn", "div", "p", "article", "details", "section", "span", "h3", "h4":
					entries[n.attrs["id"]] = n
				}
			}
			for _, c := range n.children {
				find(c)
			}
		}
		find(g)
	}
	if len(supLinks) > 0 && len(glossaryRoots) == 0 {
		res.Failures = append(res.Failures, reportFailure{Check: "broken links and glossary", Line: supLinks[0].line, Message: `the report uses <sup class="g"> markers but has no glossary section (an element whose id or class contains "glossary")`})
	}
	for _, s := range supLinks {
		var link *hnode
		var find func(n *hnode)
		find = func(n *hnode) {
			if n.tag == "a" && link == nil {
				link = n
			}
			for _, c := range n.children {
				find(c)
			}
		}
		find(s)
		if link == nil || !strings.HasPrefix(link.attrs["href"], "#") {
			res.Failures = append(res.Failures, reportFailure{Check: "broken links and glossary", Line: s.line, Message: `<sup class="g"> marker without an in-page link`, Snippet: strings.TrimSpace(nodeText(s))})
			continue
		}
		id, _ := url.PathUnescape(strings.TrimPrefix(link.attrs["href"], "#"))
		if _, ok := ids[id]; !ok {
			res.Failures = append(res.Failures, reportFailure{Check: "broken links and glossary", Line: s.line, Token: link.attrs["href"], Message: fmt.Sprintf("glossary link %q points at a missing glossary entry", link.attrs["href"])})
		} else if len(glossaryRoots) > 0 && entries[id] == nil {
			res.Failures = append(res.Failures, reportFailure{Check: "broken links and glossary", Line: s.line, Token: link.attrs["href"], Message: fmt.Sprintf("glossary link %q points outside the glossary", link.attrs["href"])})
		}
	}
	var unref []string
	for id := range entries {
		if !referenced[id] {
			unref = append(unref, id)
		}
	}
	sort.Strings(unref)
	for _, id := range unref {
		n := entries[id]
		res.Failures = append(res.Failures, reportFailure{Check: "broken links and glossary", Line: n.line, Token: id, Message: fmt.Sprintf("glossary entry %q is never referenced", id), Snippet: strings.Join(strings.Fields(nodeText(n)), " ")})
	}

	sort.SliceStable(res.Failures, func(i, j int) bool {
		if res.Failures[i].Check != res.Failures[j].Check {
			return res.Failures[i].Check < res.Failures[j].Check
		}
		return res.Failures[i].Line < res.Failures[j].Line
	})
	for _, f := range res.Failures {
		res.Counts[f.Check]++
	}
	res.Passed = len(res.Failures) == 0
	res.Harvested = h
	return res
}

func isWordChar(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// wordIndexes finds term in text where it stands alone: not inside a longer
// word, number or dotted/dashed label.
func wordIndexes(text, term string) []int {
	var out []int
	from := 0
	for {
		i := strings.Index(text[from:], term)
		if i < 0 {
			return out
		}
		at := from + i
		end := at + len(term)
		ok := true
		if at > 0 && (isWordChar(text[at-1]) || (text[at-1] == '.' || text[at-1] == '-') && at > 1 && isWordChar(text[at-2])) {
			ok = false
		}
		if end < len(text) {
			nx := text[end]
			if isWordChar(nx) {
				ok = false
			}
			if (nx == '.' || nx == '-') && end+1 < len(text) && isWordChar(text[end+1]) {
				ok = false
			}
		}
		if ok {
			out = append(out, at)
		}
		from = at + 1
	}
}

var reOnHandler = regexp.MustCompile(`(?i)\son\w+\s*=`)

// scriptyString says what in a string would execute inside rendered HTML.
func scriptyString(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "<script"):
		return "<script"
	case strings.Contains(l, "javascript:"):
		return "javascript:"
	case reOnHandler.MatchString(s):
		return "an inline event handler (on...=)"
	}
	return ""
}

// walkJSONStrings calls f with the path and value of every string in v.
func walkJSONStrings(v any, p string, f func(path, s string)) {
	switch t := v.(type) {
	case string:
		f(p, t)
	case []any:
		for i, e := range t {
			walkJSONStrings(e, fmt.Sprintf("%s[%d]", p, i), f)
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			np := k
			if p != "" {
				np = p + "." + k
			}
			walkJSONStrings(t[k], np, f)
		}
	}
}

// oddQuotes is true when a text has an odd number of unescaped double quotes,
// the sign of a string cut off in the middle.
func oddQuotes(s string) bool {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
		} else if s[i] == '"' {
			n++
		}
	}
	return n%2 == 1
}
