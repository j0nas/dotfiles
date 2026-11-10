package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLoopsOnlyMergesIntoExistingResults(t *testing.T) {
	dir := t.TempDir()
	mk := func(id, cmd string) loop {
		return loop{ID: id, Kind: "test", Cmd: cmd, Cwd: ".", Enabled: true, TimeoutS: 30}
	}
	env := fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})
	lf := loopsFile{Loops: []loop{mk("a", `echo "# tests 1"; echo "# pass 1"; echo "# fail 0"`), mk("b", "exit 1"), mk("c", `echo "# tests 3"; echo "# pass 3"; echo "# fail 0"`)}}
	first := runLoops(lf, dir, runOpts{Repeat: 1, Getenv: env, Now: timeNow})
	if first.Summary["failed"] != 1 {
		t.Fatalf("first run summary = %v", first.Summary)
	}
	// b gets fixed; rerun only b against the earlier results.
	lf.Loops[1].Cmd = `echo "# tests 2"; echo "# pass 2"; echo "# fail 0"`
	second := runLoops(lf, dir, runOpts{Repeat: 1, Only: map[string]bool{"b": true}, Prev: &first, Getenv: env, Now: timeNow})
	if len(second.Loops) != 3 {
		t.Fatalf("loops = %d", len(second.Loops))
	}
	for i, id := range []string{"a", "b", "c"} {
		if second.Loops[i].ID != id {
			t.Errorf("order: %d = %s", i, second.Loops[i].ID)
		}
	}
	if second.Loops[1].Status != "passed" || second.Loops[1].Parsed["total"] != 2 {
		t.Errorf("b should be rerun: %+v", second.Loops[1])
	}
	if second.Loops[0].Parsed["total"] != 1 || second.Loops[2].Parsed["total"] != 3 || second.Loops[0].Status != "passed" {
		t.Errorf("a and c should be kept: %+v %+v", second.Loops[0], second.Loops[2])
	}
	if second.Summary["passed"] != 3 || second.Summary["failed"] != 0 || second.Summary["not_selected"] != 0 {
		t.Errorf("summary = %v", second.Summary)
	}
	// A rerun of a dependant sees an install that failed earlier.
	lf2 := loopsFile{Loops: []loop{mk("install", "exit 2"), {ID: "t", Kind: "test", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30, Requires: []string{"install"}}}}
	p := runLoops(lf2, dir, runOpts{Repeat: 1, Getenv: env, Now: timeNow})
	q := runLoops(lf2, dir, runOpts{Repeat: 1, Only: map[string]bool{"t": true}, Prev: &p, Getenv: env, Now: timeNow})
	if q.Loops[1].Status != "skipped" {
		t.Errorf("dependant of a failed install should be skipped: %+v", q.Loops[1])
	}
}

func TestAuditExitIsAResult(t *testing.T) {
	dir := t.TempDir()
	env := fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})
	found := `echo '{"metadata":{"vulnerabilities":{"info":0,"low":1,"moderate":2,"high":3,"critical":4}}}'; exit 1`
	broken := `echo '{"error":{"code":"ENOLOCK","message":"no lockfile"}}'; exit 1`
	lf := loopsFile{Loops: []loop{
		{ID: "findings", Kind: "audit", Cmd: found, Cwd: ".", Enabled: true, TimeoutS: 30, ExitIsResult: true},
		{ID: "broken", Kind: "audit", Cmd: broken, Cwd: ".", Enabled: true, TimeoutS: 30, ExitIsResult: true},
		{ID: "clean", Kind: "audit", Cmd: `echo '{"metadata":{"vulnerabilities":{"critical":0,"high":0}}}'`, Cwd: ".", Enabled: true, TimeoutS: 30, ExitIsResult: true},
		{ID: "ordinary", Kind: "lint", Cmd: "exit 1", Cwd: ".", Enabled: true, TimeoutS: 30},
	}}
	res := runLoops(lf, dir, runOpts{Repeat: 1, Getenv: env, Now: timeNow})
	by := map[string]loopResult{}
	for _, r := range res.Loops {
		by[r.ID] = r
	}
	if r := by["findings"]; r.Status != "findings" || !r.ExitIsResult || r.Parsed["critical"] != 4 || r.Parsed["total"] != 10 {
		t.Errorf("findings = %+v", r)
	}
	if by["broken"].Status != "failed" || by["clean"].Status != "passed" || by["ordinary"].Status != "failed" {
		t.Errorf("statuses = %s %s %s", by["broken"].Status, by["clean"].Status, by["ordinary"].Status)
	}
}

func TestParseAudit(t *testing.T) {
	npm := `{"auditReportVersion":2,"metadata":{"vulnerabilities":{"info":0,"low":2,"moderate":1,"high":0,"critical":3,"total":6}}}`
	if p := parseAudit("pnpm audit --json", npm); p["critical"] != 3 || p["low"] != 2 || p["total"] != 6 || p["tool"] != "pnpm audit" {
		t.Errorf("pnpm/npm = %v", p)
	}
	classic := `{"type":"auditAdvisory","data":{}}` + "\n" + `{"type":"auditSummary","data":{"vulnerabilities":{"info":0,"low":0,"moderate":4,"high":2,"critical":0},"dependencies":10}}` + "\n"
	if p := parseAudit("yarn audit --json", classic); p["moderate"] != 4 || p["high"] != 2 || p["total"] != 6 {
		t.Errorf("yarn classic = %v", p)
	}
	berry := `{"value":"a","children":{"ID":1,"Severity":"high"}}` + "\n" + `{"value":"b","children":{"ID":2,"Severity":"moderate"}}` + "\n" + `{"value":"c","children":{"ID":3,"Severity":"high"}}`
	if p := parseAudit("yarn npm audit --json", berry); p["high"] != 2 || p["moderate"] != 1 || p["total"] != 3 {
		t.Errorf("yarn berry = %v", p)
	}
	pnpmAdv := `{"advisories":{"1":{"severity":"high"},"2":{"severity":"low"}}}`
	if p := parseAudit("pnpm audit --json", pnpmAdv); p["high"] != 1 || p["total"] != 2 {
		t.Errorf("advisories only = %v", p)
	}
	if p := parseAudit("npm audit --json", `{"error":{"code":"ENOAUDIT","message":"x"}}`); p["error"] != "ENOAUDIT" {
		t.Errorf("error = %v", p)
	}
	gv := "Scanning your code and 10 packages across 2 dependent modules for known vulnerabilities...\n\n=== Symbol Results ===\n\nVulnerability #1: GO-2024-0001\n    A\nVulnerability #2: GO-2024-0002\n\n=== Package Results ===\n\nVulnerability #1: GO-2024-0003\n\nYour code is affected by 2 vulnerabilities.\n"
	if p := parseAudit("govulncheck ./...", gv); p["called"] != 2 || p["imported_or_required_only"] != 1 || p["tool"] != "govulncheck" {
		t.Errorf("govulncheck = %v", p)
	}
	if p := parseAudit("govulncheck ./...", "No vulnerabilities found.\n"); p["total"] != 0 {
		t.Errorf("govulncheck clean = %v", p)
	}
	if p := parseAudit("pip-audit -f json", "[]"); p != nil {
		t.Errorf("pip-audit records the exit code only, got %v", p)
	}
}

func TestDiscoverAuditLoops(t *testing.T) {
	js := findLoop(t, discoverFixture(t, "js-pnpm"), "js-audit")
	if js.Kind != "audit" || js.Cmd != "pnpm audit --json" || !js.Enabled || !js.ExitIsResult {
		t.Errorf("js audit = %+v", js)
	}
	check := func(fixture, id, bin string) {
		l := findLoop(t, discoverFixture(t, fixture), id)
		_, err := exec.LookPath(bin)
		onPath := err == nil
		if l.Kind != "audit" || l.Enabled != onPath || !l.ExitIsResult {
			t.Errorf("%s = %+v (tool on PATH: %v)", id, l, onPath)
		}
		if !onPath && !contains(l.Needs, "tool:"+bin) {
			t.Errorf("%s needs = %v", id, l.Needs)
		}
	}
	check("python-uv", "py-pip-audit", "pip-audit")
	check("go-mod", "go-govulncheck", "govulncheck")
	check("rust", "rust-audit", "cargo-audit")
	check("ruby", "ruby-bundle-audit", "bundle-audit")
}

func TestDocsScan(t *testing.T) {
	content := map[string]string{
		"README.md":                       "# Title\n\nHello world again\n",
		"docs/guide.md":                   "one two three\nfour\n",
		"docs/adr/0001-use-go.md":         "we chose go\n",
		"docs/img.png":                    "binary",
		"AGENTS.md":                       "rules here\n",
		".claude/agents/a.md":             "agent a\n",
		".github/copilot-instructions.md": "be nice\n",
		"node_modules/x/README.md":        "vendored\n",
		"dist/notes.md":                   "built\n",
		"vendor/lib/README.md":            "vendored\n",
		"src/main.go":                     "package main\n",
		"CHANGELOG":                       "v1\nv2\n",
		"packages/a/README.md":            "pkg a\n",
	}
	var files []string
	for p := range content {
		files = append(files, p)
	}
	d := scanDocs(files, func(rel string) (string, bool) { s, ok := content[rel]; return s, ok },
		func(paths []string) map[string]string {
			m := map[string]string{}
			for _, p := range paths {
				m[p] = "2026-11-01"
			}
			return m
		})
	got := map[string]docFile{}
	for _, f := range d.Files {
		got[f.Path] = f
	}
	for _, gone := range []string{"node_modules/x/README.md", "dist/notes.md", "vendor/lib/README.md", "src/main.go", "docs/img.png"} {
		if _, ok := got[gone]; ok {
			t.Errorf("%s must not be counted", gone)
		}
	}
	if f := got["docs/guide.md"]; f.Words != 4 || f.Lines != 2 || f.Kind != "other" || f.LastTouched != "2026-11-01" {
		t.Errorf("guide = %+v", f)
	}
	for p, kind := range map[string]string{"README.md": "readme", "packages/a/README.md": "readme", "AGENTS.md": "agent-instructions", ".claude/agents/a.md": "agent-instructions",
		".github/copilot-instructions.md": "agent-instructions", "docs/adr/0001-use-go.md": "decision", "CHANGELOG": "other"} {
		if got[p].Kind != kind {
			t.Errorf("%s kind = %q, want %q", p, got[p].Kind, kind)
		}
	}
	if strings.Join(d.AgentFiles, ",") != ".claude/agents/a.md,.github/copilot-instructions.md,AGENTS.md" {
		t.Errorf("agent files = %v", d.AgentFiles)
	}
	byFolder := map[string]docFolder{}
	for _, f := range d.Folders {
		byFolder[f.Folder] = f
	}
	if f := byFolder["docs"]; f.Files != 2 || f.Words != 4+3 {
		t.Errorf("docs folder = %+v", f)
	}
	if f := byFolder["(root)"]; f.Files != 3 {
		t.Errorf("root folder = %+v (all %v)", f, d.Folders)
	}
	if w, l := countWords("a b\nc"); w != 3 || l != 2 {
		t.Errorf("countWords = %d %d", w, l)
	}
}

func TestCustomMetricsAndGlobs(t *testing.T) {
	content := map[string]string{
		"app/api/a.ts":      "export function GET() {}\nexport function POST() {}\n// checkOwner(x)\n",
		"app/api/b.ts":      "export function GET() {}\n",
		"app/api/b.test.ts": "export function GET() {}\n",
		"lib/c.ts":          "export function GET() {}\n",
		"app/api/d.go":      "func GET() {}\n",
	}
	var files []string
	for p := range content {
		files = append(files, p)
	}
	defs := []metricDef{
		{Name: "handlers", Kind: "count", Pattern: `^export function (GET|POST)\b`, Globs: []string{"app/**/*.ts"}, Exclude: []string{"**/*.test.ts"}, Better: "down"},
		{Name: "anywhere", Kind: "count", Pattern: `GET`, Globs: []string{"*.ts"}},
		{Name: "braces", Kind: "count", Pattern: `GET`, Globs: []string{"app/**/*.{ts,go}"}},
		{Name: "bad", Kind: "count", Pattern: `(`},
		{Name: "carried", Kind: "snapshot", Path: "size.totals.code"},
	}
	res := runCustomMetrics(defs, files, func(rel string) (string, bool) { s, ok := content[rel]; return s, ok })
	want := map[string]int{"handlers": 3, "anywhere": 4, "braces": 4}
	for _, r := range res {
		if w, ok := want[r.Name]; ok && (r.Value == nil || *r.Value != w) {
			t.Errorf("%s = %v, want %d", r.Name, r.Value, w)
		}
	}
	if res[0].FilesHit != 2 || res[0].TopFiles[0].Path != "app/api/a.ts" || res[0].Definition.Better != "down" {
		t.Errorf("handlers detail = %+v", res[0])
	}
	if !strings.Contains(res[3].Unavailable, "bad pattern") || res[3].Value != nil {
		t.Errorf("bad = %+v", res[3])
	}
	if !res[4].Carried || res[4].Value != nil {
		t.Errorf("carried = %+v", res[4])
	}
}

func TestMeasureMetricsReadTrackedFilesOnly(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	write := func(rel, content string) {
		p := filepath.Join(repo, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.ts", "TODO_X\nTODO_X\n")
	write("README.md", "one two three four five\n")
	write("AGENTS.md", "rules\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "init")
	write("b.ts", "TODO_X\n") // untracked
	write("a.ts", "TODO_X\n") // edited in the working tree, not committed
	run := filepath.Join(t.TempDir(), "run.json")
	_ = os.WriteFile(run, []byte(`{"metrics":[{"name":"todo x","kind":"count","pattern":"TODO_X","globs":["*.ts"],"unit":"markers","better":"down"}],"headline":["todo x"]}`), 0o644)
	for _, worktree := range []bool{false, true} {
		out := filepath.Join(t.TempDir(), "snap.json")
		args := []string{"measure", repo, "--out", out, "--metrics", run, "--months", "2"}
		if worktree {
			args = append(args, "--worktree")
		}
		var so, se strings.Builder
		a := &app{stdout: &so, stderr: &se, now: timeNow}
		if code := a.run(args); code != 0 {
			t.Fatalf("measure: %s", se.String())
		}
		var snap snapshot
		b, _ := os.ReadFile(out)
		if err := json.Unmarshal(b, &snap); err != nil {
			t.Fatal(err)
		}
		if len(snap.Custom) != 1 || snap.Custom[0].Value == nil || *snap.Custom[0].Value != 2 {
			t.Errorf("worktree=%v: custom = %+v (HEAD has two matches in a.ts; b.ts is untracked)", worktree, snap.Custom)
		}
		if snap.Custom[0].Definition.Unit != "markers" {
			t.Errorf("definition not embedded: %+v", snap.Custom[0].Definition)
		}
		if snap.Docs.Totals.Files != 2 || snap.Docs.Totals.Words != 6 || len(snap.Docs.AgentFiles) != 1 {
			t.Errorf("docs = %+v", snap.Docs)
		}
		for _, f := range snap.Docs.Files {
			if f.LastTouched == "" {
				t.Errorf("%s has no last-touched date", f.Path)
			}
		}
	}
}

func TestScoreboard(t *testing.T) {
	dir := t.TempDir()
	snap := func(file, at string, code float64, extra map[string]any) {
		m := map[string]any{"schema_version": 1, "meta": map[string]any{"measured_at": at, "as_of": at[:10], "head_sha": file}, "size": map[string]any{"totals_code_only": map[string]any{"code": code}}}
		for k, v := range extra {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, file), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loop := func(status string, passed, failed float64, flaky bool) map[string]any {
		return map[string]any{"loops": []any{
			map[string]any{"id": "js-test", "kind": "test", "cmd": "pnpm run test", "status": status, "flaky": flaky, "parsed": map[string]any{"passed": passed, "failed": failed}},
			map[string]any{"id": "js-build", "kind": "build", "cmd": "pnpm run build", "status": status},
		}}
	}
	// File names sort against the dates on purpose.
	snap("z-old.json", "2026-01-01T00:00:00Z", 100, map[string]any{"loops": loop("passed", 10, 0, false), "custom": []any{map[string]any{"name": "Handlers without owner check", "value": 9.0}}})
	snap("a-mid.json", "2026-02-01T00:00:00Z", 150, map[string]any{"custom": []any{map[string]any{"name": "Handlers without owner check", "value": 7.0}}})
	snap("m-new.json", "2026-03-01T00:00:00Z", 180, map[string]any{"loops": loop("failed", 12, 2, true), "custom": []any{map[string]any{"name": "Handlers without owner check", "value": 4.0}}})
	_ = os.WriteFile(filepath.Join(dir, "notes.json"), []byte(`{"not":"a snapshot"}`), 0o644)

	run := runFile{
		Metrics: []metricDef{
			{Name: "Code", Kind: "snapshot", Path: "size.totals_code_only.code", Unit: "lines", Better: "none"},
			{Name: "Handlers without owner check", Kind: "count", Pattern: "x", Globs: []string{"app/**/*.ts"}, Better: "down", Unit: "handlers"},
			{Name: "Passed", Kind: "snapshot", Path: "loops.loops[id=js-test].parsed.passed", Better: "up"},
			{Name: "Missing", Kind: "snapshot", Path: "nope.nothing"},
		},
		Headline: []any{"Handlers without owner check", "Code"},
	}
	res, err := buildScoreboard(run, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) != 3 || res.Snapshots[0].File != "z-old.json" || len(res.Skipped) != 1 {
		t.Errorf("snapshots = %+v skipped = %v", res.Snapshots, res.Skipped)
	}
	if got := []string{res.Rows[0].Metric, res.Rows[1].Metric, res.Rows[2].Metric, res.Rows[3].Metric}; strings.Join(got, "|") != "Handlers without owner check|Code|Passed|Missing" {
		t.Errorf("order = %v", got)
	}
	h := res.Rows[0]
	if !h.Headline || h.Value != 4.0 || h.First != 9.0 || len(h.Trend) != 3 || h.Trend[0] != 9 || h.Trend[2] != 4 || h.Better != "down" || !strings.Contains(h.How, "app/**/*.ts") || h.AsOf != "2026-03-01" {
		t.Errorf("handlers row = %+v", h)
	}
	c := res.Rows[1]
	if c.Value != 180.0 || c.First != 100.0 || c.Trend[1] != 150 || c.How != "snapshot field size.totals_code_only.code" {
		t.Errorf("code row = %+v", c)
	}
	p := res.Rows[2]
	if p.Headline || p.Value != 12.0 || p.First != 10.0 || len(p.Trend) != 2 {
		t.Errorf("passed row (middle snapshot has no loops) = %+v", p)
	}
	m := res.Rows[3]
	if m.Value != nil || m.First != nil || len(m.Trend) != 0 {
		t.Errorf("missing row = %+v", m)
	}
	b, _ := json.Marshal(res.Rows[3])
	if !strings.Contains(string(b), `"value":null`) || !strings.Contains(string(b), `"trend":[]`) {
		t.Errorf("json = %s", b)
	}

	// The default set when run.json has no metrics.
	def, err := buildScoreboard(runFile{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]sbRow{}
	for _, r := range def.Rows {
		rows[r.Metric] = r
	}
	for _, name := range []string{"Tests passed (js-test)", "Tests failed (js-test)", "Build (js-build)", "Flaky test loops", "Code lines (excluding docs and data)", "Direct dependencies", "Test files", "Revert commits, last 12 months", "CI configured"} {
		if _, ok := rows[name]; !ok {
			t.Errorf("default set lacks %q", name)
		}
	}
	if _, ok := rows["CI runs tests"]; ok {
		t.Error("CI runs tests should be omitted when no snapshot has ci_kinds_run")
	}
	if b := rows["Build (js-build)"]; b.Value != "fails" || b.First != "passes" || b.Trend[0] != 1 || b.Trend[1] != 0 {
		t.Errorf("build row = %+v", b)
	}
	if f := rows["Flaky test loops"]; f.Value != 1.0 || f.First != 0.0 {
		t.Errorf("flaky row = %+v", f)
	}
	if _, err := buildScoreboard(runFile{}, filepath.Join(dir, "nope")); err == nil {
		t.Error("a missing snapshot directory is an error")
	}
	empty, err := buildScoreboard(runFile{}, t.TempDir())
	if err != nil || len(empty.Snapshots) != 0 {
		t.Errorf("empty dir: %v %+v", err, empty)
	}
}

func TestFoldSmallest(t *testing.T) {
	root := &tmNode{Name: "r"}
	locs := map[string]int64{"a/x.go": 500, "a/y.go": 5, "a/z.go": 4, "b/p.go": 300, "b/q.go": 3, "c/only.go": 2}
	for p, l := range locs {
		insertLeaf(root, p, &tmNode{Name: p[strings.LastIndex(p, "/")+1:], Path: p, Loc: l, Churn: 1, leaf: true})
	}
	folded := foldSmallest(root, 4)
	count := 0
	var walk func(n *tmNode)
	var others []string
	walk = func(n *tmNode) {
		for _, c := range n.Children {
			if c.leaf {
				count++
				if strings.HasPrefix(c.Name, "other") {
					others = append(others, c.Path+"="+c.Name)
				}
			} else {
				walk(c)
			}
		}
	}
	walk(root)
	if count > 4 || folded < 3 {
		t.Errorf("leaves = %d folded = %d", count, folded)
	}
	joined := strings.Join(others, ",")
	if !strings.Contains(joined, "a/=other (2 files)") {
		t.Errorf("a's small files should fold into one other leaf: %s", joined)
	}
	if foldSmallest(&tmNode{Name: "x"}, 10) != 0 {
		t.Error("nothing to fold")
	}
	root.ChurnLabel = "commits touching the file in the last 180 days"
	b, _ := json.Marshal(root)
	if !strings.Contains(string(b), `"churnLabel":"commits touching the file in the last 180 days"`) {
		t.Errorf("root json = %s", b)
	}
}

func TestCheckReportJSONBlocks(t *testing.T) {
	good := `<script type="application/json" id="summary-data">{"facts":[1,2,3,4,5],"risks":[1,2,3,4,5],"actions":[1,2,3]}</script>
<script type="application/json" id="ok-data">{"a": 1}</script>`
	if r := checkReport(good, defaultAllowSet(), nil, false); !r.Passed {
		t.Errorf("good blocks failed: %+v", r.Failures)
	}
	bad := "<p>x</p>\n<script type=\"application/json\" id=\"scoreboard-data\">\n{ \"rows\": [1, 2,, 3] }\n</script>\n" +
		`<script type="application/json" id="summary-data">{"facts":[1,2,3],"risks":[1,2,3,4,5],"actions":[1,2,3,4]}</script>` +
		`<script>not json at all {</script>`
	r := checkReport(bad, defaultAllowSet(), nil, false)
	var msgs []string
	for _, f := range failuresOf(r, "embedded JSON data") {
		msgs = append(msgs, f.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"script#scoreboard-data does not parse", "line 2 of the block", "exactly 5 facts, has 3", "exactly 3 actions, has 4"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "risks") || len(msgs) != 3 {
		t.Errorf("messages = %v", msgs)
	}
}

func TestExportSubtreeSaysSo(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	for p, c := range map[string]string{"top.txt": "t", "svc/api/main.go": "package main\n", "svc/README.md": "x"} {
		fp := filepath.Join(repo, p)
		_ = os.MkdirAll(filepath.Dir(fp), 0o755)
		_ = os.WriteFile(fp, []byte(c), 0o644)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "init")
	dest := filepath.Join(t.TempDir(), "out")
	var so, se strings.Builder
	a := &app{stdout: &so, stderr: &se, now: timeNow}
	if code := a.run([]string{"export", filepath.Join(repo, "svc"), dest}); code != 0 {
		t.Fatalf("export: %s", se.String())
	}
	if !strings.Contains(so.String(), "only the subtree svc/") {
		t.Errorf("output should say it exported a subtree: %s", so.String())
	}
	got := walkFiles(dest)
	if strings.Join(got, ",") != "README.md,api/main.go" {
		t.Errorf("exported = %v", got)
	}
}
