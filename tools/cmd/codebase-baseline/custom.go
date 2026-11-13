package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

func regexpMust(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// metricDef is one entry of run.json's metrics array.
type metricDef struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Pattern string   `json:"pattern,omitempty"`
	Globs   []string `json:"globs,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
	Path    string   `json:"path,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Better  string   `json:"better,omitempty"`
	Note    string   `json:"note,omitempty"`
	// How says in plain words what the number is, for the report's reader.
	// The scoreboard falls back to a generic phrase when it is empty.
	How string `json:"how,omitempty"`
}

type runFile struct {
	Metrics  []metricDef `json:"metrics"`
	Headline []any       `json:"headline"`
	// BuiltinRows false turns the scoreboard's built-in rows off, so only the
	// declared metrics show. Absent or true keeps them.
	BuiltinRows *bool `json:"builtin_rows,omitempty"`
}

func (r runFile) builtinRows() bool { return r.BuiltinRows == nil || *r.BuiltinRows }

func (r runFile) headlineNames() []string {
	var out []string
	for _, h := range r.Headline {
		switch t := h.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			for _, k := range []string{"name", "metric"} {
				if s, ok := t[k].(string); ok {
					out = append(out, s)
					break
				}
			}
		}
	}
	return out
}

func readRunFile(p string) (runFile, error) {
	var r runFile
	if err := readJSON(p, &r); err != nil {
		return r, err
	}
	for i, m := range r.Metrics {
		if m.Name == "" {
			return r, fmt.Errorf("%s: metrics[%d] has no name", p, i)
		}
		switch m.Kind {
		case "count":
			if m.Pattern == "" {
				return r, fmt.Errorf("%s: metric %q is a count without a pattern", p, m.Name)
			}
		case "snapshot":
			if m.Path == "" {
				return r, fmt.Errorf("%s: metric %q is a snapshot metric without a path", p, m.Name)
			}
		default:
			return r, fmt.Errorf("%s: metric %q has kind %q, want count or snapshot", p, m.Name, m.Kind)
		}
		if m.Better != "" && m.Better != "up" && m.Better != "down" && m.Better != "none" {
			return r, fmt.Errorf("%s: metric %q has better %q, want up, down or none", p, m.Name, m.Better)
		}
	}
	return r, nil
}

type customResult struct {
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Value       *int      `json:"value,omitempty"`
	Unavailable string    `json:"unavailable,omitempty"`
	Definition  metricDef `json:"definition"`
	FilesScan   int       `json:"files_scanned,omitempty"`
	FilesHit    int       `json:"files_with_matches,omitempty"`
	TopFiles    []hitFile `json:"top_files,omitempty"`
	Carried     bool      `json:"carried_through,omitempty"`
}

type hitFile struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// globToRE turns a glob into a regexp over slash paths: ** crosses directories,
// * and ? stay inside one, {a,b} alternates. A glob without a slash matches the
// base name at any depth, as in .gitignore.
func globToRE(g string) *regexp.Regexp {
	g = strings.TrimPrefix(g, "./")
	prefix := "^"
	if !strings.Contains(g, "/") {
		prefix = "^(?:.*/)?"
	}
	var b strings.Builder
	b.WriteString(prefix)
	depth := 0
	for i := 0; i < len(g); i++ {
		switch c := g[i]; {
		case strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			depth++
			b.WriteString("(?:")
		case c == '}' && depth > 0:
			depth--
			b.WriteString(")")
		case c == ',' && depth > 0:
			b.WriteString("|")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`^\b$`)
	}
	return re
}

func matchesAny(res []*regexp.Regexp, p string) bool {
	for _, r := range res {
		if r.MatchString(p) {
			return true
		}
	}
	return false
}

// runCustomMetrics counts, for each count metric, the lines matching its
// pattern in the tracked files that match its globs. read gets a file's text
// from HEAD.
func runCustomMetrics(defs []metricDef, files []string, read func(rel string) (string, bool)) []customResult {
	out := []customResult{}
	for _, m := range defs {
		r := customResult{Name: m.Name, Kind: m.Kind, Definition: m}
		if m.Kind == "snapshot" {
			r.Carried = true
			out = append(out, r)
			continue
		}
		re, err := regexp.Compile(m.Pattern)
		if err != nil {
			r.Unavailable = "bad pattern: " + err.Error()
			out = append(out, r)
			continue
		}
		var inc, exc []*regexp.Regexp
		for _, g := range m.Globs {
			inc = append(inc, globToRE(g))
		}
		for _, g := range m.Exclude {
			exc = append(exc, globToRE(g))
		}
		total := 0
		var hits []hitFile
		for _, f := range files {
			if isVendoredPath(f) || (len(inc) > 0 && !matchesAny(inc, f)) || matchesAny(exc, f) {
				continue
			}
			text, ok := read(f)
			if !ok {
				continue
			}
			r.FilesScan++
			n := 0
			for _, ln := range strings.Split(text, "\n") {
				if re.MatchString(ln) {
					n++
				}
			}
			if n > 0 {
				total += n
				r.FilesHit++
				hits = append(hits, hitFile{f, n})
			}
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].Count != hits[j].Count {
				return hits[i].Count > hits[j].Count
			}
			return hits[i].Path < hits[j].Path
		})
		if len(hits) > 10 {
			hits = hits[:10]
		}
		r.TopFiles = hits
		v := total
		r.Value = &v
		out = append(out, r)
	}
	return out
}

// readerFor reads a tracked file's text at HEAD: from the export when there is
// one, else through git, so untracked files in a working tree never count.
func readerFor(root, repo string, fromExport bool) func(rel string) (string, bool) {
	if fromExport {
		return func(rel string) (string, bool) { return readText(root, rel, 2<<20) }
	}
	return func(rel string) (string, bool) {
		out, err := git(repo, "show", "HEAD:./"+rel)
		if err != nil || len(out) > 2<<20 || strings.IndexByte(out, 0) >= 0 {
			return "", false
		}
		return out, true
	}
}
