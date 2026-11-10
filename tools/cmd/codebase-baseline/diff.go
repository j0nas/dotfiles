package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
)

type metricDelta struct {
	Path      string   `json:"path"`
	Old       *float64 `json:"old,omitempty"`
	New       *float64 `json:"new,omitempty"`
	Delta     float64  `json:"delta"`
	Pct       *float64 `json:"pct,omitempty"`
	Direction string   `json:"direction,omitempty"` // better, worse, or empty when neutral
}

type setDelta struct {
	Path    string   `json:"path"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

type loopChange struct {
	ID      string   `json:"id"`
	Change  string   `json:"change"` // added, removed, status, counts
	Old     string   `json:"old,omitempty"`
	New     string   `json:"new,omitempty"`
	Details []string `json:"details,omitempty"`
}

type authorFile struct {
	Path    string `json:"path"`
	Commits int    `json:"commits"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

type authorSection struct {
	Pattern        string       `json:"pattern"`
	Unavailable    string       `json:"unavailable,omitempty"`
	Range          string       `json:"range,omitempty"`
	CommitsInRange int          `json:"commits_in_range"`
	Commits        int          `json:"matching_commits"`
	Authors        []authorFile `json:"matching_authors,omitempty"`
	FilesTouched   int          `json:"files_touched"`
	Added          int          `json:"lines_added"`
	Deleted        int          `json:"lines_deleted"`
	ShareOfChurn   float64      `json:"share_of_all_churn"`
	TopFiles       []authorFile `json:"top_files,omitempty"`
}

type diffOut struct {
	SchemaVersion int            `json:"schema_version"`
	Tool          toolInfo       `json:"tool"`
	Old           diffSide       `json:"old"`
	New           diffSide       `json:"new"`
	Warnings      []string       `json:"warnings"`
	Headline      []metricDelta  `json:"headline"`
	Metrics       []metricDelta  `json:"metrics"`
	Sets          []setDelta     `json:"sets"`
	Loops         []loopChange   `json:"loops"`
	Authors       *authorSection `json:"authors,omitempty"`
}

type diffSide struct {
	Head       string `json:"head_sha"`
	MeasuredAt string `json:"measured_at"`
	Tool       string `json:"tool_version"`
	Schema     int    `json:"schema_version"`
	Path       string `json:"path,omitempty"`
}

func (a *app) cmdDiff(args []string) error {
	fl := flag.NewFlagSet("diff", flag.ContinueOnError)
	format := fl.String("format", "md", "md or json")
	author := fl.String("author", "", "regex on author name or email: count that author's commits and churn between the two snapshots' SHAs")
	repo := fl.String("repo", "", "repo for --author (default: the path recorded in the new snapshot)")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline diff <old.json> <new.json> [--format md|json] [--author <regex>] [--repo <path>]")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		fl.Usage()
		return fmt.Errorf("diff needs <old.json> and <new.json>")
	}
	if *format != "md" && *format != "json" {
		return fmt.Errorf("--format must be md or json")
	}
	var oldS, newS map[string]any
	if err := readJSON(pos[0], &oldS); err != nil {
		return err
	}
	if err := readJSON(pos[1], &newS); err != nil {
		return err
	}
	res := buildDiff(oldS, newS)
	if *author != "" {
		r := *repo
		if r == "" {
			r, _ = dig(newS, "meta", "path").(string)
		}
		res.Authors = authorSectionFor(*author, r, str(dig(oldS, "meta", "head_sha")), str(dig(newS, "meta", "head_sha")))
	}
	if *format == "json" {
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, string(b))
		return nil
	}
	renderDiffMD(a.stdout, res)
	return nil
}

func dig(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func sideOf(s map[string]any) diffSide {
	d := diffSide{Head: str(dig(s, "meta", "head_sha")), MeasuredAt: str(dig(s, "meta", "measured_at")), Tool: str(dig(s, "tool", "version")), Path: str(dig(s, "meta", "path"))}
	if f, ok := s["schema_version"].(float64); ok {
		d.Schema = int(f)
	}
	return d
}

// flat is a snapshot reduced to dotted numeric paths, string sets and loops.
type flat struct {
	nums  map[string]float64
	sets  map[string]map[string]bool
	loops map[string]map[string]any
}

// skipNumeric names subtrees that are series or rankings, not scalar metrics.
var skipNumeric = []string{"git.commits_by_month", "git.agent_commits_by_month", "git.bot_authored_commits_by_month", "git.churn_top_files", "git.top_authors_last_12m", "meta.", "tool.", "loops."}

func flatten(s map[string]any) flat {
	f := flat{nums: map[string]float64{}, sets: map[string]map[string]bool{}, loops: map[string]map[string]any{}}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, c := range t {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, c)
			}
		case float64:
			for _, sk := range skipNumeric {
				if strings.HasPrefix(prefix, sk) {
					return
				}
			}
			f.nums[prefix] = t
		case bool:
			if strings.HasPrefix(prefix, "loops.") {
				return
			}
			n := 0.0
			if t {
				n = 1
			}
			f.nums[prefix] = n
		case []any:
			if len(t) == 0 {
				f.sets[prefix] = map[string]bool{}
				return
			}
			allStr, allMap := true, true
			for _, e := range t {
				if _, ok := e.(string); !ok {
					allStr = false
				}
				if _, ok := e.(map[string]any); !ok {
					allMap = false
				}
			}
			switch {
			case allStr:
				set := map[string]bool{}
				for _, e := range t {
					set[e.(string)] = true
				}
				f.sets[prefix] = set
			case allMap && (prefix == "deps.manifests" || prefix == "docs.folders" || prefix == "docs.files" || prefix == "custom"):
				key := "path"
				switch prefix {
				case "docs.folders":
					key = "folder"
				case "custom":
					key = "name"
				}
				set := map[string]bool{}
				for _, e := range t {
					m := e.(map[string]any)
					p := str(m[key])
					set[p] = true
					if prefix == "docs.files" {
						continue // per-file word counts are too many to diff; added and removed files are listed
					}
					for k, v := range m {
						if n, ok := v.(float64); ok {
							f.nums[prefix+"["+p+"]."+k] = n
						}
					}
				}
				if prefix != "custom" {
					f.sets[prefix] = set
				}
			}
		}
	}
	for k, v := range s {
		if k == "loops" {
			if m, ok := v.(map[string]any); ok {
				if ls, ok := m["loops"].([]any); ok {
					for _, e := range ls {
						if lm, ok := e.(map[string]any); ok {
							f.loops[str(lm["id"])] = lm
						}
					}
				}
			}
			continue
		}
		walk(k, v)
	}
	return f
}

// customBetter holds the better: up|down|none of each custom metric, by name.
var customBetter = map[string]string{}

var reCustomPath = regexp.MustCompile(`^custom\[(.*)\]\.value$`)

func directionFor(path string, delta float64) string {
	if delta == 0 {
		return ""
	}
	if m := reCustomPath.FindStringSubmatch(path); m != nil {
		switch customBetter[m[1]] {
		case "up":
			if delta > 0 {
				return "better"
			}
			return "worse"
		case "down":
			if delta < 0 {
				return "better"
			}
			return "worse"
		}
		return ""
	}
	// Only metrics whose direction was declared (custom metrics with better: up
	// or down) are judged. Counts that can be gamed, such as test files, TODOs,
	// fix-commit share or lines of code, stay context.
	return ""
}

var headlinePaths = []string{
	"size.totals_code_only.code", "size.totals.code", "size.totals.files", "size.totals_code_only.complexity",
	"git.commits_total", "git.commits_last_12m", "git.contributors_last_12m", "git.top_author_share_last_12m",
	"git.agent_commits_last_12m", "git.fix_commit_share", "git.revert_commits", "git.revert_commits_last_12m", "docs.totals.words", "git.merge_commits",
	"git.median_files_changed_per_commit_12m", "deps.direct_total", "hygiene.test_files.total", "hygiene.todo_markers.total",
}

func buildDiff(oldS, newS map[string]any) diffOut {
	res := diffOut{SchemaVersion: schemaVersion, Tool: currentTool(), Old: sideOf(oldS), New: sideOf(newS), Warnings: []string{}, Headline: []metricDelta{}, Metrics: []metricDelta{}, Sets: []setDelta{}, Loops: []loopChange{}}
	if res.Old.Tool != res.New.Tool {
		res.Warnings = append(res.Warnings, fmt.Sprintf("the snapshots were taken by different tool versions (%q and %q): differences may come from the tool, not the code", res.Old.Tool, res.New.Tool))
	}
	if res.Old.Schema != res.New.Schema {
		res.Warnings = append(res.Warnings, fmt.Sprintf("different schema versions (%d and %d): some metrics will show as added or removed", res.Old.Schema, res.New.Schema))
	}
	if t1, t2 := str(dig(oldS, "tool", "vcs_revision")), str(dig(newS, "tool", "vcs_revision")); t1 != t2 && res.Old.Tool == res.New.Tool {
		res.Warnings = append(res.Warnings, fmt.Sprintf("same tool version but different builds (%s and %s)", shortSha(t1), shortSha(t2)))
	}
	secretKind := func(s map[string]any) string {
		t := str(dig(s, "hygiene", "secret_scan", "tool"))
		if t == "" {
			t = "none (" + str(dig(s, "hygiene", "secret_scan", "unavailable")) + ")"
		}
		return t
	}
	if a, b := secretKind(oldS), secretKind(newS); a != b {
		res.Warnings = append(res.Warnings, fmt.Sprintf("the secret scanner differs between the snapshots (%s and %s): findings are not comparable", a, b))
	}
	if a, b := str(dig(oldS, "size", "tool")), str(dig(newS, "size", "tool")); a != b {
		res.Warnings = append(res.Warnings, fmt.Sprintf("the line counter differs between the snapshots (%q and %q): size figures may shift", a, b))
	}
	if str(dig(oldS, "meta", "tree")) != str(dig(newS, "meta", "tree")) {
		res.Warnings = append(res.Warnings, "the snapshots scanned different trees (HEAD export vs working tree)")
	}
	for _, s := range []map[string]any{oldS, newS} {
		if l, ok := s["custom"].([]any); ok {
			for _, e := range l {
				if em, ok := e.(map[string]any); ok {
					if def, ok := em["definition"].(map[string]any); ok {
						customBetter[str(em["name"])] = str(def["better"])
					}
				}
			}
		}
	}
	o, n := flatten(oldS), flatten(newS)
	paths := map[string]bool{}
	for p := range o.nums {
		paths[p] = true
	}
	for p := range n.nums {
		paths[p] = true
	}
	mk := func(p string) (metricDelta, bool) {
		ov, ook := o.nums[p]
		nv, nok := n.nums[p]
		d := metricDelta{Path: p}
		if ook {
			d.Old = &ov
		}
		if nok {
			d.New = &nv
		}
		if ook && nok && ov == nv {
			return d, false
		}
		d.Delta = round2(nv - ov)
		if ook && nok && ov != 0 {
			pct := round2((nv - ov) / math.Abs(ov) * 100)
			d.Pct = &pct
		}
		d.Direction = directionFor(p, nv-ov)
		return d, true
	}
	isHead := map[string]bool{}
	for _, p := range headlinePaths {
		isHead[p] = true
		if _, ok := paths[p]; ok {
			d, _ := mk(p)
			res.Headline = append(res.Headline, d)
		}
	}
	for _, p := range sortedKeys(paths) {
		if isHead[p] {
			continue
		}
		if d, changed := mk(p); changed {
			res.Metrics = append(res.Metrics, d)
		}
	}
	setPaths := map[string]bool{}
	for p := range o.sets {
		setPaths[p] = true
	}
	for p := range n.sets {
		setPaths[p] = true
	}
	for _, p := range sortedKeys(setPaths) {
		sd := setDelta{Path: p}
		for k := range n.sets[p] {
			if !o.sets[p][k] {
				sd.Added = append(sd.Added, k)
			}
		}
		for k := range o.sets[p] {
			if !n.sets[p][k] {
				sd.Removed = append(sd.Removed, k)
			}
		}
		sort.Strings(sd.Added)
		sort.Strings(sd.Removed)
		if len(sd.Added)+len(sd.Removed) > 0 {
			res.Sets = append(res.Sets, sd)
		}
	}
	res.Loops = diffLoops(o.loops, n.loops)
	return res
}

func numOf(m map[string]any, k string) (float64, bool) {
	f, ok := m[k].(float64)
	return f, ok
}

func diffLoops(o, n map[string]map[string]any) []loopChange {
	ids := map[string]bool{}
	for id := range o {
		ids[id] = true
	}
	for id := range n {
		ids[id] = true
	}
	var out []loopChange
	for _, id := range sortedKeys(ids) {
		ol, nl := o[id], n[id]
		switch {
		case ol == nil:
			out = append(out, loopChange{ID: id, Change: "added", New: str(nl["status"])})
			continue
		case nl == nil:
			out = append(out, loopChange{ID: id, Change: "removed", Old: str(ol["status"])})
			continue
		}
		var details []string
		op, _ := ol["parsed"].(map[string]any)
		np, _ := nl["parsed"].(map[string]any)
		for _, k := range []string{"passed", "failed", "skipped", "total", "errors", "warnings", "warnings_and_errors", "packages_failed", "critical", "high", "moderate", "low"} {
			ov, ok1 := numOf(op, k)
			nv, ok2 := numOf(np, k)
			if ok1 && ok2 && ov != nv {
				dir := ""
				switch k {
				case "failed", "errors", "warnings", "warnings_and_errors", "packages_failed":
					dir = " (worse)"
					if nv < ov {
						dir = " (better)"
					}
				}
				details = append(details, fmt.Sprintf("%s %v -> %v%s", k, ov, nv, dir))
			} else if ok1 != ok2 {
				details = append(details, fmt.Sprintf("%s appears in only one of them", k))
			}
		}
		of, _ := ol["flaky"].(bool)
		nf, _ := nl["flaky"].(bool)
		if of != nf {
			details = append(details, fmt.Sprintf("flaky %v -> %v", of, nf))
		}
		os, ns := str(ol["status"]), str(nl["status"])
		switch {
		case os != ns:
			out = append(out, loopChange{ID: id, Change: "status", Old: os, New: ns, Details: details})
		case len(details) > 0:
			out = append(out, loopChange{ID: id, Change: "counts", Old: os, New: ns, Details: details})
		}
	}
	return out
}

func authorSectionFor(pattern, repo, oldSha, newSha string) *authorSection {
	s := &authorSection{Pattern: pattern}
	re, err := regexp.Compile("(?i)^(?:" + pattern + ")$")
	if err != nil {
		s.Unavailable = "bad --author regex: " + err.Error()
		return s
	}
	if repo == "" || !hasGit(repo) || oldSha == "" || newSha == "" {
		s.Unavailable = "needs a git checkout (--repo or meta.path in the new snapshot) and both snapshot SHAs"
		return s
	}
	s.Range = shortSha(oldSha) + ".." + shortSha(newSha)
	out, err := git(repo, scoped(repo, "log", "--no-merges", "--no-renames", "--numstat", "--relative", "--format=%x1e%H%x1f%aN%x1f%aE%x1f%aI%x1f", oldSha+".."+newSha)...)
	if err != nil {
		s.Unavailable = err.Error()
		return s
	}
	authors := map[string]*authorFile{}
	files := map[string]*authorFile{}
	totalChurn, matchChurn := 0, 0
	for _, rec := range strings.Split(out, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x1f", 5)
		if len(parts) < 5 {
			continue
		}
		s.CommitsInRange++
		match := re.MatchString(parts[1]) || re.MatchString(parts[2])
		var churn int
		for _, ln := range strings.Split(parts[4], "\n") {
			p := strings.SplitN(ln, "\t", 3)
			if len(p) != 3 {
				continue
			}
			if isLockfilePath(p[2]) {
				continue
			}
			ad, de := atoi(p[0]), atoi(p[1])
			churn += ad + de
			if match {
				f := files[p[2]]
				if f == nil {
					f = &authorFile{Path: p[2]}
					files[p[2]] = f
				}
				f.Commits++
				f.Added += ad
				f.Deleted += de
				s.Added += ad
				s.Deleted += de
			}
		}
		totalChurn += churn
		if match {
			s.Commits++
			matchChurn += churn
			a := authors[parts[1]]
			if a == nil {
				a = &authorFile{Path: parts[1]}
				authors[parts[1]] = a
			}
			a.Commits++
		}
	}
	for _, a := range authors {
		s.Authors = append(s.Authors, *a)
	}
	sort.Slice(s.Authors, func(i, j int) bool { return s.Authors[i].Commits > s.Authors[j].Commits })
	for _, f := range files {
		s.TopFiles = append(s.TopFiles, *f)
	}
	s.FilesTouched = len(files)
	sort.Slice(s.TopFiles, func(i, j int) bool {
		a, b := s.TopFiles[i], s.TopFiles[j]
		if a.Added+a.Deleted != b.Added+b.Deleted {
			return a.Added+a.Deleted > b.Added+b.Deleted
		}
		return a.Path < b.Path
	})
	if len(s.TopFiles) > 15 {
		s.TopFiles = s.TopFiles[:15]
	}
	if totalChurn > 0 {
		s.ShareOfChurn = round2(float64(matchChurn) / float64(totalChurn))
	}
	return s
}

func fmtNum(p *float64) string {
	if p == nil {
		return "-"
	}
	if *p == math.Trunc(*p) {
		return fmt.Sprintf("%d", int64(*p))
	}
	return fmt.Sprintf("%.2f", *p)
}

func deltaRow(d metricDelta) string {
	pct := ""
	if d.Pct != nil {
		pct = fmt.Sprintf("%+.1f%%", *d.Pct)
	}
	sign := ""
	if d.Delta > 0 {
		sign = "+"
	}
	return fmt.Sprintf("| `%s` | %s | %s | %s%s | %s | %s |", d.Path, fmtNum(d.Old), fmtNum(d.New), sign, fmtNum(&d.Delta), pct, d.Direction)
}

type mdWriter interface{ Write(p []byte) (int, error) }

func renderDiffMD(w mdWriter, r diffOut) {
	p := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	p("# Snapshot diff")
	p("")
	p("- old: `%s` measured %s (tool %s)", shortSha(r.Old.Head), r.Old.MeasuredAt, r.Old.Tool)
	p("- new: `%s` measured %s (tool %s)", shortSha(r.New.Head), r.New.MeasuredAt, r.New.Tool)
	for _, wmsg := range r.Warnings {
		p("- WARNING: %s", wmsg)
	}
	p("")
	table := func(title string, ds []metricDelta) {
		if len(ds) == 0 {
			return
		}
		p("## %s", title)
		p("")
		p("| metric | old | new | change | %% | |")
		p("|---|---:|---:|---:|---:|---|")
		for _, d := range ds {
			p("%s", deltaRow(d))
		}
		p("")
	}
	table("Headline metrics", r.Headline)
	table("Other changed metrics", r.Metrics)
	if len(r.Loops) > 0 {
		p("## Feedback loops")
		p("")
		for _, l := range r.Loops {
			switch l.Change {
			case "added":
				p("- `%s`: new loop (%s)", l.ID, l.New)
			case "removed":
				p("- `%s`: no longer measured (was %s)", l.ID, l.Old)
			case "status":
				p("- `%s`: %s -> %s %s", l.ID, l.Old, l.New, strings.Join(l.Details, "; "))
			default:
				p("- `%s`: %s", l.ID, strings.Join(l.Details, "; "))
			}
		}
		p("")
	}
	if len(r.Sets) > 0 {
		p("## New and removed items")
		p("")
		for _, s := range r.Sets {
			if len(s.Added) > 0 {
				p("- `%s` added: %s", s.Path, strings.Join(s.Added, ", "))
			}
			if len(s.Removed) > 0 {
				p("- `%s` removed: %s", s.Path, strings.Join(s.Removed, ", "))
			}
		}
		p("")
	}
	if a := r.Authors; a != nil {
		p("## Contributions matching `%s`", a.Pattern)
		p("")
		if a.Unavailable != "" {
			p("Unavailable: %s", a.Unavailable)
			return
		}
		p("Range %s: %d of %d non-merge commits match; %d files touched, +%d -%d lines (%.0f%% of all churn in the range).", a.Range, a.Commits, a.CommitsInRange, a.FilesTouched, a.Added, a.Deleted, a.ShareOfChurn*100)
		p("")
		for _, au := range a.Authors {
			p("- %s: %d commits", au.Path, au.Commits)
		}
		if len(a.TopFiles) > 0 {
			p("")
			p("| file | commits | added | deleted |")
			p("|---|---:|---:|---:|")
			for _, f := range a.TopFiles {
				p("| `%s` | %d | %d | %d |", f.Path, f.Commits, f.Added, f.Deleted)
			}
		}
	}
}

// isLockfilePath is true for lockfiles, whose churn says nothing about authorship.
func isLockfilePath(p string) bool {
	b := path.Base(p)
	return lockfileNames[b] || strings.HasSuffix(b, ".lock") || strings.HasSuffix(b, "-lock.json") || strings.HasSuffix(b, "-lock.yaml")
}
