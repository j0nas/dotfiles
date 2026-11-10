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
	Name, Unit, Better, How string
	Get                     func(snap map[string]any) (sbVal, bool)
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
	return snaps, skipped, nil
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
	var metrics []sbMetric
	if len(run.Metrics) > 0 {
		for _, d := range run.Metrics {
			metrics = append(metrics, metricFromDef(d))
		}
	} else {
		metrics = defaultMetrics(snaps)
	}
	asOf := ""
	if len(snaps) > 0 {
		last := snaps[len(snaps)-1]
		asOf = str(dig(last.data, "meta", "as_of"))
		if asOf == "" && len(last.MeasuredAt) >= 10 {
			asOf = last.MeasuredAt[:10]
		}
	}
	for _, m := range metrics {
		row := sbRow{Metric: m.Name, Better: m.Better, How: m.How, AsOf: asOf, Unit: m.Unit, Trend: []float64{}}
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
	switch len(snaps) {
	case 0:
		res.Note = "No snapshots found; every value is null."
	default:
		res.Note = fmt.Sprintf("%d snapshots, %s to %s. Every value is read from a snapshot field or recounted by codebase-baseline measure; first is the oldest snapshot that has the metric, trend skips snapshots that lack it.",
			len(snaps), snaps[0].MeasuredAt[:min(10, len(snaps[0].MeasuredAt))], snaps[len(snaps)-1].MeasuredAt[:min(10, len(snaps[len(snaps)-1].MeasuredAt))])
	}
	return res, nil
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
	m := sbMetric{Name: d.Name, Unit: d.Unit, Better: d.Better}
	if d.Kind == "snapshot" {
		m.How = "snapshot field " + d.Path
		m.Get = func(s map[string]any) (sbVal, bool) {
			v, ok := getPath(s, d.Path)
			if !ok {
				return sbVal{}, false
			}
			return toVal(v)
		}
		return m
	}
	m.How = "lines matching /" + d.Pattern + "/"
	if len(d.Globs) > 0 {
		m.How += " in " + strings.Join(d.Globs, ", ")
	}
	if len(d.Exclude) > 0 {
		m.How += ", excluding " + strings.Join(d.Exclude, ", ")
	}
	m.How += " (files tracked at the snapshot's commit; recount with measure --metrics run.json)"
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

func pathMetric(name, unit, better, path string) sbMetric {
	return metricFromDef(metricDef{Name: name, Kind: "snapshot", Path: path, Unit: unit, Better: better})
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

// defaultMetrics is the built-in scoreboard: loop results for the loops the
// snapshots contain, then size, dependencies, tests, TODOs, ownership and CI.
func defaultMetrics(snaps []sbSnapshot) []sbMetric {
	type loopInfo struct{ kind, cmd string }
	loops := map[string]loopInfo{}
	var order []string
	for _, s := range snaps {
		if lm, ok := dig(s.data, "loops").(map[string]any); ok {
			if ls, ok := lm["loops"].([]any); ok {
				for _, e := range ls {
					if em, ok := e.(map[string]any); ok {
						id := str(em["id"])
						if _, seen := loops[id]; !seen {
							order = append(order, id)
						}
						loops[id] = loopInfo{str(em["kind"]), str(em["cmd"])}
					}
				}
			}
		}
	}
	var ms []sbMetric
	loopPath := func(id, field string) string { return "loops.loops[id=" + id + "]." + field }
	loopMetric := func(name, unit, better, id, field string) sbMetric {
		m := pathMetric(name+" ("+id+")", unit, better, loopPath(id, field))
		m.How = "run-loops `" + loops[id].cmd + "`; snapshot field " + loopPath(id, field)
		return m
	}
	flakyIDs := []string{}
	for _, id := range order {
		l := loops[id]
		switch l.kind {
		case "test":
			ms = append(ms, loopMetric("Tests passed", "tests", "none", id, "parsed.passed"), loopMetric("Tests failed", "tests", "down", id, "parsed.failed"))
			flakyIDs = append(flakyIDs, id)
		case "typecheck":
			ms = append(ms, loopMetric("Type errors", "errors", "down", id, "parsed.errors"))
		case "lint":
			ms = append(ms, loopMetric("Lint warnings", "warnings", "down", id, "parsed.warnings"), loopMetric("Lint errors", "errors", "down", id, "parsed.errors"))
		case "build":
			id := id
			m := sbMetric{Name: "Build (" + id + ")", Better: "none", How: "run-loops `" + l.cmd + "`; snapshot field " + loopPath(id, "status")}
			m.Get = func(s map[string]any) (sbVal, bool) {
				v, ok := getPath(s, loopPath(id, "status"))
				st, _ := v.(string)
				if !ok || st == "" || st == "disabled" || st == "not_selected" || st == "skipped" {
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
			m := sbMetric{Name: "Advisories, critical and high (" + id + ")", Unit: "advisories", Better: "down", How: "run-loops `" + l.cmd + "`; snapshot fields " + loopPath(id, "parsed.critical") + " + " + loopPath(id, "parsed.high")}
			m.Get = func(s map[string]any) (sbVal, bool) {
				n, ok := sumPath(s, loopPath(id, "parsed.critical"), loopPath(id, "parsed.high"))
				if !ok {
					return sbVal{}, false
				}
				return sbVal{Show: n, Num: n, HasNum: true}, true
			}
			ms = append(ms, m, loopMetric("Advisories, all severities", "advisories", "down", id, "parsed.total"))
		}
	}
	if len(flakyIDs) > 0 {
		fm := sbMetric{Name: "Flaky test loops", Unit: "loops", Better: "down", How: "run-loops --repeat-tests: loops of kind test whose counts differ between runs; snapshot fields loops.loops[kind=test].flaky"}
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
		pathMetric("Code lines (excluding docs and data)", "lines", "none", "size.totals_code_only.code"),
		pathMetric("Direct dependencies", "dependencies", "none", "deps.direct_total"),
		pathMetric("Test files", "files", "none", "hygiene.test_files.total"),
		pathMetric("TODO, FIXME, HACK and XXX markers", "markers", "none", "hygiene.todo_markers.total"),
		pathMetric("Top author's share of commits, last 12 months", "of commits", "none", "git.top_author_share_last_12m"),
		pathMetric("Authors covering 80% of commits, last 12 months", "authors", "none", "git.authors_for_80pct_last_12m"),
		pathMetric("Revert commits, last 12 months", "commits", "none", "git.revert_commits_last_12m"),
	)
	agent := sbMetric{Name: "Commits with an agent co-author trailer, last 3 months (lower bound)", Unit: "of commits", Better: "none",
		How: "git.agent_commits_by_month over git.commits_by_month, latest three months of the snapshot; agents that leave no trailer are not counted"}
	agent.Get = func(s map[string]any) (sbVal, bool) {
		all, ok1 := dig(s, "git", "commits_by_month").(map[string]any)
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
	ci := sbMetric{Name: "CI configured", Better: "none", How: "snapshot field hygiene.ci: any CI config file tracked"}
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
	ciTests := sbMetric{Name: "CI runs tests", Better: "none", How: "snapshot field hygiene.ci_kinds_run contains test (GitHub Actions and GitLab run: steps only)"}
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
