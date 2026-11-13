package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type sbRow struct {
	Metric   string    `json:"metric"`
	Value    any       `json:"value"`
	First    any       `json:"first"`
	Better   string    `json:"better"`
	Trend    []float64 `json:"trend"`
	How      string    `json:"how"`
	Source   string    `json:"source,omitempty"`
	AsOf     string    `json:"asOf"`
	Unit     string    `json:"unit,omitempty"`
	Headline bool      `json:"headline"`
}

type sbSnapshot struct {
	File       string `json:"file"`
	Head       string `json:"head_sha"`
	MeasuredAt string `json:"measured_at"`
	data       map[string]any
}

type sbOut struct {
	SchemaVersion int          `json:"schema_version"`
	Tool          toolInfo     `json:"tool"`
	Note          string       `json:"note"`
	Rows          []sbRow      `json:"rows"`
	Snapshots     []sbSnapshot `json:"snapshots"`
	Skipped       []string     `json:"skipped_files,omitempty"`
}

// sbVal is a metric's reading in one snapshot: what to show and, when there is
// one, the number to plot.
type sbVal struct {
	Show   any
	Num    float64
	HasNum bool
}

type sbMetric struct {
	Name, Unit, Better, How, Source string
	Get                             func(snap map[string]any) (sbVal, bool)
}

func (a *app) cmdScoreboard(args []string) error {
	fl := flag.NewFlagSet("scoreboard", flag.ContinueOnError)
	runPath := fl.String("run", "", "run.json with metrics and headline names (default metric set when it has no metrics)")
	dir := fl.String("snapshots", "", "directory of snapshot JSON files (required)")
	out := fl.String("out", "", "scoreboard.json to write (required)")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline scoreboard --run run.json --snapshots <dir> --out scoreboard.json")
		fl.PrintDefaults()
	}
	if _, err := parseFlags(fl, args, a.stderr); err != nil {
		return err
	}
	if *dir == "" || *out == "" {
		fl.Usage()
		return fmt.Errorf("scoreboard needs --snapshots and --out")
	}
	var run runFile
	if *runPath != "" {
		var err error
		if run, err = readRunFile(*runPath); err != nil {
			return err
		}
	}
	res, err := buildScoreboard(run, *dir)
	if err != nil {
		return err
	}
	if err := writeJSON(*out, res); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s: %d rows from %d snapshots\n", *out, len(res.Rows), len(res.Snapshots))
	for _, r := range res.Rows {
		h := " "
		if r.Headline {
			h = "*"
		}
		fmt.Fprintf(a.stdout, " %s %-44s %v (first %v, %d points)\n", h, r.Metric, r.Value, r.First, len(r.Trend))
	}
	return nil
}

func loadSnapshots(dir string) ([]sbSnapshot, []string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	var snaps []sbSnapshot
	var skipped []string
	for _, p := range paths {
		var m map[string]any
		if err := readJSON(p, &m); err != nil {
			skipped = append(skipped, filepath.Base(p)+": not JSON")
			continue
		}
		at := str(dig(m, "meta", "measured_at"))
		if at == "" {
			skipped = append(skipped, filepath.Base(p)+": no meta.measured_at, so not a snapshot")
			continue
		}
		snaps = append(snaps, sbSnapshot{File: filepath.Base(p), Head: str(dig(m, "meta", "head_sha")), MeasuredAt: at, data: m})
	}
	sort.SliceStable(snaps, func(i, j int) bool {
		if snaps[i].MeasuredAt != snaps[j].MeasuredAt {
			return snaps[i].MeasuredAt < snaps[j].MeasuredAt
		}
		return snaps[i].File < snaps[j].File
	})
	snaps, superseded := newestPerCommit(snaps)
	skipped = append(skipped, superseded...)
	return snaps, skipped, nil
}

// newestPerCommit keeps the newest snapshot of each commit (head_sha) and drops
// the older ones, so a snapshot retaken after run.json gained metrics replaces
// the first one without anyone moving files. snaps must be sorted oldest
// first; the order is kept. A snapshot without a head_sha is kept as it is.
// The returned strings say which file each dropped one lost to.
func newestPerCommit(snaps []sbSnapshot) ([]sbSnapshot, []string) {
	newest := map[string]string{}
	for _, s := range snaps {
		if s.Head != "" {
			newest[s.Head] = s.File // later entries are newer
		}
	}
	var kept []sbSnapshot
	var dropped []string
	for _, s := range snaps {
		if s.Head != "" && newest[s.Head] != s.File {
			dropped = append(dropped, fmt.Sprintf("%s: superseded by %s, a newer snapshot of the same commit", s.File, newest[s.Head]))
			continue
		}
		kept = append(kept, s)
	}
	return kept, dropped
}

func buildScoreboard(run runFile, dir string) (sbOut, error) {
	if _, err := os.Stat(dir); err != nil {
		return sbOut{}, err
	}
	snaps, skipped, err := loadSnapshots(dir)
	if err != nil {
		return sbOut{}, err
	}
	res := sbOut{SchemaVersion: schemaVersion, Tool: currentTool(), Rows: []sbRow{}, Snapshots: []sbSnapshot{}, Skipped: skipped}
	for _, s := range snaps {
		res.Snapshots = append(res.Snapshots, sbSnapshot{File: s.File, Head: s.Head, MeasuredAt: s.MeasuredAt})
	}
	var declared []sbMetric
	loops, _, repeat := collectLoopInfo(snaps)
	for _, d := range run.Metrics {
		if d.Kind == "snapshot" && d.How == "" {
			d.How = loopPathHow(d.Path, loops, repeat) // else metricFromDef's generic wording
		}
		declared = append(declared, metricFromDef(d))
	}
	metrics := mergeMetrics(defaultMetricsFor(run, snaps), declared)
	asOf := ""
	if len(snaps) > 0 {
		last := snaps[len(snaps)-1]
		asOf = str(dig(last.data, "meta", "as_of"))
		if asOf == "" && len(last.MeasuredAt) >= 10 {
			asOf = last.MeasuredAt[:10]
		}
	}
	for _, m := range metrics {
		row := sbRow{Metric: m.Name, Better: m.Better, How: m.How, Source: m.Source, AsOf: asOf, Unit: m.Unit, Trend: []float64{}}
		if row.Better == "" {
			row.Better = "none"
		}
		var firstShow any
		haveFirst := false
		for i, s := range snaps {
			v, ok := m.Get(s.data)
			if ok {
				if v.HasNum {
					row.Trend = append(row.Trend, v.Num)
				}
				if !haveFirst {
					firstShow, haveFirst = v.Show, true
				}
				if i == len(snaps)-1 {
					row.Value = v.Show
				}
			}
		}
		row.First = firstShow
		res.Rows = append(res.Rows, row)
	}
	// Headline metrics lead, in the order run.json names them.
	var lead, rest []sbRow
	used := map[int]bool{}
	for _, name := range run.headlineNames() {
		for i, r := range res.Rows {
			if !used[i] && strings.EqualFold(r.Metric, name) {
				r.Headline = true
				lead = append(lead, r)
				used[i] = true
				break
			}
		}
	}
	for i, r := range res.Rows {
		if !used[i] {
			rest = append(rest, r)
		}
	}
	res.Rows = append(lead, rest...)
	superseded := 0
	for _, sk := range skipped {
		if strings.Contains(sk, ": superseded by ") {
			superseded++
		}
	}
	switch len(snaps) {
	case 0:
		res.Note = "No snapshots found; every value is null."
	default:
		res.Note = fmt.Sprintf("%d snapshots, %s to %s. Every value is read from a snapshot field or recounted by codebase-baseline measure; first is the oldest snapshot that has the metric, trend skips snapshots that lack it.",
			len(snaps), snaps[0].MeasuredAt[:min(10, len(snaps[0].MeasuredAt))], snaps[len(snaps)-1].MeasuredAt[:min(10, len(snaps[len(snaps)-1].MeasuredAt))])
	}
	if superseded > 0 {
		res.Note += fmt.Sprintf(" Only the newest snapshot of each commit is used: %d older snapshot%s of the same commit skipped (listed in skipped_files).", superseded, plural(superseded))
	}
	return res, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// defaultMetricsFor is the built-in row set unless run.json turns it off.
func defaultMetricsFor(run runFile, snaps []sbSnapshot) []sbMetric {
	if !run.builtinRows() {
		return nil
	}
	return defaultMetrics(snaps)
}

// mergeMetrics puts the declared metrics after the built-in ones. A declared
// metric with the name of a built-in (case-insensitive) takes its place.
func mergeMetrics(builtin, declared []sbMetric) []sbMetric {
	out := append([]sbMetric(nil), builtin...)
	at := map[string]int{}
	for i, m := range out {
		at[strings.ToLower(m.Name)] = i
	}
	for _, d := range declared {
		if i, ok := at[strings.ToLower(d.Name)]; ok && i < len(builtin) {
			out[i] = d
			continue
		}
		at[strings.ToLower(d.Name)] = len(out)
		out = append(out, d)
	}
	return out
}

var reSelector = regexp.MustCompile(`^([^\[\]]*)((?:\[[^\]]*\])*)$`)

// getPath reads a dotted path from a snapshot. A segment may select a list
// entry: name[id=js-test] picks the entry whose id is js-test, name[2] the third.
func getPath(m map[string]any, path string) (any, bool) {
	var cur any = m
	for _, seg := range strings.Split(path, ".") {
		sm := reSelector.FindStringSubmatch(seg)
		if sm == nil {
			return nil, false
		}
		if sm[1] != "" {
			mm, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			if cur, ok = mm[sm[1]]; !ok {
				return nil, false
			}
		}
		for _, sel := range regexp.MustCompile(`\[([^\]]*)\]`).FindAllStringSubmatch(sm[2], -1) {
			list, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			found := false
			if k, v, isKV := strings.Cut(sel[1], "="); isKV {
				for _, e := range list {
					if em, ok := e.(map[string]any); ok && fmt.Sprint(em[k]) == v {
						cur, found = e, true
						break
					}
				}
			} else if i, err := strconv.Atoi(sel[1]); err == nil && i >= 0 && i < len(list) {
				cur, found = list[i], true
			}
			if !found {
				return nil, false
			}
		}
	}
	return cur, cur != nil
}

func toVal(v any) (sbVal, bool) {
	switch t := v.(type) {
	case float64:
		return sbVal{Show: t, Num: t, HasNum: true}, true
	case bool:
		n := 0.0
		s := "no"
		if t {
			n, s = 1, "yes"
		}
		return sbVal{Show: s, Num: n, HasNum: true}, true
	case string:
		return sbVal{Show: t}, true
	}
	return sbVal{}, false
}

func metricFromDef(d metricDef) sbMetric {
	m := sbMetric{Name: d.Name, Unit: d.Unit, Better: d.Better, How: d.How}
	if d.Kind == "snapshot" {
		if m.How == "" {
			m.How = "value recorded in each snapshot"
		}
		m.Source = "snapshot field " + d.Path
		m.Get = func(s map[string]any) (sbVal, bool) {
			v, ok := getPath(s, d.Path)
			if !ok {
				return sbVal{}, false
			}
			return toVal(v)
		}
		return m
	}
	if m.How == "" {
		m.How = "lines matching the pattern in " + globWords(d.Globs)
		if len(d.Exclude) > 0 {
			m.How += ", excluding " + strings.Join(d.Exclude, ", ")
		}
	}
	m.Source = "lines matching /" + d.Pattern + "/"
	if len(d.Globs) > 0 {
		m.Source += " in " + strings.Join(d.Globs, ", ")
	}
	if len(d.Exclude) > 0 {
		m.Source += ", excluding " + strings.Join(d.Exclude, ", ")
	}
	m.Source += " (files tracked at the snapshot's commit; recount with measure --metrics run.json)"
	m.Get = func(s map[string]any) (sbVal, bool) {
		list, _ := s["custom"].([]any)
		for _, e := range list {
			if em, ok := e.(map[string]any); ok && em["name"] == d.Name {
				return toVal(em["value"])
			}
		}
		return sbVal{}, false
	}
	return m
}

func globWords(globs []string) string {
	if len(globs) == 0 {
		return "every tracked file"
	}
	return strings.Join(globs, ", ")
}

// pathMetric reads one snapshot field. how is what the number is in plain
// words; the field path goes to the row's source.
func pathMetric(name, how, unit, better, path string) sbMetric {
	return metricFromDef(metricDef{Name: name, Kind: "snapshot", Path: path, Unit: unit, Better: better, How: how})
}

func sumPath(snap map[string]any, paths ...string) (float64, bool) {
	total, found := 0.0, false
	for _, p := range paths {
		if v, ok := getPath(snap, p); ok {
			if f, ok := v.(float64); ok {
				total += f
				found = true
			}
		}
	}
	return total, found
}

// loopWords names a loop for a reader: its command, and the folder when it is
// not the root.
func loopWords(cmd, cwd string) string {
	w := "`" + cmd + "`"
	if cwd != "" && cwd != "." {
		w += " in " + cwd
	}
	return w
}

// timesWords says how often a test loop ran: "" for once, ", run twice", ", run 4 times".
func timesWords(n int) string {
	switch {
	case n <= 1:
		return ""
	case n == 2:
		return ", run twice"
	}
	return fmt.Sprintf(", run %d times", n)
}

type loopInfo struct{ kind, cmd, cwd string }

// collectLoopInfo reads the loops the snapshots hold (the latest entry of each
// id wins), in first-seen order, and how often test loops were run.
func collectLoopInfo(snaps []sbSnapshot) (map[string]loopInfo, []string, int) {
	loops := map[string]loopInfo{}
	var order []string
	repeat := 1
	for _, s := range snaps {
		if lm, ok := dig(s.data, "loops").(map[string]any); ok {
			if n, ok := lm["repeat_tests"].(float64); ok && int(n) > 0 {
				repeat = int(n)
			}
			if ls, ok := lm["loops"].([]any); ok {
				for _, e := range ls {
					if em, ok := e.(map[string]any); ok {
						id := str(em["id"])
						if _, seen := loops[id]; !seen {
							order = append(order, id)
						}
						loops[id] = loopInfo{str(em["kind"]), str(em["cmd"]), str(em["cwd"])}
					}
				}
			}
		}
	}
	return loops, order, repeat
}

var reLoopPath = regexp.MustCompile(`^loops\.loops\[id=([^\]]+)\]\.(.+)$`)

// loopPathHow words a snapshot path into one loop's result ("errors reported
// by `tsc --noEmit` in apps/web"), or "" when the path is something else.
func loopPathHow(path string, loops map[string]loopInfo, repeat int) string {
	m := reLoopPath.FindStringSubmatch(path)
	if m == nil {
		return ""
	}
	l, ok := loops[m[1]]
	if !ok {
		return ""
	}
	w := loopWords(l.cmd, l.cwd)
	switch m[2] {
	case "parsed.passed":
		return "passed tests in " + w + timesWords(repeat)
	case "parsed.failed":
		return "failed tests in " + w + timesWords(repeat)
	case "parsed.skipped":
		return "skipped tests in " + w + timesWords(repeat)
	case "parsed.total":
		return "tests counted in " + w + timesWords(repeat)
	case "parsed.errors":
		return "errors reported by " + w
	case "parsed.warnings":
		return "warnings reported by " + w
	case "duration_s":
		return "seconds one run of " + w + " takes"
	case "status":
		return "whether " + w + " succeeds"
	}
	return ""
}

// defaultMetrics is the built-in scoreboard: loop results for the loops the
// snapshots contain, then size, dependencies, tests, TODOs, ownership and CI.
func defaultMetrics(snaps []sbSnapshot) []sbMetric {
	loops, order, repeat := collectLoopInfo(snaps)
	var ms []sbMetric
	loopPath := func(id, field string) string { return "loops.loops[id=" + id + "]." + field }
	loopMetric := func(name, how, unit, better, id, field string) sbMetric {
		return pathMetric(name+" ("+id+")", how, unit, better, loopPath(id, field))
	}
	flakyIDs := []string{}
	for _, id := range order {
		l := loops[id]
		w := loopWords(l.cmd, l.cwd)
		switch l.kind {
		case "test":
			ms = append(ms, loopMetric("Tests passed", "passed tests in "+w+timesWords(repeat), "tests", "none", id, "parsed.passed"),
				loopMetric("Tests failed", "failed tests in "+w+timesWords(repeat), "tests", "down", id, "parsed.failed"))
			flakyIDs = append(flakyIDs, id)
		case "typecheck":
			ms = append(ms, loopMetric("Type errors", "type errors reported by "+w, "errors", "down", id, "parsed.errors"))
		case "lint":
			ms = append(ms, loopMetric("Lint warnings", "warnings reported by "+w, "warnings", "down", id, "parsed.warnings"),
				loopMetric("Lint errors", "errors reported by "+w, "errors", "down", id, "parsed.errors"))
		case "build":
			id := id
			m := sbMetric{Name: "Build (" + id + ")", Better: "none", How: "whether " + w + " succeeds", Source: "snapshot field " + loopPath(id, "status")}
			m.Get = func(s map[string]any) (sbVal, bool) {
				v, ok := getPath(s, loopPath(id, "status"))
				st, _ := v.(string)
				if !ok || st == "" || st == "disabled" || st == "not_selected" || st == "skipped" || st == "duplicate" {
					return sbVal{}, false
				}
				if st == "passed" {
					return sbVal{Show: "passes", Num: 1, HasNum: true}, true
				}
				return sbVal{Show: "fails", Num: 0, HasNum: true}, true
			}
			ms = append(ms, m)
		case "audit":
			id := id
			m := sbMetric{Name: "Advisories, critical and high (" + id + ")", Unit: "advisories", Better: "down", How: "critical and high advisories reported by " + w,
				Source: "snapshot fields " + loopPath(id, "parsed.critical") + " + " + loopPath(id, "parsed.high")}
			m.Get = func(s map[string]any) (sbVal, bool) {
				n, ok := sumPath(s, loopPath(id, "parsed.critical"), loopPath(id, "parsed.high"))
				if !ok {
					return sbVal{}, false
				}
				return sbVal{Show: n, Num: n, HasNum: true}, true
			}
			ms = append(ms, m, loopMetric("Advisories, all severities", "advisories of every severity reported by "+w, "advisories", "down", id, "parsed.total"))
		}
	}
	if len(flakyIDs) > 0 {
		fm := sbMetric{Name: "Flaky test loops", Unit: "loops", Better: "down", How: "test loops whose counts differ between runs of the same loop",
			Source: "snapshot fields loops.loops[kind=test].flaky"}
		fm.Get = func(s map[string]any) (sbVal, bool) {
			ls, ok := dig(s, "loops", "loops").([]any)
			if !ok {
				return sbVal{}, false
			}
			n, tested := 0.0, false
			for _, e := range ls {
				if em, ok := e.(map[string]any); ok && em["kind"] == "test" {
					if f, has := em["flaky"].(bool); has {
						tested = true
						if f {
							n++
						}
					}
				}
			}
			return sbVal{Show: n, Num: n, HasNum: true}, tested
		}
		ms = append(ms, fm)
	}
	ms = append(ms,
		pathMetric("Code lines (excluding docs and data)", "lines of code, leaving out docs, data, generated and vendored files", "lines", "none", "size.totals_code_only.code"),
		pathMetric("Direct dependencies", "dependencies the manifests declare directly", "dependencies", "none", "deps.direct_total"),
		pathMetric("Test files", "files recognised as tests by name or folder", "files", "none", "hygiene.test_files.total"),
		pathMetric("TODO, FIXME, HACK and XXX markers", "lines with a TODO, FIXME, HACK or XXX marker", "markers", "none", "hygiene.todo_markers.total"),
		pathMetric("Top author's share of commits, last 12 months", "share of the last 12 months' commits (bots left out) made by the busiest author", "of commits", "none", "git.top_author_share_last_12m"),
		pathMetric("Authors covering 80% of commits, last 12 months", "fewest authors whose commits make up 80% of the last 12 months", "authors", "none", "git.authors_for_80pct_last_12m"),
		pathMetric("Revert commits, last 12 months", "commits that revert another commit, last 12 months", "commits", "none", "git.revert_commits_last_12m"),
	)
	agent := sbMetric{Name: "Commits made with an agent, last 3 months (lower bound)", Unit: "of commits", Better: "none",
		How:    "share of non-merge commits in the last 3 months made with a coding agent, seen from its session or co-author trailer; agents that leave no trailer are not counted",
		Source: "git.agent_commits_by_month over git.non_merge_commits_by_month (git.commits_by_month in older snapshots), latest three months of the snapshot"}
	agent.Get = func(s map[string]any) (sbVal, bool) {
		all, ok1 := dig(s, "git", "non_merge_commits_by_month").(map[string]any)
		if !ok1 {
			all, ok1 = dig(s, "git", "commits_by_month").(map[string]any)
		}
		ag, ok2 := dig(s, "git", "agent_commits_by_month").(map[string]any)
		if !ok1 || !ok2 {
			return sbVal{}, false
		}
		months := sortedKeys(all)
		if len(months) > 3 {
			months = months[len(months)-3:]
		}
		tot, a := 0.0, 0.0
		for _, m := range months {
			tot += asFloat(all[m])
			a += asFloat(ag[m])
		}
		if tot == 0 {
			return sbVal{}, false
		}
		v := round2(a / tot)
		return sbVal{Show: v, Num: v, HasNum: true}, true
	}
	ci := sbMetric{Name: "CI configured", Better: "none", How: "whether any CI configuration file is tracked", Source: "snapshot field hygiene.ci"}
	ci.Get = func(s map[string]any) (sbVal, bool) {
		m, ok := dig(s, "hygiene", "ci").(map[string]any)
		if !ok {
			return sbVal{}, false
		}
		for _, v := range m {
			if l, ok := v.([]any); ok && len(l) > 0 {
				return toVal(true)
			}
		}
		return toVal(false)
	}
	ciTests := sbMetric{Name: "CI runs tests", Better: "none", How: "whether a run: step of the GitHub Actions or GitLab CI config runs tests", Source: "snapshot field hygiene.ci_kinds_run contains test"}
	ciTests.Get = func(s map[string]any) (sbVal, bool) {
		l, ok := dig(s, "hygiene", "ci_kinds_run").([]any)
		if !ok {
			return sbVal{}, false
		}
		for _, k := range l {
			if k == "test" {
				return toVal(true)
			}
		}
		return toVal(false)
	}
	ms = append(ms, agent, ci)
	has := false
	for _, s := range snaps {
		if _, ok := dig(s.data, "hygiene", "ci_kinds_run").([]any); ok {
			has = true
		}
	}
	if has {
		ms = append(ms, ciTests)
	}
	return ms
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}
