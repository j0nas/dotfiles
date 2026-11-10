package main

import (
	"os"
	"testing"
)

func TestParseGitHubWorkflow(t *testing.T) {
	b, err := os.ReadFile("testdata/ci/github.yml")
	if err != nil {
		t.Fatal(err)
	}
	steps := parseGitHubWorkflow(".github/workflows/ci.yml", string(b))
	type want struct {
		line int
		cmd  string
		kind string
		job  string
	}
	wants := []want{
		{18, "pnpm install --frozen-lockfile", "install", "test"},
		{22, "pnpm lint", "lint", "test"},
		{23, "pnpm exec tsc --noEmit", "typecheck", "test"},
		{25, "pnpm test", "test", "test"},
		{29, "pnpm run test:e2e", "e2e", "e2e"},
	}
	if len(steps) != len(wants) {
		t.Fatalf("got %d steps: %+v", len(steps), steps)
	}
	for i, w := range wants {
		s := steps[i]
		if s.Line != w.line || s.Cmd != w.cmd || s.Kind != w.kind || s.Job != w.job {
			t.Errorf("step %d = %+v, want %+v", i, s, w)
		}
	}
	if steps[1].WorkingDir != "packages/api" {
		t.Errorf("working dir = %q", steps[1].WorkingDir)
	}
	if len(steps[0].Services) != 1 || steps[0].Services[0] != "postgres" {
		t.Errorf("services = %v", steps[0].Services)
	}
	if !contains(steps[0].EnvNames, "DATABASE_URL") {
		t.Errorf("env names = %v", steps[0].EnvNames)
	}
	if len(steps[4].Services) != 0 {
		t.Errorf("e2e job has no services: %v", steps[4].Services)
	}
}

func TestParseGitLabCI(t *testing.T) {
	b, err := os.ReadFile("testdata/ci/gitlab-ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	steps := parseGitLabCI(".gitlab-ci.yml", string(b))
	var cmds []string
	for _, s := range steps {
		cmds = append(cmds, s.Cmd)
	}
	want := []string{"pip install -r requirements.txt", "pytest -q", "ruff check .", "mypy .", "ruff check ."}
	if len(cmds) != len(want) {
		t.Fatalf("cmds = %v", cmds)
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Errorf("cmd %d = %q, want %q", i, cmds[i], want[i])
		}
	}
	if steps[1].Job != "unit" || len(steps[1].Services) != 1 || steps[1].Services[0] != "redis:7" {
		t.Errorf("unit step = %+v", steps[1])
	}
	if steps[1].Line != 11 {
		t.Errorf("line = %d", steps[1].Line)
	}
}

func TestClassifyCmdKind(t *testing.T) {
	for cmd, want := range map[string]string{
		"npm ci": "install", "pnpm install --frozen-lockfile": "install", "npx playwright test": "e2e",
		"go test ./...": "test", "cargo clippy -- -D warnings": "lint", "tsc --noEmit": "typecheck",
		"prettier --check .": "format", "next build": "build", "echo hi": "other",
	} {
		if got := classifyCmdKind(cmd); got != want {
			t.Errorf("classifyCmdKind(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestDiscoverMarksLoopsRunInCI(t *testing.T) {
	// Copy the CI fixture into a repo-shaped tree.
	dir := t.TempDir()
	write := func(rel, content string) {
		p := dir + "/" + rel
		if err := os.MkdirAll(dir+"/.github/workflows", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile("testdata/ci/github.yml")
	write(".github/workflows/ci.yml", string(b))
	pkg, _ := os.ReadFile("testdata/js-pnpm/package.json")
	write("package.json", string(pkg))
	write("pnpm-lock.yaml", "x")
	res := discoverLoops(dir, timeNow())
	for _, id := range []string{"js-lint", "js-test", "js-install"} {
		if !findLoop(t, res, id).InCI {
			t.Errorf("%s should be marked as run in CI", id)
		}
	}
	if findLoop(t, res, "js-build").InCI {
		t.Error("build is not run in CI")
	}
	// The CI job that runs the tests has a postgres service: the test loop needs a database.
	if l := findLoop(t, res, "js-test"); !contains(l.Needs, "database") || l.Enabled {
		t.Errorf("js-test = %+v", l)
	}
	if !contains(res.CICoverage.KindsNotRun, "build") || !contains(res.CICoverage.KindsRun, "lint") {
		t.Errorf("coverage = %+v", res.CICoverage)
	}
}
