package main

import (
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

type docFile struct {
	Path        string `json:"path"`
	Words       int    `json:"words"`
	Lines       int    `json:"lines"`
	LastTouched string `json:"last_touched,omitempty"`
	Kind        string `json:"kind"`
}

type docFolder struct {
	Folder string `json:"folder"`
	Files  int    `json:"files"`
	Words  int    `json:"words"`
	Lines  int    `json:"lines"`
}

type docKindTotal struct {
	Files int `json:"files"`
	Words int `json:"words"`
}

type docsSection struct {
	Totals      docKindTotal            `json:"totals"`
	ByKind      map[string]docKindTotal `json:"by_kind"`
	Folders     []docFolder             `json:"folders"`
	Files       []docFile               `json:"files"`
	AgentFiles  []string                `json:"agent_instruction_files"`
	Note        string                  `json:"note"`
	Unavailable string                  `json:"unavailable,omitempty"`
}

var reDecisionName = regexpMust(`(?i)^(adr|decision|rfc)[-_ ]?\d+|^\d{3,4}[-_].+`)

// docKind guesses what a doc file is for.
func docKind(rel string, agentDirOrFile bool) string {
	base := path.Base(rel)
	segs := strings.Split(rel, "/")
	switch {
	case agentDirOrFile:
		return "agent-instructions"
	case strings.HasPrefix(strings.ToUpper(base), "README"):
		return "readme"
	}
	for _, s := range segs[:len(segs)-1] {
		switch strings.ToLower(s) {
		case "adr", "adrs", "decisions", "rfcs":
			return "decision"
		}
	}
	if reDecisionName.MatchString(base) && strings.Contains(strings.ToLower(rel), "decision") {
		return "decision"
	}
	if strings.Contains(strings.ToLower(base), "decision") || strings.HasPrefix(strings.ToLower(base), "adr-") {
		return "decision"
	}
	return "other"
}

func isDocExt(base string) bool {
	ext := strings.ToLower(path.Ext(base))
	return docExts[ext] || ext == ".txt"
}

// countWords counts whitespace-separated words, as wc -w does, and lines.
func countWords(text string) (words, lines int) {
	words = len(strings.Fields(text))
	lines = strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		lines++
	}
	return
}

// scanDocs finds the prose docs and agent-instruction files among the tracked
// files, using the same rules as export --strip-docs, and sizes them.
// read returns a file's text; lastTouched maps a path to its last commit date.
func scanDocs(files []string, read func(rel string) (string, bool), lastTouched func(paths []string) map[string]string) docsSection {
	d := docsSection{ByKind: map[string]docKindTotal{}, Files: []docFile{}, Folders: []docFolder{}, AgentFiles: []string{},
		Note: "words counts whitespace-separated tokens (wc -w), code blocks included; tracked files at HEAD only; node_modules, vendor, .venv, dist and build are skipped; same file rules as export --strip-docs"}
	var paths []string
	agent := map[string]bool{}
	for _, f := range files {
		segs := strings.Split(f, "/")
		skip, inDocDir, inAgentDir := isVendoredPath(f), false, false
		for i := range segs[:len(segs)-1] {
			switch stripReason(strings.Join(segs[:i+1], "/"), true) {
			case "doc directory":
				inDocDir = true
			case "agent directory":
				inAgentDir = true
			}
		}
		if skip {
			continue
		}
		reason := stripReason(f, false)
		if reason == "" && !((inDocDir || inAgentDir) && isDocExt(segs[len(segs)-1])) {
			continue
		}
		isAgent := reason == "agent file" || inAgentDir
		paths = append(paths, f)
		agent[f] = isAgent
	}
	sort.Strings(paths)
	dates := lastTouched(paths)
	folders := map[string]*docFolder{}
	for _, p := range paths {
		text, ok := read(p)
		if !ok || !utf8.ValidString(text) {
			continue
		}
		w, l := countWords(text)
		f := docFile{Path: p, Words: w, Lines: l, LastTouched: dates[p], Kind: docKind(p, agent[p])}
		d.Files = append(d.Files, f)
		d.Totals.Files++
		d.Totals.Words += w
		k := d.ByKind[f.Kind]
		k.Files++
		k.Words += w
		d.ByKind[f.Kind] = k
		if agent[p] {
			d.AgentFiles = append(d.AgentFiles, p)
		}
		top := "(root)"
		if i := strings.IndexByte(p, '/'); i > 0 {
			top = p[:i]
		}
		fo := folders[top]
		if fo == nil {
			fo = &docFolder{Folder: top}
			folders[top] = fo
		}
		fo.Files++
		fo.Words += w
		fo.Lines += l
	}
	for _, k := range sortedKeys(folders) {
		d.Folders = append(d.Folders, *folders[k])
	}
	sort.Slice(d.Folders, func(i, j int) bool { return d.Folders[i].Words > d.Folders[j].Words })
	return d
}

// gitLastTouched returns the committer date (YYYY-MM-DD) of the last commit
// that touched each path. It asks git per file for a few docs and walks the
// history once for many.
func gitLastTouched(repo, rev string) func(paths []string) map[string]string {
	return func(paths []string) map[string]string {
		out := map[string]string{}
		if !hasGit(repo) || len(paths) == 0 {
			return out
		}
		if len(paths) <= 300 {
			for _, p := range paths {
				if s, err := git(repo, "log", "-1", "--format=%cs", rev, "--", p); err == nil {
					if s = strings.TrimSpace(s); s != "" {
						out[p] = s
					}
				}
			}
			return out
		}
		want := map[string]bool{}
		for _, p := range paths {
			want[p] = true
		}
		log, err := git(repo, scoped(repo, "log", rev, "--name-only", "--relative", "--format=%x1e%cs", "-n", "100000")...)
		if err != nil {
			return out
		}
		for _, rec := range strings.Split(log, "\x1e") {
			date, rest, _ := strings.Cut(strings.TrimLeft(rec, "\n"), "\n")
			for _, f := range strings.Split(rest, "\n") {
				if want[f] && out[f] == "" {
					out[f] = strings.TrimSpace(date)
				}
			}
		}
		return out
	}
}
