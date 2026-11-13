package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// run-loops: install, then build, then the rest.

func TestRunLoopsInstallThenBuildThenRest(t *testing.T) {
	dir := t.TempDir()
	// The type check needs a file the build generates, as with i18n code or
	// route trees. In loops.json order it comes before the build.
	lf := loopsFile{Loops: []loop{
		{ID: "lint", Kind: "lint", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "tsc", Kind: "typecheck", Cmd: "test -f generated.txt", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "build", Kind: "build", Cmd: "echo built > generated.txt", Cwd: ".", Enabled: true, TimeoutS: 30, Requires: []string{"install"}},
		{ID: "test", Kind: "test", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "install", Kind: "install", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "build2", Kind: "build", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30},
	}}
	res := runLoops(lf, dir, runOpts{Repeat: 1, Now: timeNow, Getenv: fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})})
	if got := strings.Join(res.RunOrder, ","); got != "install,build,build2,lint,tsc,test" {
		t.Errorf("run_order = %s", got)
	}
	var ids []string
	for _, r := range res.Loops {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != strings.Join(res.RunOrder, ",") {
		t.Errorf("results are in order %v, run_order says %v", ids, res.RunOrder)
	}
	for _, r := range res.Loops {
		if r.ID == "tsc" && r.Status != "passed" {
			t.Errorf("tsc ran before the build generated its input: %+v", r)
		}
	}
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), `"run_order":["install","build","build2","lint","tsc","test"]`) {
		t.Errorf("run_order missing from the JSON: %s", b)
	}
}

func TestRunLoopsFailedBuildSkipsOnlyWhatRequiresIt(t *testing.T) {
	dir := t.TempDir()
	lf := loopsFile{Loops: []loop{
		{ID: "tsc", Kind: "typecheck", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "needs-build", Kind: "test", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30, Requires: []string{"build"}},
		{ID: "build", Kind: "build", Cmd: "exit 2", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "lint", Kind: "lint", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30},
		// A build that requires a loop which is not a build still starts after it.
		{ID: "build-after-lint", Kind: "build", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30, Requires: []string{"lint"}},
	}}
	res := runLoops(lf, dir, runOpts{Repeat: 1, Now: timeNow, Getenv: fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})})
	by := map[string]loopResult{}
	for _, r := range res.Loops {
		by[r.ID] = r
	}
	if by["build"].Status != "failed" {
		t.Fatalf("build = %+v", by["build"])
	}
	if by["tsc"].Status != "passed" || by["lint"].Status != "passed" {
		t.Errorf("a failed build skipped loops that do not require it: tsc=%s lint=%s", by["tsc"].Status, by["lint"].Status)
	}
	if r := by["needs-build"]; r.Status != "skipped" || !strings.Contains(r.Reason, "requires build") {
		t.Errorf("needs-build = %+v", r)
	}
	if by["build-after-lint"].Status != "passed" {
		t.Errorf("build-after-lint = %+v", by["build-after-lint"])
	}
	order := strings.Join(res.RunOrder, ",")
	if strings.Index(order, "lint,build-after-lint") < 0 {
		t.Errorf("a loop must follow what it requires: %s", order)
	}
}

// duplicate_of

func TestDuplicateLoops(t *testing.T) {
	dir := t.TempDir()
	lf := loopsFile{Loops: []loop{
		{ID: "root-test", Kind: "test", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "pkg-test", Kind: "test", Cmd: "exit 9", Cwd: ".", Enabled: false, TimeoutS: 30, DuplicateOf: "root-test", Needs: []string{"x"}},
		{ID: "off", Kind: "e2e", Cmd: "true", Cwd: ".", Enabled: false},
	}}
	if err := checkDuplicates(lf.Loops); err != nil {
		t.Fatal(err)
	}
	res := runLoops(lf, dir, runOpts{Repeat: 1, Now: timeNow, Getenv: fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})})
	by := map[string]loopResult{}
	for _, r := range res.Loops {
		by[r.ID] = r
	}
	d := by["pkg-test"]
	if d.Status != "duplicate" || d.DuplicateOf != "root-test" || !strings.Contains(d.Reason, "root-test") || d.ExitCode != nil {
		t.Errorf("duplicate = %+v", d)
	}
	if by["off"].Status != "disabled" || res.Summary["duplicate"] != 1 {
		t.Errorf("off = %+v summary = %v", by["off"], res.Summary)
	}
	// measure: a duplicate is no gap.
	b, _ := json.Marshal(res)
	var lr any
	_ = json.Unmarshal(b, &lr)
	nr := notRunOf(lr)
	if nr.Count != 1 || nr.Reasons[0].ID != "off" {
		t.Errorf("loops_not_run = %+v", nr)
	}
	dup := duplicatesOf(lr)
	if dup.Count != 1 || dup.Loops[0].ID != "pkg-test" || dup.Loops[0].DuplicateOf != "root-test" {
		t.Errorf("loops_duplicate = %+v", dup)
	}
	// A typo is refused.
	bad := []loop{{ID: "a", DuplicateOf: "nope"}}
	if err := checkDuplicates(bad); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown target accepted: %v", err)
	}
	if err := checkDuplicates([]loop{{ID: "a", DuplicateOf: "a"}}); err == nil {
		t.Error("self-duplicate accepted")
	}
	// The loops file field is read as duplicate_of.
	var lf2 loopsFile
	if err := json.Unmarshal([]byte(`{"loops":[{"id":"x","duplicate_of":"y"}]}`), &lf2); err != nil || lf2.Loops[0].DuplicateOf != "y" {
		t.Errorf("duplicate_of not read: %v %+v", err, lf2)
	}
}

// discover marks inferred loops

func TestDiscoverMarksInferredLoops(t *testing.T) {
	res := discoverFixture(t, "js-pnpm")
	tsc := findLoop(t, res, "js-tsc-noemit")
	if !tsc.Inferred || !strings.Contains(tsc.InferredFrom, "tsconfig.json") || !strings.Contains(tsc.InferredFrom, "no typecheck script") {
		t.Errorf("tsc loop = inferred %v from %q", tsc.Inferred, tsc.InferredFrom)
	}
	for _, id := range []string{"js-test", "js-lint", "js-build", "js-packages-api-typecheck"} {
		if l := findLoop(t, res, id); l.Inferred || l.InferredFrom != "" {
			t.Errorf("%s comes from a package.json script, not inferred: %+v", id, l)
		}
	}
	if l := findLoop(t, res, "js-install"); !l.Inferred || l.InferredFrom == "" {
		t.Errorf("install is the package manager's usual command: %+v", l)
	}
	mk := discoverFixture(t, "make")
	for _, l := range mk.Loops {
		if l.Ecosystem == "make" && l.Inferred {
			t.Errorf("Makefile target %s must not be inferred", l.ID)
		}
	}
	goRes := discoverFixture(t, "go-mod")
	if l := findLoop(t, goRes, "go-vet"); !l.Inferred || !strings.Contains(l.InferredFrom, "go.mod") {
		t.Errorf("go vet = %+v", l)
	}
	// The field reaches the file, the run results and the snapshot.
	b, _ := json.Marshal(tsc)
	if !strings.Contains(string(b), `"inferred":true`) || !strings.Contains(string(b), `"inferred_from"`) {
		t.Errorf("loops.json entry = %s", b)
	}
	dir := t.TempDir()
	lf := loopsFile{Loops: []loop{{ID: "tsc", Kind: "typecheck", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30, Inferred: true, InferredFrom: "typescript is a dependency"}, {ID: "own", Kind: "lint", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30}}}
	rr := runLoops(lf, dir, runOpts{Repeat: 1, Now: timeNow, Getenv: fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})})
	by := map[string]loopResult{}
	for _, r := range rr.Loops {
		by[r.ID] = r
	}
	if !by["tsc"].Inferred || by["tsc"].InferredFrom != "typescript is a dependency" || by["own"].Inferred {
		t.Errorf("results = %+v / %+v", by["tsc"], by["own"])
	}
	rb, _ := json.Marshal(rr)
	var snapLoops any
	_ = json.Unmarshal(rb, &snapLoops) // measure embeds the results as they are
	if v, ok := getPath(map[string]any{"loops": snapLoops}, "loops.loops[id=tsc].inferred"); !ok || v != true {
		t.Errorf("snapshot path loops.loops[id=tsc].inferred = %v %v", v, ok)
	}
}

// hooks

func TestHygieneFindsHookDirectories(t *testing.T) {
	files := []string{
		".vite-hooks/pre-commit", ".vite-hooks/pre-push", ".vite-hooks/_/husky.sh",
		"ci-hooks/commit-msg", "ci-hooks/README.md",
		"scripts/update", "tools/deep/pre-commit", "src/pre-push.ts", "package.json",
		".husky/pre-commit",
	}
	h := scanHygiene(t.TempDir(), files, nil)
	if got := strings.Join(h.Hooks["vite_hooks"], ","); got != ".vite-hooks/pre-commit,.vite-hooks/pre-push" {
		t.Errorf("vite_hooks = %v", h.Hooks["vite_hooks"])
	}
	if got := strings.Join(h.Hooks["hooks_dir"], ","); got != "ci-hooks/commit-msg" {
		t.Errorf("hooks_dir = %v (only hook-named files directly under a top-level directory count)", h.Hooks["hooks_dir"])
	}
	if len(h.Hooks["husky"]) != 1 {
		t.Errorf("husky = %v", h.Hooks["husky"])
	}
	if _, ok := h.Hooks["git_hooks_dir"]; ok {
		t.Errorf("unexpected git_hooks_dir: %v", h.Hooks)
	}
}

// agent share

func TestClassifyClaudeSessionTrailer(t *testing.T) {
	c := gitCommit{Author: "J", Email: "j@x.io", Body: "Fix the thing\n\nWhy it broke.\n\nClaude-Session: https://claude.ai/code/session_01Abc\n"}
	agents, bot := classifyCommit(c)
	if bot || len(agents) != 1 || agents[0] != "claude:session-trailer" {
		t.Errorf("agents = %v bot = %v", agents, bot)
	}
	// An empty trailer or a mention in prose is no session trailer.
	for _, body := range []string{"Claude-Session:\n", "we log a Claude-Session: trailer now"} {
		if a, _ := classifyCommit(gitCommit{Author: "J", Email: "j@x.io", Body: body}); len(a) != 0 {
			t.Errorf("body %q counted as agent-made: %v", body, a)
		}
	}
}

func TestAgentShareIsOverNonMergeCommits(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	commit := func(file string, msg ...string) {
		writeFiles(t, repo, map[string]string{file: "x"})
		gitRun(t, repo, "add", "-A")
		args := []string{"commit", "-q"}
		for _, m := range msg {
			args = append(args, "-m", m)
		}
		gitRun(t, repo, args...)
	}
	session := "Claude-Session: https://claude.ai/code/session_01Abc"
	commit("a.txt", "one", session)
	commit("b.txt", "two", session)
	commit("c.txt", "three")
	gitRun(t, repo, "checkout", "-q", "-b", "side")
	commit("d.txt", "four", session)
	gitRun(t, repo, "checkout", "-q", "main")
	commit("e.txt", "five")
	// A merge commit that carries the trailer too: it is not counted at all.
	gitRun(t, repo, "merge", "-q", "--no-ff", "side", "-m", "Merge side", "-m", session)
	g := collectGit(repo, "HEAD", 2, time.Now().Add(24*time.Hour), nil)
	if g.MergeCommits != 1 || g.CommitsTotal != 6 || g.NonMergeCommits12m != 5 {
		t.Fatalf("merges=%d total=%d non-merge-12m=%d", g.MergeCommits, g.CommitsTotal, g.NonMergeCommits12m)
	}
	if g.AgentCommits12m != 3 || g.AgentCommitsTotal != 3 || g.AgentTrailerShare != 0.6 {
		t.Errorf("agent 12m=%d total=%d share=%v, want 3, 3, 0.6", g.AgentCommits12m, g.AgentCommitsTotal, g.AgentTrailerShare)
	}
	if !strings.Contains(g.AgentNote, "non-merge") || !strings.Contains(g.AgentNote, "Claude-Session") {
		t.Errorf("note = %s", g.AgentNote)
	}
	var nm, ag int
	for _, n := range g.NonMergeByMonth {
		nm += n
	}
	for _, n := range g.AgentCommitsByMonth {
		ag += n
	}
	if nm != 5 || ag != 3 {
		t.Errorf("by month: non-merge %d agent %d", nm, ag)
	}
}

// scoreboard

func writeSnap(t *testing.T, dir, file, at, head string, extra map[string]any) {
	t.Helper()
	m := map[string]any{"schema_version": 1, "meta": map[string]any{"measured_at": at, "as_of": at[:10], "head_sha": head}, "size": map[string]any{"totals_code_only": map[string]any{"code": 100.0}}}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, file), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func testLoops(status string, passed, failed float64) map[string]any {
	return map[string]any{"repeat_tests": 2.0, "loops": []any{
		map[string]any{"id": "js-test", "kind": "test", "cmd": "pnpm run test", "cwd": ".", "status": status, "parsed": map[string]any{"passed": passed, "failed": failed}},
		map[string]any{"id": "js-build", "kind": "build", "cmd": "pnpm run build", "cwd": ".", "status": status},
		map[string]any{"id": "js-pkg-test", "kind": "test", "cmd": "pnpm run test", "cwd": "packages/a", "status": "duplicate"},
	}}
}

func rowsByName(res sbOut) map[string]sbRow {
	m := map[string]sbRow{}
	for _, r := range res.Rows {
		m[r.Metric] = r
	}
	return m
}

func TestScoreboardKeepsBuiltInRows(t *testing.T) {
	dir := t.TempDir()
	writeSnap(t, dir, "a.json", "2026-01-01T00:00:00Z", "aaa", map[string]any{"loops": testLoops("passed", 10, 1)})
	writeSnap(t, dir, "b.json", "2026-02-01T00:00:00Z", "bbb", map[string]any{"loops": testLoops("passed", 12, 0)})
	run := runFile{
		Metrics: []metricDef{
			{Name: "Owner checks missing", Kind: "snapshot", Path: "size.totals_code_only.code", Better: "down", How: "handlers without an owner check"},
			{Name: "tests passed (js-test)", Kind: "snapshot", Path: "loops.loops[id=js-test].parsed.passed", Better: "up", How: "my own words"},
		},
		Headline: []any{"Owner checks missing"},
	}
	res, err := buildScoreboard(run, dir)
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByName(res)
	for _, name := range []string{"Tests failed (js-test)", "Build (js-build)", "Direct dependencies", "Test files", "CI configured"} {
		if _, ok := rows[name]; !ok {
			t.Errorf("built-in row %q dropped by a declared metric", name)
		}
	}
	if res.Rows[0].Metric != "Owner checks missing" || !res.Rows[0].Headline {
		t.Errorf("headline must lead: %q", res.Rows[0].Metric)
	}
	// The declared metric replaces the built-in of the same name, in its place.
	count := 0
	for _, r := range res.Rows {
		if strings.EqualFold(r.Metric, "Tests passed (js-test)") {
			count++
			if r.How != "my own words" || r.Better != "up" || r.Value != 12.0 {
				t.Errorf("replacement row = %+v", r)
			}
		}
	}
	if count != 1 {
		t.Errorf("%d rows named Tests passed (js-test)", count)
	}
	if _, ok := rows["Owner checks missing"]; !ok {
		t.Error("declared row missing")
	}
	if len(res.Rows) < 8 {
		t.Errorf("only %d rows", len(res.Rows))
	}

	// builtin_rows: false gives the declared metrics alone.
	off := false
	run.BuiltinRows = &off
	only, err := buildScoreboard(run, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(only.Rows) != 2 || only.Rows[0].Metric != "Owner checks missing" {
		t.Errorf("builtin_rows false: rows = %v", only.Rows)
	}
	// And the flag is read from run.json.
	p := filepath.Join(t.TempDir(), "run.json")
	_ = os.WriteFile(p, []byte(`{"builtin_rows": false, "metrics": [{"name":"x","kind":"snapshot","path":"a.b"}]}`), 0o644)
	rf, err := readRunFile(p)
	if err != nil || rf.builtinRows() {
		t.Errorf("run.json builtin_rows: %v %+v", err, rf.BuiltinRows)
	}
	if (runFile{}).builtinRows() != true {
		t.Error("built-in rows are on by default")
	}
	// Duplicate loops give no build reading.
	if b := rows["Build (js-pkg-test)"]; b.Metric != "" {
		t.Errorf("unexpected build row %+v", b)
	}
}

func TestScoreboardNewestSnapshotPerCommit(t *testing.T) {
	dir := t.TempDir()
	// The first snapshot of commit ccc lacks the custom metric run.json gained later.
	writeSnap(t, dir, "2026-01-01T0900_aaa.json", "2026-01-01T09:00:00Z", "aaa", nil)
	writeSnap(t, dir, "2026-02-01T0900_ccc.json", "2026-02-01T09:00:00Z", "ccc", nil)
	writeSnap(t, dir, "2026-02-01T1000_ccc.json", "2026-02-01T10:00:00Z", "ccc", map[string]any{"custom": []any{map[string]any{"name": "Marked", "value": 5.0}}})
	writeSnap(t, dir, "2026-02-01T1100_ccc-b.json", "2026-02-01T11:00:00Z", "ccc", map[string]any{"custom": []any{map[string]any{"name": "Marked", "value": 7.0}}})
	run := runFile{Metrics: []metricDef{{Name: "Marked", Kind: "count", Pattern: "x", Globs: []string{"src/**"}, Better: "down"}}}
	res, err := buildScoreboard(run, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) != 2 || res.Snapshots[0].File != "2026-01-01T0900_aaa.json" || res.Snapshots[1].File != "2026-02-01T1100_ccc-b.json" {
		t.Errorf("snapshots = %+v", res.Snapshots)
	}
	if len(res.Skipped) != 2 || !strings.Contains(res.Skipped[0], "superseded by 2026-02-01T1100_ccc-b.json") {
		t.Errorf("skipped = %v", res.Skipped)
	}
	if !strings.Contains(res.Note, "2 older snapshots of the same commit skipped") {
		t.Errorf("note = %s", res.Note)
	}
	r := rowsByName(res)["Marked"]
	if r.First != 7.0 || r.Value != 7.0 || len(r.Trend) != 1 {
		t.Errorf("first must come from the newest snapshot of the commit: %+v", r)
	}
	// One snapshot per commit: nothing to skip, nothing to say.
	dir2 := t.TempDir()
	writeSnap(t, dir2, "x.json", "2026-01-01T09:00:00Z", "aaa", nil)
	writeSnap(t, dir2, "y.json", "2026-01-02T09:00:00Z", "bbb", nil)
	r2, _ := buildScoreboard(runFile{}, dir2)
	if strings.Contains(r2.Note, "skipped") || len(r2.Skipped) != 0 {
		t.Errorf("note = %q skipped = %v", r2.Note, r2.Skipped)
	}
	// Snapshots with no head_sha are never merged into one.
	dir3 := t.TempDir()
	writeSnap(t, dir3, "x.json", "2026-01-01T09:00:00Z", "", nil)
	writeSnap(t, dir3, "y.json", "2026-01-02T09:00:00Z", "", nil)
	if r3, _ := buildScoreboard(runFile{}, dir3); len(r3.Snapshots) != 2 {
		t.Errorf("snapshots without a commit were merged: %+v", r3.Snapshots)
	}
}

func TestScoreboardHowIsPlainWordsAndSourceHoldsThePath(t *testing.T) {
	dir := t.TempDir()
	writeSnap(t, dir, "a.json", "2026-01-01T00:00:00Z", "aaa", map[string]any{"loops": testLoops("passed", 10, 1)})
	run := runFile{Metrics: []metricDef{
		{Name: "Handlers", Kind: "count", Pattern: `export async function handler`, Globs: []string{"apps/web/src"}, Exclude: []string{"**/*.test.ts"}},
		{Name: "Everywhere", Kind: "count", Pattern: "TODO"},
		{Name: "Raw", Kind: "snapshot", Path: "size.totals_code_only.code"},
		{Name: "Failed in root", Kind: "snapshot", Path: "loops.loops[id=js-test].parsed.failed", Better: "down"},
		{Name: "Slow", Kind: "snapshot", Path: "loops.loops[id=js-pkg-test].duration_s"},
	}}
	res, err := buildScoreboard(run, dir)
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByName(res)
	if h := rows["Handlers"]; h.How != "lines matching the pattern in apps/web/src, excluding **/*.test.ts" || !strings.HasPrefix(h.Source, "lines matching /export async function handler/ in apps/web/src") {
		t.Errorf("count row how=%q source=%q", h.How, h.Source)
	}
	if e := rows["Everywhere"]; e.How != "lines matching the pattern in every tracked file" {
		t.Errorf("how = %q", e.How)
	}
	if r := rows["Raw"]; r.How != "value recorded in each snapshot" || r.Source != "snapshot field size.totals_code_only.code" {
		t.Errorf("raw = %+v", r)
	}
	if r := rows["Failed in root"]; r.How != "failed tests in `pnpm run test`, run twice" || !strings.Contains(r.Source, "loops.loops[id=js-test]") {
		t.Errorf("declared loop metric = %+v", r)
	}
	if r := rows["Slow"]; r.How != "seconds one run of `pnpm run test` in packages/a takes" {
		t.Errorf("duration row how = %q", r.How)
	}
	if f := rows["Tests failed (js-test)"]; f.How != "failed tests in `pnpm run test`, run twice" || f.Source != "snapshot field loops.loops[id=js-test].parsed.failed" {
		t.Errorf("built-in test row how=%q source=%q", f.How, f.Source)
	}
	if f := rows["Tests failed (js-pkg-test)"]; !strings.Contains(f.How, "in packages/a") {
		t.Errorf("a loop in a folder says where: %q", f.How)
	}
	if b := rows["Build (js-build)"]; b.How != "whether `pnpm run build` succeeds" || b.Source != "snapshot field loops.loops[id=js-build].status" {
		t.Errorf("build row = %+v", b)
	}
	for _, r := range res.Rows {
		if strings.Contains(r.How, "loops.loops[") || strings.Contains(r.How, "snapshot field") || strings.Contains(r.How, "git.") || strings.Contains(r.How, "hygiene.") {
			t.Errorf("how of %q holds a field path: %q", r.Metric, r.How)
		}
		if r.Source == "" {
			t.Errorf("row %q has no source", r.Metric)
		}
	}
	b, _ := json.Marshal(rows["Raw"])
	if !strings.Contains(string(b), `"source":"snapshot field size.totals_code_only.code"`) {
		t.Errorf("json = %s", b)
	}
}

func TestScoreboardAgentRowReadsOldAndNewSnapshots(t *testing.T) {
	dir := t.TempDir()
	// An old snapshot has commits_by_month only; a new one also non_merge_commits_by_month.
	writeSnap(t, dir, "old.json", "2026-01-01T00:00:00Z", "aaa", map[string]any{"git": map[string]any{
		"commits_by_month": map[string]any{"2026-01": 10.0}, "agent_commits_by_month": map[string]any{"2026-01": 2.0}}})
	writeSnap(t, dir, "new.json", "2026-02-01T00:00:00Z", "bbb", map[string]any{"git": map[string]any{
		"commits_by_month": map[string]any{"2026-02": 12.0}, "non_merge_commits_by_month": map[string]any{"2026-02": 10.0},
		"agent_commits_by_month": map[string]any{"2026-02": 6.0}}})
	res, err := buildScoreboard(runFile{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var row sbRow
	for _, r := range res.Rows {
		if strings.HasPrefix(r.Metric, "Commits made with an agent") {
			row = r
		}
	}
	if row.First != 0.2 || row.Value != 0.6 {
		t.Errorf("agent row = %+v", row)
	}
}
