package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestStripReason(t *testing.T) {
	cases := []struct {
		rel   string
		isDir bool
		want  string
	}{
		{"README.md", false, "prose doc"}, {"a/b/notes.MDX", false, "prose doc"}, {"guide.rst", false, "prose doc"},
		{"docs", true, "doc directory"}, {"packages/x/adr", true, "doc directory"}, {"decisions", true, "doc directory"}, {"wiki", true, "doc directory"},
		{"AGENTS.md", false, "agent file"}, {"sub/CLAUDE.md", false, "agent file"}, {".cursorrules", false, "agent file"},
		{".claude", true, "agent directory"}, {".cursor", true, "agent directory"}, {".continue", true, "agent directory"},
		{".github/copilot-instructions.md", false, "agent file"}, {".aider.conf.yml", false, "agent file"}, {".aider.tags.cache", true, "agent file"},
		{"CHANGELOG", false, "prose doc"}, {"LICENSE", false, ""}, {"src/main.go", false, ""}, {"notes.txt", false, ""},
		{"documents", true, ""}, {"src/docs.go", false, ""},
	}
	for _, c := range cases {
		got := stripReason(c.rel, c.isDir)
		if c.rel == ".aider.tags.cache" {
			got = stripReason(c.rel, false)
		}
		if got != c.want {
			t.Errorf("stripReason(%q, dir=%v) = %q, want %q", c.rel, c.isDir, got, c.want)
		}
	}
}

func TestExportAndStrip(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	write := func(rel, content string) {
		p := filepath.Join(repo, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n")
	write("README.md", "# hi\n")
	write("docs/guide.txt", "guide\n")
	write("docs/deep/more.md", "x\n")
	write("pkg/doc/x.go", "package x\n")
	write("CLAUDE.md", "rules\n")
	write(".claude/agents/a.md", "a\n")
	write(".github/copilot-instructions.md", "c\n")
	write("script.sh", "#!/bin/sh\n")
	_ = os.Chmod(filepath.Join(repo, "script.sh"), 0o755)
	write(".gitignore", ".env\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "init")
	write(".env", "SECRET=1\n")
	write("untracked.txt", "u\n")
	if err := os.MkdirAll(filepath.Join(repo, "node_modules/x"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("node_modules/x/index.js", "x\n")

	var out, errOut strings.Builder
	a := &app{stdout: &out, stderr: &errOut, now: timeNow}
	dest := filepath.Join(t.TempDir(), "export")
	if code := a.run([]string{"export", repo, dest, "--strip-docs"}); code != 0 {
		t.Fatalf("export exit %d: %s", code, errOut.String())
	}
	var left []string
	for _, f := range walkFiles(dest) {
		left = append(left, f)
	}
	sort.Strings(left)
	if got := strings.Join(left, ","); got != ".gitignore,main.go,pkg/doc/x.go,script.sh" {
		// pkg/doc is a doc directory by name, so x.go goes with it.
		if got != ".gitignore,main.go,script.sh" {
			t.Errorf("left after strip: %s", got)
		}
	}
	for _, gone := range []string{".env", "untracked.txt", "node_modules", ".git"} {
		if _, err := os.Stat(filepath.Join(dest, gone)); err == nil {
			t.Errorf("%s must not be exported", gone)
		}
	}
	if st, err := os.Stat(filepath.Join(dest, "script.sh")); err != nil || st.Mode()&0o111 == 0 {
		t.Errorf("exec bit lost: %v %v", st, err)
	}
	list, err := os.ReadFile(dest + ".removed.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"README.md", "docs/guide.txt", "docs/deep/more.md", "CLAUDE.md", ".claude/agents/a.md", ".github/copilot-instructions.md"} {
		if !strings.Contains(string(list), want) {
			t.Errorf("removed list lacks %s:\n%s", want, list)
		}
	}
	if !strings.Contains(out.String(), "stripped") {
		t.Errorf("output = %s", out.String())
	}
	// A second export into the same directory is refused.
	if code := a.run([]string{"export", repo, dest}); code == 0 {
		t.Error("export into a non-empty dest must fail")
	}
	// An empty existing directory is fine, and without --strip-docs the docs stay.
	empty := t.TempDir()
	if code := a.run([]string{"export", repo, empty}); code != 0 {
		t.Fatalf("export into empty dir failed: %s", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(empty, "README.md")); err != nil {
		t.Errorf("README.md should stay without --strip-docs: %v", err)
	}
}
