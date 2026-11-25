package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local)

// fixture is a projects root, home, vault and Claude folder in temp dirs, so
// tests never read or write the real ones.
type fixture struct {
	t                            *testing.T
	root, home, vault, claudeDir string
	env                          map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := realpath(t, t.TempDir())
	f := &fixture{
		t:         t,
		root:      filepath.Join(base, "projects"),
		home:      filepath.Join(base, "home"),
		vault:     filepath.Join(base, "vault"),
		claudeDir: filepath.Join(base, "home", ".claude"),
	}
	f.env = map[string]string{
		"HOME":                f.home,
		"PROJECT_BOARD_ROOT":  f.root,
		"PROJECT_BOARD_VAULT": f.vault,
	}
	for _, d := range []string{f.root, f.home, f.vault} {
		mkdir(t, d)
	}
	return f
}

func (f *fixture) app(cwd string) (*app, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &app{
		stdout:  &out,
		stderr:  &errOut,
		getenv:  func(k string) string { return f.env[k] },
		cwd:     cwd,
		now:     func() time.Time { return fixedNow },
		openURL: func(string) error { return nil },
	}, &out, &errOut
}

func (f *fixture) run(cwd string, args ...string) (string, string, int) {
	f.t.Helper()
	a, out, errOut := f.app(cwd)
	code := a.run(args)
	return out.String(), errOut.String(), code
}

func (f *fixture) mustRun(cwd string, args ...string) string {
	f.t.Helper()
	out, errOut, code := f.run(cwd, args...)
	if code != 0 {
		f.t.Fatalf("project-board %v: exit %d: %s", args, code, errOut)
	}
	return out
}

func (f *fixture) config() *config {
	a, _, _ := f.app(f.root)
	return a.config()
}

// project makes <root>/<id> with the given files (path → content).
func (f *fixture) project(id string, files map[string]string) string {
	f.t.Helper()
	dir := filepath.Join(f.root, filepath.FromSlash(id))
	mkdir(f.t, dir)
	for name, content := range files {
		write(f.t, filepath.Join(dir, name), content)
	}
	return dir
}

func (f *fixture) registry(content string) {
	f.t.Helper()
	write(f.t, filepath.Join(f.root, "projects.json"), content)
}

func (f *fixture) board() *board {
	f.t.Helper()
	b, err := f.config().buildBoard()
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (b *board) card(t *testing.T, id string) *card {
	t.Helper()
	for _, c := range b.Projects {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no card %s on the board", id)
	return nil
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func realpath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegistryRoundTripKeepsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	write(t, path, `{
  "version": 1,
  "owner": {"name": "Jonas"},
  "projects": [
    {"id": "code/b", "name": "b", "status": "live", "color": "#421691", "links": {"doc": "HANDOFF.md"}},
    {"id": "code/a", "name": "a", "status": "idea", "priority": 3, "nested": {"x": [1, 2]}, "summary": "<b> & co"}
  ]
}`)
	reg, err := loadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.find("code/a").Next = "write tests"
	if err := reg.save(path); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	for _, want := range []string{`"owner": {`, `"priority": 3`, `"color": "#421691"`, `"x": [`, `"doc": "HANDOFF.md"`, `"next": "write tests"`, `"summary": "<b> & co"`} {
		if !strings.Contains(got, want) {
			t.Errorf("saved registry lacks %s:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "}\n") || !strings.Contains(got, "\n  \"projects\": [\n    {\n      \"id\": \"code/a\"") {
		t.Errorf("not 2-space indented, sorted by id, with a trailing newline:\n%s", got)
	}
	if strings.Index(got, `"code/a"`) > strings.Index(got, `"code/b"`) {
		t.Error("projects not sorted by id")
	}
	// A second round trip changes nothing.
	reg, err = loadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.save(path); err != nil {
		t.Fatal(err)
	}
	if again := read(t, path); again != got {
		t.Errorf("second round trip changed the file:\n%s\nvs\n%s", got, again)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*tmp*")); len(leftovers) > 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestMissingRegistryIsEmpty(t *testing.T) {
	reg, err := loadRegistry(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(reg.Projects) != 0 {
		t.Fatalf("got %v, %v", reg, err)
	}
}

func TestApplyPatch(t *testing.T) {
	p := &project{ID: "code/x", Name: "x", Status: "idea", Summary: "old", Tags: []string{"a"}}
	err := applyPatch(p, map[string]json.RawMessage{
		"status":     json.RawMessage(`"active"`),
		"next":       json.RawMessage(`"  ship it  "`),
		"summary":    json.RawMessage(`null`),
		"open_loops": json.RawMessage(`["one", "", "one", "two"]`),
		"pinned":     json.RawMessage(`true`),
		"links":      json.RawMessage(`{"live": "https://x.example", "repo": ""}`),
		"facts":      json.RawMessage(`{"ignored": true}`),
		"id":         json.RawMessage(`"code/x"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "active" || p.Next != "ship it" || p.Summary != "" || !p.Pinned {
		t.Errorf("fields not set: %+v", p)
	}
	if !slices.Equal(p.OpenLoops, []string{"one", "two"}) {
		t.Errorf("open_loops = %q", p.OpenLoops)
	}
	if len(p.Links) != 1 || p.Links["live"] != "https://x.example" {
		t.Errorf("links = %v", p.Links)
	}
	for name, patch := range map[string]string{
		"bad status":  `{"status": "someday"}`,
		"empty name":  `{"name": ""}`,
		"wrong type":  `{"tags": "web"}`,
		"unknown key": `{"colour": "red"}`,
		"other id":    `{"id": "code/y"}`,
	} {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(patch), &m); err != nil {
			t.Fatal(err)
		}
		if err := applyPatch(&project{ID: "code/x", Name: "x", Status: "idea"}, m); err == nil {
			t.Errorf("%s: %s accepted", name, patch)
		}
	}
}

func TestResolve(t *testing.T) {
	f := newFixture(t)
	edh := f.project("code/edh.land", map[string]string{"src/app/main.go": "package main"})
	f.project("code/blocks", nil)
	f.project("3d/blocks", nil)
	f.project("home/music", nil)
	f.project(".hidden/x", nil)
	f.project("llm-sessions/x", nil)
	f.registry(`{"version": 1, "projects": [
		{"id": "code/edh.land", "name": "EDH Land", "status": "live"},
		{"id": "cut/gone", "name": "gone", "status": "dropped"}
	]}`)
	c := f.config()
	reg, err := loadRegistry(c.registry)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		cwd, arg, want string
	}{
		{f.root, "code/edh.land", "code/edh.land"},
		{f.root, "edh.land", "code/edh.land"},
		{f.root, "EDH Land", "code/edh.land"},
		{f.root, "edh land", "code/edh.land"}, // no exact match: case-insensitive name
		{f.root, "music", "home/music"},
		{f.root, "cut/gone", "cut/gone"}, // registry entry, folder missing
		{f.root, "gone", "cut/gone"},
		{edh, "", "code/edh.land"},
		{edh, ".", "code/edh.land"},
		{filepath.Join(edh, "src", "app"), "", "code/edh.land"},
		{filepath.Join(edh, "src", "app"), "..", "code/edh.land"},
		{f.root, edh, "code/edh.land"},
		{f.root, filepath.Join(edh, "src"), "code/edh.land"},
		{filepath.Join(f.root, "code"), "edh.land/src", "code/edh.land"},
	}
	for _, tc := range cases {
		a, _, _ := f.app(tc.cwd)
		got, err := a.resolve(c, reg, tc.arg)
		if err != nil || got != tc.want {
			t.Errorf("resolve(%q) in %s = %q, %v; want %q", tc.arg, tc.cwd, got, err, tc.want)
		}
	}
	bad := []struct{ cwd, arg string }{
		{f.root, "blocks"},                          // ambiguous: code/blocks and 3d/blocks
		{f.root, "nope"},                            // nothing
		{f.root, ""},                                // the root isn't a project
		{filepath.Join(f.root, "code"), ""},         // a category isn't one
		{t.TempDir(), ""},                           // outside the root
		{filepath.Join(f.root, ".hidden", "x"), ""}, // dot folders aren't projects
		{f.root, "llm-sessions/x"},
	}
	for _, tc := range bad {
		a, _, _ := f.app(tc.cwd)
		if got, err := a.resolve(c, reg, tc.arg); err == nil {
			t.Errorf("resolve(%q) in %s = %q, want an error", tc.arg, tc.cwd, got)
		}
	}
}

func TestWarnings(t *testing.T) {
	days := func(n int) *int { return &n }
	cases := []struct {
		name string
		card card
		want []string
	}{
		{"untriaged", card{Untriaged: true, Facts: facts{Exists: true}}, []string{"untriaged"}},
		{"missing", card{Status: "live", Facts: facts{}}, []string{"missing"}},
		{"dirty and ahead", card{Status: "live", Facts: facts{Exists: true, Git: &gitFacts{Dirty: 2, Ahead: 1, Upstream: true, LastCommit: &commit{}}}}, []string{"uncommitted", "unpushed"}},
		{"commits, no upstream", card{Status: "live", Facts: facts{Exists: true, Git: &gitFacts{LastCommit: &commit{}}}}, []string{"unpushed"}},
		{"no commits, no upstream", card{Status: "live", Facts: facts{Exists: true, Git: &gitFacts{}}}, []string{}},
		{"stale", card{Status: "active", Next: "x", Facts: facts{Exists: true, DaysIdle: days(22)}}, []string{"stale"}},
		{"three weeks is not stale", card{Status: "active", Next: "x", Facts: facts{Exists: true, DaysIdle: days(21)}}, []string{}},
		{"paused is never stale", card{Status: "paused", Next: "x", Facts: facts{Exists: true, DaysIdle: days(200)}}, []string{}},
		{"no next", card{Status: "waiting", Facts: facts{Exists: true}}, []string{"no-next"}},
		{"vault next counts", card{Status: "paused", Facts: facts{Exists: true, Vault: &vaultFacts{NextAction: "do it"}}}, []string{}},
		{"done needs no next", card{Status: "done", Facts: facts{Exists: true}}, []string{}},
	}
	for _, tc := range cases {
		if got := warnings(&tc.card); !slices.Equal(got, tc.want) {
			t.Errorf("%s: warnings = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSnapshotEscapesScriptClose(t *testing.T) {
	tmpl := []byte("<html><body>" + placeholder + "</body></html>")
	b := &board{Generated: "now", Projects: []*card{{ID: "code/x", Name: "</script><script>alert(1)</script>", Summary: "a & b <!--"}}}
	page, err := snapshot(tmpl, b)
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if strings.Count(s, "</script>") != 1 {
		t.Errorf("data broke out of the script element:\n%s", s)
	}
	if strings.Contains(s, "<!--") || strings.Contains(s, ">null<") {
		t.Errorf("unescaped < or placeholder left:\n%s", s)
	}
	start := strings.Index(s, `application/json">`) + len(`application/json">`)
	end := strings.Index(s, "</script>")
	var back board
	if err := json.Unmarshal([]byte(s[start:end]), &back); err != nil {
		t.Fatalf("inlined data isn't JSON: %v", err)
	}
	if back.Projects[0].Name != b.Projects[0].Name {
		t.Errorf("name came back as %q", back.Projects[0].Name)
	}
	if _, err := snapshot([]byte("<html></html>"), b); err == nil {
		t.Error("a template without the placeholder was accepted")
	}
	if !bytes.Contains(boardHTML, []byte(placeholder)) {
		t.Error("the embedded board.html lacks the snapshot placeholder")
	}
}

func TestParseIndex(t *testing.T) {
	idx := `# Session log

One line per Claude Code session, newest first.

- 2026-11-19 [Tree: the \[+\] on touch](2026-11-19-tree.md): Touch screens get the +. Open: why.
- 2026-11-15 [Fold switches](2026-11-15-fold.md): Big decks fold.
- 2026-11-13 [Baseline](2026-11-13-baseline.md):
- a line someone typed by hand
`
	got := parseIndex([]byte(idx))
	if len(got) != 4 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	want := logEntry{Date: "2026-11-19", Title: "Tree: the [+] on touch", Summary: "Touch screens get the +. Open: why.", File: "2026-11-19-tree.md"}
	if got[0] != want {
		t.Errorf("first entry = %+v, want %+v", got[0], want)
	}
	if got[2].Summary != "" || got[2].Title != "Baseline" {
		t.Errorf("entry without summary = %+v", got[2])
	}
	if got[3].Title != "a line someone typed by hand" || got[3].Date != "" {
		t.Errorf("free-form line = %+v", got[3])
	}
}

func TestOpenNotes(t *testing.T) {
	session := `---
title: "x"
---

## Decisions

- not this one

## Open

- First open item
  that wraps onto a second line.
- Second item

Some paragraph that isn't a bullet.

* Third, star bullet

## Learnings

- not this either

## 2026-11-07, resumed

### Open

- Added on resume
- Second item
`
	got := openNotes([]byte(session))
	want := []string{"First open item that wraps onto a second line.", "Second item", "Third, star bullet", "Added on resume"}
	if !slices.Equal(got, want) {
		t.Errorf("openNotes = %q\nwant %q", got, want)
	}
	if got := openNotes([]byte("## Decisions\n\n- x\n")); len(got) != 0 {
		t.Errorf("no Open section gave %q", got)
	}
}

func TestLogFacts(t *testing.T) {
	f := newFixture(t)
	f.project("code/x", map[string]string{
		"llm-sessions/INDEX.md":        "# Session log\n\n- 2026-11-19 [B](2026-11-19-b.md): b.\n- 2026-11-18 [A](2026-11-18-a.md): a.\n",
		"llm-sessions/2026-11-19-b.md": "## Open\n\n- newest open\n",
		"llm-sessions/2026-11-18-a.md": "## Open\n\n- older open\n",
	})
	f.project("code/noindex", map[string]string{
		"llm-sessions/2026-10-01-old.md": "## Open\n\n- old\n",
		"llm-sessions/2026-10-02-new.md": "## Open\n\n- new\n",
	})
	b := f.board()
	l := b.card(t, "code/x").Facts.Log
	if l == nil || l.Entries != 2 || len(l.Recent) != 2 || l.Recent[0].Title != "B" || !slices.Equal(l.Open, []string{"newest open"}) {
		t.Errorf("log = %+v", l)
	}
	if l := b.card(t, "code/noindex").Facts.Log; l == nil || l.Entries != 0 || !slices.Equal(l.Open, []string{"new"}) {
		t.Errorf("log without INDEX.md = %+v", l)
	}
}

func TestVaultLinking(t *testing.T) {
	f := newFixture(t)
	kindle := f.project("electronics/kindle", nil)
	f.project("electronics/blinkeball", nil)
	f.project("home/house", nil)
	f.project("home/other", nil)
	write(t, filepath.Join(f.vault, "Projects", "Kindle.md"), "---\nfolder: ~/../"+filepath.Base(f.home)+"/../projects/electronics/kindle\nsessions:\n  - abc\n---\n# Kindle\n\n- [ ] Charge each unit\n- [x] Done already\n  - [ ] Nested task\n- not a task\n")
	write(t, filepath.Join(f.vault, "Archive", "Old", "Blink.md"), "---\nfolder: \""+filepath.Join(f.root, "electronics", "blinkeball")+"/\"\n---\n- [ ] Cut one open\n")
	write(t, filepath.Join(f.vault, "Projects", "Unlinked.md"), "---\nfolder: ~/elsewhere\n---\n- [ ] nope\n")
	write(t, filepath.Join(f.vault, "Someday", "House.md"), "# House\n\n- [ ] Top task\n\n## Kitchen\n\n- [ ] Paint\n### Details\n- [ ] Buy brushes\n\n## Garden\n\n- [ ] Mow\n")
	f.registry(`{"version": 1, "projects": [
		{"id": "home/house", "name": "house", "status": "paused", "links": {"vault": "Someday/House.md#Kitchen"}},
		{"id": "home/other", "name": "other", "status": "paused", "links": {"vault": "Someday/House"}},
		{"id": "electronics/kindle", "name": "Kindle", "status": "active", "next": "x"}
	]}`)
	b := f.board()

	v := b.card(t, "electronics/kindle").Facts.Vault
	if v == nil {
		t.Fatalf("kindle not linked (folder %s)", kindle)
	}
	if v.Note != "Projects/Kindle.md" || !slices.Equal(v.OpenTasks, []string{"Charge each unit", "Nested task"}) || v.NextAction != "Charge each unit" {
		t.Errorf("kindle vault = %+v", v)
	}
	wantURL := "obsidian://open?vault=vault&file=Projects%2FKindle"
	if v.URL != wantURL {
		t.Errorf("url = %s, want %s", v.URL, wantURL)
	}
	if v := b.card(t, "electronics/blinkeball").Facts.Vault; v == nil || v.Note != "Archive/Old/Blink.md" {
		t.Errorf("quoted absolute folder with trailing slash not linked: %+v", v)
	}
	if v := b.card(t, "home/house").Facts.Vault; v == nil || v.Heading != "Kitchen" || !slices.Equal(v.OpenTasks, []string{"Paint", "Buy brushes"}) {
		t.Errorf("links.vault with heading = %+v", v)
	}
	if v := b.card(t, "home/other").Facts.Vault; v == nil || v.Note != "Someday/House.md" || len(v.OpenTasks) != 4 {
		t.Errorf("links.vault without .md = %+v", v)
	}
	if got := obsidianURL("Jonas' vault", "Projects/Finance & benefits.md"); got != "obsidian://open?vault=Jonas%27%20vault&file=Projects%2FFinance%20%26%20benefits" {
		t.Errorf("obsidianURL = %s", got)
	}

	// No vault: skipped silently.
	f.env["PROJECT_BOARD_VAULT"] = filepath.Join(t.TempDir(), "absent")
	if v := f.board().card(t, "electronics/kindle").Facts.Vault; v != nil {
		t.Errorf("absent vault gave %+v", v)
	}
}

func TestFrontmatter(t *testing.T) {
	fm := frontmatter([]byte("---\r\nfolder: '~/x y'\r\nsessions:\r\n  - a: b\r\ntitle: \"T\"\r\n---\r\nbody\r\n"))
	if fm["folder"] != "~/x y" || fm["title"] != "T" || len(fm) != 3 {
		t.Errorf("frontmatter = %v", fm)
	}
	if len(frontmatter([]byte("# no frontmatter\nfolder: x\n"))) != 0 {
		t.Error("parsed frontmatter where there is none")
	}
}

func TestClaudeFacts(t *testing.T) {
	f := newFixture(t)
	dir := f.project("3d/gåre sign", nil)
	if got, want := claudeSlug("/Users/jonas/Desktop/projects/3d/gåre-mailbox-sign"), "-Users-jonas-Desktop-projects-3d-g-re-mailbox-sign"; got != want {
		t.Errorf("claudeSlug = %s, want %s", got, want)
	}
	cdir := filepath.Join(f.claudeDir, "projects", claudeSlug(dir))
	write(t, filepath.Join(cdir, "a.jsonl"), "{}")
	write(t, filepath.Join(cdir, "b.jsonl"), "{}")
	write(t, filepath.Join(cdir, "notes.txt"), "")
	write(t, filepath.Join(cdir, "sub", "c.jsonl"), "{}")
	mkdir(t, filepath.Join(cdir, "memory"))
	newest := fixedNow.Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(cdir, "b.jsonl"), newest, newest); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(cdir, "a.jsonl"), newest.Add(-time.Hour), newest.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	cd := f.board().card(t, "3d/gåre sign")
	cl := cd.Facts.Claude
	if cl == nil || cl.Sessions != 2 || !cl.Memory || cl.Last != newest.Format(time.RFC3339) {
		t.Errorf("claude = %+v", cl)
	}
	if cd.Facts.DaysIdle == nil || *cd.Facts.DaysIdle != 0 {
		t.Errorf("days_idle = %v", cd.Facts.DaysIdle)
	}
}

func TestWalkSkipsBuildOutput(t *testing.T) {
	f := newFixture(t)
	dir := f.project("cut/tray", map[string]string{
		"tray.svg":                 "<svg/>",
		"node_modules/x/index.js":  "",
		"dist/out.js":              "",
		".DS_Store":                "",
		"parts/sub/deep/notes.txt": "",
	})
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	deep := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	filepath.WalkDir(dir, func(p string, _ os.DirEntry, _ error) error {
		os.Chtimes(p, old, old)
		return nil
	})
	os.Chtimes(filepath.Join(dir, "parts/sub/deep/notes.txt"), deep, deep)
	recent := time.Now()
	for _, skipped := range []string{"node_modules/x/index.js", "dist/out.js", ".DS_Store"} {
		os.Chtimes(filepath.Join(dir, skipped), recent, recent)
	}
	if got := newestFile(dir, walkCap); !got.Equal(deep) {
		t.Errorf("newest = %v, want %v (skipped folders and .DS_Store are touched now)", got, deep)
	}
	empty := f.project("cut/empty", nil)
	os.Chtimes(empty, old, old)
	if cd := f.board().card(t, "cut/empty"); cd.Facts.LastActivity != old.Format(time.RFC3339) {
		t.Errorf("empty folder last_activity = %q", cd.Facts.LastActivity)
	}
}

func TestParseStatus(t *testing.T) {
	out := strings.Join([]string{
		"# branch.oid 0123",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +2 -1",
		"1 .M N... 100644 100644 100644 abc abc src/with space.go",
		"2 R. N... 100644 100644 100644 abc abc R100 new.go", "old.go",
		"u UU N... 100644 100644 100644 100644 a b c conflict.go",
		"? untracked/",
		"",
	}, "\x00")
	st := parseStatus([]byte(out))
	if st.branch != "main" || !st.upstream || st.ahead != 2 || st.behind != 1 {
		t.Errorf("branch info = %+v", st)
	}
	if want := []string{"src/with space.go", "new.go", "conflict.go", "untracked/"}; !slices.Equal(st.paths, want) {
		t.Errorf("paths = %q, want %q", st.paths, want)
	}
}

func TestHTTPSRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:j0nas/dotfiles.git":        "https://github.com/j0nas/dotfiles",
		"ssh://git@github.com/j0nas/x.git":         "https://github.com/j0nas/x",
		"https://github.com/j0nas/y.git":           "https://github.com/j0nas/y",
		"git@gitlab.com:someone/z.git":             "git@gitlab.com:someone/z.git",
		"https://dev.azure.com/org/_git/acme-app": "https://dev.azure.com/org/_git/acme-app",
	} {
		if got := httpsRemote(in); got != want {
			t.Errorf("httpsRemote(%s) = %s, want %s", in, got, want)
		}
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	base := []string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}
	if out, err := gitCmd(dir, append(base, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitFacts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// validate runs the tests from the pre-commit hook, where GIT_DIR points at
	// the dotfiles repo; the scan has to ignore it.
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_INDEX_FILE", "/nonexistent")
	f := newFixture(t)
	repo := f.project("code/repo", map[string]string{"README.md": "# repo\n"})
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "first commit")
	git(t, repo, "remote", "add", "origin", "git@github.com:j0nas/repo.git")
	write(t, filepath.Join(repo, "new.txt"), "x")
	write(t, filepath.Join(repo, "README.md"), "# changed\n")
	nested := f.project("code/repo-sub", nil)
	mkdir(t, filepath.Join(nested, "inner"))
	git(t, filepath.Join(nested, "inner"), "init", "-q") // a repo below the folder isn't the folder's

	b := f.board()
	g := b.card(t, "code/repo").Facts.Git
	if g == nil {
		t.Fatal("no git facts")
	}
	if g.Branch != "main" || g.Dirty != 2 || g.Upstream || g.Remote != "https://github.com/j0nas/repo" || g.Commits30d != 1 {
		t.Errorf("git = %+v", g)
	}
	if g.LastCommit == nil || g.LastCommit.Subject != "first commit" || len(g.Recent) != 1 {
		t.Errorf("commits = %+v %+v", g.LastCommit, g.Recent)
	}
	if w := b.card(t, "code/repo").Warnings; !slices.Equal(w, []string{"untriaged", "uncommitted", "unpushed"}) {
		t.Errorf("warnings = %q", w)
	}
	if g := b.card(t, "code/repo-sub").Facts.Git; g != nil {
		t.Errorf("folder that isn't a work tree top got git facts: %+v", g)
	}
	if docs := b.card(t, "code/repo").Facts.Docs; !slices.Equal(docs, []string{"README.md"}) {
		t.Errorf("docs = %q", docs)
	}
}

// A category that is one repo: each project in it gets its own changes and
// commits, the repo's session log, and no card for the log's folder.
func TestCategoryRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	f := newFixture(t)
	one := f.project("mtg/one", map[string]string{"a.txt": "a"})
	f.project("mtg/two", map[string]string{"b.txt": "b"})
	top := filepath.Dir(one)
	git(t, top, "init", "-q")
	git(t, top, "add", ".")
	git(t, top, "commit", "-q", "-m", "both")
	write(t, filepath.Join(one, "c.txt"), "c")
	git(t, top, "add", "one")
	git(t, top, "commit", "-q", "-m", "only one")
	write(t, filepath.Join(one, "d.txt"), "d")
	write(t, filepath.Join(top, "llm-sessions", "INDEX.md"), "- 2026-11-04 [A title](2026-11-04-a.md): What happened.\n")
	write(t, filepath.Join(top, ".gitignore"), "llm-sessions/\n")

	b := f.board()
	g1, g2 := b.card(t, "mtg/one").Facts.Git, b.card(t, "mtg/two").Facts.Git
	if g1 == nil || g2 == nil {
		t.Fatalf("no git facts: %+v %+v", g1, g2)
	}
	if g1.Dirty != 1 || len(g1.Recent) != 2 || g1.Commits30d != 2 || g1.LastCommit.Subject != "only one" {
		t.Errorf("one = %+v", g1)
	}
	if g2.Dirty != 0 || len(g2.Recent) != 1 || g2.Commits30d != 1 || g2.LastCommit.Subject != "both" {
		t.Errorf("two = %+v", g2)
	}
	if l := b.card(t, "mtg/two").Facts.Log; l == nil || l.Entries != 1 {
		t.Errorf("log = %+v", l)
	}
	for _, c := range b.Projects {
		if c.ID == "mtg/llm-sessions" {
			t.Error("the session log got a card")
		}
	}
}

func TestBoardPayload(t *testing.T) {
	f := newFixture(t)
	f.project("code/a", nil)
	f.project("mtg/b", nil)
	write(t, filepath.Join(f.root, "board.html"), "not a project")
	mkdir(t, filepath.Join(f.root, "llm-sessions"))
	mkdir(t, filepath.Join(f.root, ".git"))
	f.registry(`{"version": 1, "projects": [
		{"id": "code/a", "name": "A", "status": "active", "hidden": true, "x-extra": 1},
		{"id": "home/gone", "name": "Gone", "status": "live"}
	]}`)
	b := f.board()
	if !slices.Equal(b.Categories, []string{"code", "home", "mtg"}) {
		t.Errorf("categories = %q", b.Categories)
	}
	if len(b.Projects) != 3 || len(b.Statuses) != 7 || b.Statuses[0].ID != "active" {
		t.Fatalf("board = %+v", b)
	}
	a := b.card(t, "code/a")
	if !a.Hidden || a.Untriaged || !slices.Equal(a.Warnings, []string{"no-next"}) {
		t.Errorf("code/a = %+v", a)
	}
	if m := b.card(t, "mtg/b"); !m.Untriaged || m.Status != "" || m.Name != "b" || !slices.Equal(m.Warnings, []string{"untriaged"}) {
		t.Errorf("untriaged card = %+v", m)
	}
	if g := b.card(t, "home/gone"); g.Facts.Exists || !slices.Equal(g.Warnings, []string{"missing"}) {
		t.Errorf("missing card = %+v", g)
	}
	// Every list and map is present, so the UI never meets null.
	raw, err := json.Marshal(b.card(t, "mtg/b"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"open_loops":[]`, `"tags":[]`, `"links":{}`, `"related":[]`, `"docs":[]`, `"warnings":["untriaged"]`, `"status":""`, `"untriaged":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("card JSON lacks %s: %s", want, raw)
		}
	}
}

func TestSetCommand(t *testing.T) {
	f := newFixture(t)
	dir := f.project("code/tool", map[string]string{"src/main.go": ""})
	f.registry(`{"version": 1, "projects": [{"id": "code/tool", "name": "tool", "status": "idea", "open_loops": ["alpha", "beta", "gamma"], "tags": ["cli"], "keep": "me"}]}`)
	f.mustRun(filepath.Join(dir, "src"), "set", "--status", "active", "--next", "write the parser", "--drop-loop", "2", "--drop-loop", "gam", "--add-loop", "delta", "--tag", "go", "--tag", "-cli", "--pin")
	reg, err := loadRegistry(filepath.Join(f.root, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := reg.find("code/tool")
	if p.Status != "active" || p.Next != "write the parser" || !p.Pinned || p.Updated != "2026-11-20" {
		t.Errorf("after set: %+v", p)
	}
	if !slices.Equal(p.OpenLoops, []string{"alpha", "delta"}) || !slices.Equal(p.Tags, []string{"go"}) {
		t.Errorf("loops %q, tags %q", p.OpenLoops, p.Tags)
	}
	if string(p.extra["keep"]) != `"me"` {
		t.Errorf("unknown key lost: %v", p.extra)
	}

	// The id first, flags after, from outside any project.
	f.mustRun(f.root, "set", "code/tool", "--next", "")
	if p := mustLoad(t, f).find("code/tool"); p.Next != "" {
		t.Errorf("--next \"\" didn't clear: %q", p.Next)
	}
	// A folder without an entry gets one.
	f.project("home/new", nil)
	out := f.mustRun(f.root, "set", "home/new", "--summary", "A new thing")
	if p := mustLoad(t, f).find("home/new"); p == nil || p.Status != "paused" || p.Name != "new" || p.Summary != "A new thing" {
		t.Errorf("new entry = %+v (%s)", p, out)
	}
	for _, args := range [][]string{
		{"set", "code/tool", "--status", "someday"},
		{"set", "code/tool"},
		{"set", "code/tool", "--pin", "--unpin"},
		{"set", "code/tool", "--drop-loop", "9"},
		{"set", "nowhere/x", "--status", "idea"},
	} {
		if _, _, code := f.run(f.root, args...); code == 0 {
			t.Errorf("%v succeeded", args)
		}
	}
}

func mustLoad(t *testing.T, f *fixture) *registry {
	t.Helper()
	reg, err := loadRegistry(filepath.Join(f.root, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestRegistryOverride(t *testing.T) {
	f := newFixture(t)
	f.project("code/x", nil)
	alt := filepath.Join(t.TempDir(), "alt.json")
	f.env["PROJECT_BOARD_REGISTRY"] = alt
	f.mustRun(f.root, "set", "code/x", "--status", "idea")
	if _, err := os.Stat(filepath.Join(f.root, "projects.json")); err == nil {
		t.Error("wrote <root>/projects.json despite PROJECT_BOARD_REGISTRY")
	}
	if p := func() *project { r, _ := loadRegistry(alt); return r.find("code/x") }(); p == nil {
		t.Error("entry not in the override registry")
	}
	other := filepath.Join(t.TempDir(), "flag.json")
	f.mustRun(f.root, "--registry", other, "set", "code/x", "--status", "live")
	f.mustRun(f.root, "set", "code/x", "--status", "done", "--registry="+other)
	if r, _ := loadRegistry(other); r.find("code/x") == nil || r.find("code/x").Status != "done" {
		t.Error("--registry not honoured")
	}
}

func TestValidate(t *testing.T) {
	f := newFixture(t)
	f.project("code/a", nil)
	f.registry(`{"version": 1, "projects": [
		{"id": "code/a", "name": "a", "status": "active", "summary": "` + strings.Repeat("x", 121) + `"},
		{"id": "code/a", "name": "dup", "status": "live"},
		{"id": "code/b", "name": "b", "status": "someday"},
		{"id": "code/c", "status": "idea", "related": ["code/zzz"]}
	]}`)
	out, _, code := f.run(f.root, "validate")
	if code == 0 {
		t.Error("validate passed a broken registry")
	}
	for _, want := range []string{"code/a: duplicate id", `unknown status "someday"`, "code/c: no name", "summary is 121 characters", "code/b: no folder", `related "code/zzz"`} {
		if !strings.Contains(out, want) {
			t.Errorf("validate output lacks %q:\n%s", want, out)
		}
	}
	f.registry(`{"version": 1, "projects": [{"id": "code/a", "name": "a", "status": "active"}]}`)
	if out, _, code := f.run(f.root, "validate"); code != 0 {
		t.Errorf("valid registry failed:\n%s", out)
	}
	f.registry(`{"version": 1, "projects": [`)
	if _, _, code := f.run(f.root, "validate"); code == 0 {
		t.Error("unparseable registry passed")
	}
}

func TestListAndShow(t *testing.T) {
	f := newFixture(t)
	f.project("code/live-one", nil)
	f.project("code/old", nil)
	f.project("home/fresh", nil)
	f.registry(`{"version": 1, "projects": [
		{"id": "code/live-one", "name": "live one", "status": "live", "summary": "Runs"},
		{"id": "code/old", "name": "old", "status": "done"},
		{"id": "home/fresh", "name": "fresh", "status": "paused", "next": "Buy screws", "open_loops": ["ask Sam"]}
	]}`)
	out := f.mustRun(f.root, "list")
	if !strings.Contains(out, "Paused (1)") || !strings.Contains(out, "next: Buy screws") || strings.Contains(out, "code/old") {
		t.Errorf("list:\n%s", out)
	}
	if out := f.mustRun(f.root, "list", "--all"); !strings.Contains(out, "Done (1)") {
		t.Errorf("list --all:\n%s", out)
	}
	out = f.mustRun(filepath.Join(f.root, "home", "fresh"), "show")
	for _, want := range []string{"fresh (home/fresh)  paused", "next:", "1. ask Sam", "claude --continue"} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
}

func TestHTTPAPI(t *testing.T) {
	f := newFixture(t)
	f.project("code/a", nil)
	f.project("code/fresh", nil)
	f.registry(`{"version": 1, "projects": [{"id": "code/a", "name": "A", "status": "idea", "custom": [1]}]}`)
	a, _, _ := f.app(f.root)
	srv := newServer(a, 7777)
	do := func(method, target, body string, headers map[string]string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host = "127.0.0.1:7777"
		for k, v := range headers {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var m map[string]any
		if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
				t.Fatalf("%s %s: bad JSON %q", method, target, rec.Body.String())
			}
		}
		return rec.Code, m
	}

	if code, _ := do("GET", "/", "", nil); code != 200 {
		t.Errorf("GET / = %d", code)
	}
	code, m := do("GET", "/api/board", "", nil)
	if code != 200 || len(m["projects"].([]any)) != 2 || m["registry"] != filepath.Join(f.root, "projects.json") {
		t.Errorf("GET /api/board = %d %v", code, m)
	}

	code, m = do("POST", "/api/project?id=code/a", `{"status": "active", "next": "do it"}`, map[string]string{"Origin": "http://127.0.0.1:7777", "Content-Type": "application/json"})
	if code != 200 || m["status"] != "active" || m["next"] != "do it" || m["updated"] != "2026-11-20" || m["facts"] == nil {
		t.Errorf("POST = %d %v", code, m)
	}
	if !strings.Contains(read(t, filepath.Join(f.root, "projects.json")), `"custom": [`) {
		t.Error("POST dropped an unknown key")
	}
	code, m = do("POST", "/api/project?id=code/fresh", `{"left_off": "started"}`, nil)
	if code != 200 || m["status"] != "paused" || m["untriaged"] != false || m["name"] != "fresh" {
		t.Errorf("POST creating an entry = %d %v", code, m)
	}

	bad := []struct {
		name, target, body string
		headers            map[string]string
		want               int
	}{
		{"bad status", "/api/project?id=code/a", `{"status": "nope"}`, nil, 400},
		{"not an object", "/api/project?id=code/a", `[1]`, nil, 400},
		{"no id", "/api/project", `{}`, nil, 400},
		{"unknown project", "/api/project?id=code/zzz", `{"status": "idea"}`, nil, 404},
		{"path escape", "/api/project?id=../../etc", `{"status": "idea"}`, nil, 404},
		{"foreign origin", "/api/project?id=code/a", `{"status": "done"}`, map[string]string{"Origin": "http://evil.example"}, 403},
		{"rebound host", "/api/project?id=code/a", `{"status": "done"}`, map[string]string{"Host": "evil.example:7777", "Origin": "http://evil.example:7777"}, 403},
	}
	for _, tc := range bad {
		code, m := do("POST", tc.target, tc.body, tc.headers)
		if code != tc.want || m["error"] == nil {
			t.Errorf("%s: %d %v, want %d with an error", tc.name, code, m, tc.want)
		}
	}
	if p := mustLoad(t, f).find("code/a"); p.Status != "active" {
		t.Errorf("a refused POST changed the registry: %+v", p)
	}
	if code, _ := do("GET", "/api/board", "", map[string]string{"Host": "evil.example:7777"}); code != 403 {
		t.Errorf("GET with a foreign Host = %d", code)
	}
	if code, _ := do("DELETE", "/api/board", "", nil); code != 405 {
		t.Errorf("DELETE = %d", code)
	}
}

func TestHTMLSnapshotCommand(t *testing.T) {
	f := newFixture(t)
	f.project("code/a", nil)
	out := filepath.Join(t.TempDir(), "snap.html")
	f.mustRun(f.root, "html", "-o", out)
	page := read(t, out)
	if strings.Contains(page, placeholder) || !strings.Contains(page, `"id":"code/a"`) {
		t.Errorf("snapshot not filled:\n%.400s", page)
	}
	f.mustRun(f.root, "html")
	if _, err := os.Stat(filepath.Join(f.root, "board.html")); err != nil {
		t.Errorf("default output missing: %v", err)
	}
}

func TestDaysBetween(t *testing.T) {
	now := time.Date(2026, 11, 20, 8, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		t    time.Time
		want int
	}{
		{time.Date(2026, 11, 20, 7, 0, 0, 0, time.Local), 0},
		{time.Date(2026, 11, 19, 23, 0, 0, 0, time.Local), 1},
		{time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local), 31},
		{now.Add(time.Hour), 0},
	} {
		if got := daysBetween(tc.t, now); got != tc.want {
			t.Errorf("daysBetween(%v) = %d, want %d", tc.t, got, tc.want)
		}
	}
	for days, want := range map[int]string{0: "today", 3: "3d", 13: "13d", 35: "5w", 120: "4mo"} {
		if got := idle(&days); got != want {
			t.Errorf("idle(%d) = %s, want %s", days, got, want)
		}
	}
}

func TestNextStepPrefersRegistry(t *testing.T) {
	vault := &vaultFacts{NextAction: "old vault task"}
	if got := nextStep(&card{Next: "fresh step", Facts: facts{Vault: vault}}); got != "fresh step" {
		t.Errorf("registry next should win, got %q", got)
	}
	if got := nextStep(&card{Facts: facts{Vault: vault}}); got != "old vault task" {
		t.Errorf("vault task should fill an empty next, got %q", got)
	}
	if got := nextStep(&card{}); got != "" {
		t.Errorf("no next anywhere, got %q", got)
	}
}
