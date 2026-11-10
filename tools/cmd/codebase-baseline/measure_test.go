package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyCommitAgents(t *testing.T) {
	cases := []struct {
		name string
		c    gitCommit
		want string
		bot  bool
	}{
		{"claude trailer", gitCommit{Author: "A", Email: "a@x.io", Body: "Fix x\n\nCo-Authored-By: Claude Opus 4 <noreply@anthropic.com>\n"}, "claude:co-author", false},
		{"copilot trailer", gitCommit{Author: "A", Email: "a@x.io", Body: "x\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"}, "copilot:co-author", false},
		{"cursor", gitCommit{Author: "A", Email: "a@x.io", Body: "x\n\nCo-authored-by: Cursor Agent <cursoragent@cursor.com>"}, "cursor:co-author", false},
		{"devin", gitCommit{Author: "A", Email: "a@x.io", Body: "x\n\nCo-authored-by: Devin AI <devin-ai-integration[bot]@users.noreply.github.com>"}, "devin:co-author", false},
		{"codex", gitCommit{Author: "A", Email: "a@x.io", Body: "Co-authored-by: Codex <codex@openai.com>"}, "codex_openai:co-author", false},
		{"gemini", gitCommit{Author: "A", Email: "a@x.io", Body: "Co-authored-by: Gemini Code Assist <gemini-code-assist[bot]@users.noreply.github.com>"}, "gemini:co-author", false},
		{"aider", gitCommit{Author: "A (aider)", Email: "aider@aider.chat", Body: "x"}, "aider:author", false},
		{"display name only", gitCommit{Author: "A", Email: "a@x.io", Body: "Co-authored-by: Claude <claude-fan@example.com>"}, "", false},
		{"bot co-author is no agent", gitCommit{Author: "A", Email: "a@x.io", Body: "Co-authored-by: dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>"}, "", false},
		{"footer", gitCommit{Author: "A", Email: "a@x.io", Body: "x\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)"}, "claude:footer", false},
		{"bot author", gitCommit{Author: "dependabot[bot]", Email: "49699333+dependabot[bot]@users.noreply.github.com", Body: "Bump x"}, "", true},
		{"human", gitCommit{Author: "Gemini Jones", Email: "gj@x.io", Body: "Co-authored-by: Sam Lee <sam@x.io>"}, "", false},
	}
	for _, c := range cases {
		agents, bot := classifyCommit(c.c)
		got := strings.Join(agents, ",")
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: agents = %q, want %q", c.name, got, c.want)
		}
		if bot != c.bot {
			t.Errorf("%s: bot = %v", c.name, bot)
		}
	}
}

func TestMeasureSnapshotOnTempRepo(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	write := func(rel, content string) {
		p := filepath.Join(repo, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"name":"x","dependencies":{"a":"1"},"devDependencies":{"b":"1"}}`)
	write("src/a.ts", "// TODO: x\nexport const a = 1\n")
	write("src/a.test.ts", "// FIXME later\ntest('a', () => {})\n")
	write(".github/workflows/ci.yml", "on: push\njobs:\n  t:\n    steps:\n      - run: npm test\n")
	write(".env.example", "DATABASE_URL=\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "Add: first", "-m", "Co-authored-by: Claude <noreply@anthropic.com>")
	write("src/b.ts", "export const b = 2\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "Fix: the bug")
	out := filepath.Join(t.TempDir(), "snap.json")
	filesPath := filepath.Join(t.TempDir(), "files.json")
	var stdout, stderr strings.Builder
	a := &app{stdout: &stdout, stderr: &stderr, now: timeNow}
	if code := a.run([]string{"measure", repo, "--out", out, "--files-out", filesPath, "--months", "3", "--as-of", "2099-01-01"}); code != 0 {
		t.Fatalf("measure: %s", stderr.String())
	}
	var snap snapshot
	b, _ := os.ReadFile(out)
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.SchemaVersion != 1 || snap.Tool.Version != version {
		t.Errorf("schema/tool = %d %+v", snap.SchemaVersion, snap.Tool)
	}
	if snap.Git.CommitsTotal != 2 || snap.Git.AgentCommitsTotal != 1 || snap.Git.FixCommits != 1 {
		t.Errorf("git = %+v", snap.Git)
	}
	if snap.Deps.DirectTotal != 2 || snap.Hygiene.Tests.Total != 1 || snap.Hygiene.Todos.Total != 2 {
		t.Errorf("deps=%d tests=%d todos=%d", snap.Deps.DirectTotal, snap.Hygiene.Tests.Total, snap.Hygiene.Todos.Total)
	}
	if len(snap.Hygiene.CI["github_actions"]) != 1 || len(snap.Hygiene.EnvExamples) != 1 {
		t.Errorf("hygiene = %+v", snap.Hygiene)
	}
	if snap.Size.Unavailable == "" && (snap.Size.Totals == nil || snap.Size.Totals.Files < 3) {
		t.Errorf("size = %+v", snap.Size)
	}
	if snap.Hygiene.SecretScan.Unavailable == "" && snap.Hygiene.SecretScan.Tool == "" {
		t.Errorf("secret scan = %+v", snap.Hygiene.SecretScan)
	}
	// Output inside the repo is refused.
	if code := a.run([]string{"measure", repo, "--out", filepath.Join(repo, "snap.json")}); code == 0 {
		t.Error("writing inside the analysed repo must be refused")
	}
	if _, err := os.Stat(filepath.Join(repo, "snap.json")); err == nil {
		t.Error("snapshot was written inside the repo")
	}
}

func TestDiffSnapshots(t *testing.T) {
	oldS := map[string]any{
		"schema_version": float64(1), "tool": map[string]any{"version": "0.1.0"}, "meta": map[string]any{"head_sha": "aaa"},
		"size":    map[string]any{"totals": map[string]any{"code": float64(100)}},
		"hygiene": map[string]any{"todo_markers": map[string]any{"total": float64(10)}, "ci": map[string]any{"github_actions": []any{"a.yml"}}},
		"deps":    map[string]any{"direct_total": float64(5), "manifests": []any{map[string]any{"path": "package.json", "direct": float64(5)}}},
		"loops":   map[string]any{"loops": []any{map[string]any{"id": "js-test", "status": "passed", "parsed": map[string]any{"passed": float64(10), "failed": float64(0)}}, map[string]any{"id": "js-lint", "status": "passed"}}},
	}
	newS := map[string]any{
		"schema_version": float64(1), "tool": map[string]any{"version": "0.2.0"}, "meta": map[string]any{"head_sha": "bbb"},
		"size":    map[string]any{"totals": map[string]any{"code": float64(150)}},
		"hygiene": map[string]any{"todo_markers": map[string]any{"total": float64(4)}, "ci": map[string]any{"github_actions": []any{"a.yml", "b.yml"}}},
		"deps":    map[string]any{"direct_total": float64(5), "manifests": []any{map[string]any{"path": "package.json", "direct": float64(5)}}},
		"loops":   map[string]any{"loops": []any{map[string]any{"id": "js-test", "status": "failed", "parsed": map[string]any{"passed": float64(9), "failed": float64(1)}}, map[string]any{"id": "js-build", "status": "passed"}}},
	}
	r := buildDiff(oldS, newS)
	if len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "different tool versions") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	by := map[string]metricDelta{}
	for _, d := range append(append([]metricDelta{}, r.Headline...), r.Metrics...) {
		by[d.Path] = d
	}
	if d := by["size.totals.code"]; d.Delta != 50 || d.Pct == nil || *d.Pct != 50 {
		t.Errorf("code delta = %+v", d)
	}
	if d := by["hygiene.todo_markers.total"]; d.Delta != -6 || d.Direction != "" {
		t.Errorf("todo delta = %+v", d)
	}
	if _, ok := by["deps.direct_total"]; ok && by["deps.direct_total"].Delta != 0 {
		t.Errorf("unchanged dependency count reported as changed")
	}
	if len(r.Sets) != 1 || r.Sets[0].Path != "hygiene.ci.github_actions" || r.Sets[0].Added[0] != "b.yml" {
		t.Errorf("sets = %+v", r.Sets)
	}
	changes := map[string]loopChange{}
	for _, c := range r.Loops {
		changes[c.ID] = c
	}
	if c := changes["js-test"]; c.Change != "status" || c.Old != "passed" || c.New != "failed" || !strings.Contains(strings.Join(c.Details, ";"), "failed 0 -> 1") {
		t.Errorf("js-test = %+v", c)
	}
	if changes["js-lint"].Change != "removed" || changes["js-build"].Change != "added" {
		t.Errorf("loops = %+v", changes)
	}
	var sb strings.Builder
	renderDiffMD(&sb, r)
	if !strings.Contains(sb.String(), "WARNING") || !strings.Contains(sb.String(), "`size.totals.code`") {
		t.Errorf("markdown:\n%s", sb.String())
	}
}

func TestAuthorSection(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	commit := func(author, file, content string) {
		_ = os.WriteFile(filepath.Join(repo, file), []byte(content), 0o644)
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "-c", "user.name="+author, "-c", "user.email="+strings.ToLower(author)+"@x.io", "commit", "-q", "--author="+author+" <"+strings.ToLower(author)+"@x.io>", "-m", "change "+file)
	}
	commit("Base", "a.txt", "1\n")
	sha1, _ := git(repo, "rev-parse", "HEAD")
	commit("Jonas", "b.txt", "1\n2\n3\n")
	commit("Other", "c.txt", "1\n")
	commit("Jonas", "b.txt", "1\n2\n3\n4\n")
	sha2, _ := git(repo, "rev-parse", "HEAD")
	s := authorSectionFor("jonas", repo, strings.TrimSpace(sha1), strings.TrimSpace(sha2))
	if s.Unavailable != "" || s.CommitsInRange != 3 || s.Commits != 2 || s.FilesTouched != 1 || s.Added != 4 {
		t.Errorf("author section = %+v", s)
	}
}

func TestHotspotsCollapse(t *testing.T) {
	root := &tmNode{Name: "repo"}
	for _, p := range []string{"src/main/java/com/App.java", "src/main/java/com/Util.java", "lib/only/one.go", "top.go"} {
		insertLeaf(root, p, &tmNode{Name: p[strings.LastIndex(p, "/")+1:], Path: p, leaf: true})
	}
	collapse(root, true)
	sortTree(root)
	var names []string
	for _, c := range root.Children {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "lib/only/one.go,src/main/java/com,top.go" {
		t.Errorf("collapsed children = %s", got)
	}
	b, err := json.Marshal(root.Children[0])
	if err != nil || !strings.Contains(string(b), `"path":"lib/only/one.go"`) || !strings.Contains(string(b), `"loc":0`) {
		t.Errorf("leaf json = %s %v", b, err)
	}
}
