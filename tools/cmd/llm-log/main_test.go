package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local)

// harness runs llm-log in-process with a private state folder, so tests never
// touch the real ~/.local/state.
type harness struct {
	t     *testing.T
	state string
}

func newHarness(t *testing.T) *harness {
	return &harness{t: t, state: t.TempDir()}
}

// project makes a folder that is a project, as llm-log init does.
func (h *harness) project() string {
	h.t.Helper()
	dir := h.t.TempDir()
	h.mustRun(dir, "", "", "init")
	return dir
}

func (h *harness) run(cwd, session, stdin string, args ...string) (string, string, int) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := map[string]string{"CLAUDE_CODE_SESSION_ID": session, "XDG_STATE_HOME": h.state}
	a := &app{
		stdin:  strings.NewReader(stdin),
		stdout: &out,
		stderr: &errOut,
		getenv: func(k string) string { return env[k] },
		cwd:    cwd,
		now:    func() time.Time { return fixedNow },
	}
	code := a.run(args)
	return out.String(), errOut.String(), code
}

func (h *harness) mustRun(cwd, session, stdin string, args ...string) string {
	h.t.Helper()
	out, errOut, code := h.run(cwd, session, stdin, args...)
	if code != 0 {
		h.t.Fatalf("llm-log %v: exit %d: %s", args, code, errOut)
	}
	return out
}

const (
	s1 = "11111111-aaaa-4bbb-8ccc-000000000001"
	s2 = "22222222-aaaa-4bbb-8ccc-000000000002"
)

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

func entryLines(index string) []string {
	var out []string
	for _, l := range strings.Split(index, "\n") {
		if strings.HasPrefix(l, "- ") {
			out = append(out, l)
		}
	}
	return out
}

func TestNoteAppendsToDraft(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	h.mustRun(dir, s1, "", "note", "decision", "Write llm-log in Go")
	h.mustRun(dir, s1, "multi\nline   note\n", "note", "learning")
	got := read(t, filepath.Join(dir, "llm-sessions", ".drafts", s1+".md"))
	want := "---\nsession: " + s1 + "\nstarted: 2026-11-03\n---\n" +
		"- 10:00 decision: Write llm-log in Go\n" +
		"- 10:00 learning: multi line note\n"
	if got != want {
		t.Fatalf("draft:\n%s\nwant:\n%s", got, want)
	}
}

func TestNoteRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	for _, c := range []struct {
		session string
		args    []string
	}{
		{s1, []string{"note", "idea", "x"}},
		{s1, []string{"note", "open"}}, // no text, empty stdin
		{"", []string{"note", "open", "x"}},
		{"../../etc/passwd", []string{"note", "open", "x"}},
	} {
		if _, _, code := h.run(dir, c.session, "", c.args...); code == 0 {
			t.Errorf("llm-log %v with session %q: want an error", c.args, c.session)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "llm-sessions", ".drafts")); err == nil {
		t.Error("a rejected note created a draft")
	}
}

func finish(h *harness, dir, session, slug, title, summary string) string {
	h.t.Helper()
	return h.mustRun(dir, session, "## Decisions\n\n- "+title+"\n", "finish", "--slug", slug, "--title", title, "--summary", summary)
}

func TestFinishWritesEntryAndIndexNewestFirst(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	h.mustRun(dir, s1, "", "note", "decision", "first")
	finish(h, dir, s1, "first-session", "First session", "Did the first thing.")
	finish(h, dir, s2, "second-session", "Second [beta] session", "Did the second thing.")

	logDir := filepath.Join(dir, "llm-sessions")
	entry := read(t, filepath.Join(logDir, "2026-11-03-first-session.md"))
	if !strings.HasPrefix(entry, "---\ntitle: \"First session\"\ndate: 2026-11-03\nsession: "+s1+"\n---\n\n## Decisions") {
		t.Errorf("entry frontmatter:\n%s", entry)
	}
	if _, err := os.Stat(filepath.Join(logDir, ".drafts", s1+".md")); err == nil {
		t.Error("finish left the draft behind")
	}
	index := read(t, filepath.Join(logDir, "INDEX.md"))
	if !strings.HasPrefix(index, indexHeader+"\n- ") {
		t.Errorf("index header:\n%s", index)
	}
	want := []string{
		`- 2026-11-03 [Second \[beta\] session](2026-11-03-second-session.md): Did the second thing.`,
		"- 2026-11-03 [First session](2026-11-03-first-session.md): Did the first thing.",
	}
	if got := entryLines(index); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("index entries:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFinishAgainUpdatesInPlace(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	finish(h, dir, s1, "old-slug", "Old", "Old summary.")
	finish(h, dir, s2, "other", "Other", "Other summary.")
	// s1 is resumed and finished again under a better slug.
	finish(h, dir, s1, "new-slug", "New", "New summary.")

	logDir := filepath.Join(dir, "llm-sessions")
	if _, err := os.Stat(filepath.Join(logDir, "2026-11-03-old-slug.md")); err == nil {
		t.Error("the old file survived the rename")
	}
	if !strings.Contains(read(t, filepath.Join(logDir, "2026-11-03-new-slug.md")), "session: "+s1) {
		t.Error("renamed entry lost its session id")
	}
	got := entryLines(read(t, filepath.Join(logDir, "INDEX.md")))
	want := []string{
		"- 2026-11-03 [Other](2026-11-03-other.md): Other summary.",
		"- 2026-11-03 [New](2026-11-03-new-slug.md): New summary.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("index entries:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFinishRefusesAnotherSessionsSlug(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	finish(h, dir, s1, "taken", "One", "One.")
	_, errOut, code := h.run(dir, s2, "body", "finish", "--slug", "taken", "--title", "Two", "--summary", "Two.")
	if code == 0 || !strings.Contains(errOut, "already exists") {
		t.Fatalf("want a slug clash error, got exit %d: %s", code, errOut)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "llm-sessions", "2026-11-03-taken.md")), "session: "+s1) {
		t.Error("the clash overwrote the first session's entry")
	}
	if n := len(entryLines(read(t, filepath.Join(dir, "llm-sessions", "INDEX.md")))); n != 1 {
		t.Errorf("index has %d entries, want 1", n)
	}
}

func TestFinishValidatesInput(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	long := strings.Repeat("x", maxSummary+1)
	for _, args := range [][]string{
		{"--slug", "Bad Slug", "--title", "T", "--summary", "S."},
		{"--slug", "ok", "--title", "", "--summary", "S."},
		{"--slug", "ok", "--title", "T", "--summary", long},
		{"--slug", "ok", "--title", "T", "--summary", "S.", "stray"},
	} {
		if _, _, code := h.run(dir, s1, "body", append([]string{"finish"}, args...)...); code == 0 {
			t.Errorf("finish %v: want an error", args)
		}
	}
	if _, _, code := h.run(dir, s1, "  \n", "finish", "--slug", "ok", "--title", "T", "--summary", "S."); code == 0 {
		t.Error("finish with an empty body: want an error")
	}
}

func TestFinishLeftoverDraftKeepsItsStartDate(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	drafts := filepath.Join(dir, "llm-sessions", ".drafts")
	if err := os.MkdirAll(drafts, 0o755); err != nil {
		t.Fatal(err)
	}
	draft := "---\nsession: " + s2 + "\nstarted: 2026-10-30\n---\n- 09:00 open: unfinished\n"
	if err := os.WriteFile(filepath.Join(drafts, s2+".md"), []byte(draft), 0o644); err != nil {
		t.Fatal(err)
	}
	out := h.mustRun(dir, s1, "", "show", "--session", s2)
	if !strings.Contains(out, "open: unfinished") {
		t.Fatalf("show --session: %s", out)
	}
	h.mustRun(dir, s1, "body", "finish", "--session", s2, "--slug", "late", "--title", "Late", "--summary", "Late.")
	if _, err := os.Stat(filepath.Join(dir, "llm-sessions", "2026-10-30-late.md")); err != nil {
		t.Errorf("entry not dated by the draft: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	base := []string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "init.defaultBranch=main"}
	cmd := gitCmd(dir, append(base, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLogDirIsTheMainCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// validate runs these tests from the pre-commit hook, where GIT_DIR points
	// at the dotfiles repo; the tool has to ignore it.
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_INDEX_FILE", "/nonexistent")

	h := newHarness(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	worktree := filepath.Join(root, "wt")
	git(t, repo, "worktree", "add", "-q", worktree)
	if err := os.MkdirAll(filepath.Join(worktree, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()

	want := realpath(t, repo) + "/llm-sessions"
	for _, cwd := range []string{repo, filepath.Join(repo, "sub", "deeper"), worktree, filepath.Join(worktree, "sub")} {
		if got := strings.TrimSpace(h.mustRun(cwd, s1, "", "path")); realpath(t, filepath.Dir(got))+"/llm-sessions" != want {
			t.Errorf("path from %s = %s, want %s", cwd, got, want)
		}
	}
	if _, _, code := h.run(plain, s1, "", "path"); code == 0 {
		t.Error("path outside a repo and with no llm-sessions/: want an error")
	}
	// A repo needs no init: a note there just works.
	h.mustRun(filepath.Join(repo, "sub"), s1, "", "note", "open", "in a subfolder")
	if !strings.Contains(read(t, filepath.Join(repo, "llm-sessions", ".drafts", s1+".md")), "in a subfolder") {
		t.Error("the note didn't land at the repo's root")
	}
	// init in a repo's subfolder means the repo.
	if out := h.mustRun(filepath.Join(repo, "sub", "deeper"), "", "", "init"); !strings.Contains(out, "/repo/llm-sessions") {
		t.Errorf("init in a repo's subfolder: %q", out)
	}
	if _, err := os.Stat(filepath.Join(repo, "sub", "deeper", "llm-sessions")); err == nil {
		t.Error("init made a folder inside the repo's subfolder")
	}
}

func TestOutsideAProjectIsAnError(t *testing.T) {
	h, plain := newHarness(t), t.TempDir()
	for _, args := range [][]string{{"note", "open", "x"}, {"path"}, {"drafts"}} {
		_, errOut, code := h.run(plain, s1, "", args...)
		if code == 0 || !strings.Contains(errOut, "llm-log init") {
			t.Errorf("llm-log %v outside a project: exit %d, %q", args, code, errOut)
		}
	}
	if _, _, code := h.run(plain, s1, "body", "finish", "--slug", "x", "--title", "T", "--summary", "S."); code == 0 {
		t.Error("finish outside a project: want an error")
	}
	if _, err := os.Stat(filepath.Join(plain, "llm-sessions")); err == nil {
		t.Error("an error left an llm-sessions folder behind")
	}
}

func TestInitMakesAFolderAProject(t *testing.T) {
	h, dir := newHarness(t), t.TempDir()
	sub := filepath.Join(dir, "sub", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(dir, "", "", "init")
	if got := read(t, filepath.Join(dir, "llm-sessions", "INDEX.md")); got != indexHeader {
		t.Errorf("index after init:\n%s", got)
	}
	// From a subfolder, the nearest project up.
	if got := strings.TrimSpace(h.mustRun(sub, s1, "", "path")); got != filepath.Join(dir, "llm-sessions") {
		t.Errorf("path from a subfolder = %s", got)
	}
	// Run again it leaves what's there.
	finish(h, dir, s1, "kept", "Kept", "Kept.")
	h.mustRun(dir, "", "", "init")
	if n := len(entryLines(read(t, filepath.Join(dir, "llm-sessions", "INDEX.md")))); n != 1 {
		t.Errorf("second init left %d index entries, want 1", n)
	}
	other := t.TempDir()
	h.mustRun(dir, "", "", "init", other)
	if _, err := os.Stat(filepath.Join(other, "llm-sessions", "INDEX.md")); err != nil {
		t.Errorf("init <folder>: %v", err)
	}
	if _, _, code := h.run(dir, "", "", "init", filepath.Join(dir, "missing")); code == 0 {
		t.Error("init of a missing folder: want an error")
	}
}

func TestOneSessionLogsInEachProjectItWorksIn(t *testing.T) {
	h := newHarness(t)
	stash, touchup, dotfiles := h.project(), h.project(), h.project()
	h.mustRun(stash, s1, hookJSON(s1, stash, "startup"), "hook", "session-start")
	h.mustRun(stash, s1, "", "note", "decision", "about stash")
	h.mustRun(touchup, s1, "", "note", "decision", "about touchup")
	h.mustRun(dotfiles, s1, "", "note", "learning", "about dotfiles")

	if d := read(t, filepath.Join(touchup, "llm-sessions", ".drafts", s1+".md")); !strings.Contains(d, "about touchup") || strings.Contains(d, "about stash") {
		t.Errorf("touchup's draft:\n%s", d)
	}
	// Compaction and a resume change nothing about where notes go.
	h.mustRun(stash, s1, hookJSON(s1, stash, "compact"), "hook", "session-start")
	h.mustRun(touchup, s1, "", "note", "open", "still touchup")
	if d := read(t, filepath.Join(touchup, "llm-sessions", ".drafts", s1+".md")); !strings.Contains(d, "still touchup") {
		t.Errorf("after compaction, touchup's draft:\n%s", d)
	}

	// show finds them all from any of them, or from somewhere else entirely.
	for _, cwd := range []string{stash, touchup, t.TempDir()} {
		out := h.mustRun(cwd, s1, "", "show")
		for _, want := range []string{"about stash", "about touchup", "about dotfiles"} {
			if !strings.Contains(out, want) {
				t.Errorf("show from %s lacks %q:\n%s", cwd, want, out)
			}
		}
	}

	// finish writes only here, and names what's left.
	out := finish(h, stash, s1, "stash-side", "Stash side", "Stash.")
	if !strings.Contains(out, touchup) || !strings.Contains(out, dotfiles) || !strings.Contains(out, "finish there too") {
		t.Errorf("finish in stash:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(touchup, "llm-sessions", ".drafts", s1+".md")); err != nil {
		t.Error("finishing stash's entry touched touchup's draft")
	}
	finish(h, touchup, s1, "touchup-side", "Touchup side", "Touchup.")
	out = finish(h, dotfiles, s1, "dotfiles-side", "Dotfiles side", "Dotfiles.")
	if strings.Contains(out, "finish there too") {
		t.Errorf("the last finish still names a project:\n%s", out)
	}
	for dir, name := range map[string]string{stash: "2026-11-03-stash-side.md", touchup: "2026-11-03-touchup-side.md", dotfiles: "2026-11-03-dotfiles-side.md"} {
		if !strings.Contains(read(t, filepath.Join(dir, "llm-sessions", name)), "session: "+s1) {
			t.Errorf("no entry %s in %s", name, dir)
		}
		if n := len(entryLines(read(t, filepath.Join(dir, "llm-sessions", "INDEX.md")))); n != 1 {
			t.Errorf("%s has %d index lines, want 1", dir, n)
		}
	}
	// All finished, show lists the three entries.
	out = h.mustRun(stash, s1, "", "show")
	if strings.Count(out, "== entry") != 3 || strings.Contains(out, "== draft") {
		t.Errorf("show after finishing:\n%s", out)
	}
}

func TestShowFindsARecordFromBeforeNotesFollowedTheFolder(t *testing.T) {
	h, launch := newHarness(t), folderWithLog(t)
	state := filepath.Join(h.state, "llm-log", "sessions")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, s1), []byte(launch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	drafts := filepath.Join(launch, "llm-sessions", ".drafts")
	if err := os.MkdirAll(drafts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(drafts, s1+".md"), []byte("---\nsession: x\n---\n- 09:00 open: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := h.mustRun(t.TempDir(), s1, "", "show"); !strings.Contains(out, "open: old") {
		t.Errorf("show:\n%s", out)
	}
}

func folderWithLog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "llm-sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func hookJSON(session, cwd, source string) string {
	return fmt.Sprintf(`{"session_id":%q,"cwd":%q,"source":%q,"hook_event_name":"SessionStart"}`, session, cwd, source)
}

func TestHookReportsOtherSessionsDrafts(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	h.mustRun(dir, s1, "", "note", "open", "left unfinished")

	out := h.mustRun(dir, s2, hookJSON(s2, dir, "startup"), "hook", "session-start")
	if !strings.Contains(out, "1 unfinished session draft") || !strings.Contains(out, "1 note,") || !strings.Contains(out, s1) || strings.Count(out, "\n") != 1 {
		t.Errorf("another session's startup: want one line naming %s, got %q", s1, out)
	}
	if out := h.mustRun(dir, s1, hookJSON(s1, dir, "resume"), "hook", "session-start"); out != "" {
		t.Errorf("resuming the draft's own session: want no notice, got %q", out)
	}
	if out := h.mustRun(dir, s2, hookJSON(s2, dir, "compact"), "hook", "session-start"); out != "" {
		t.Errorf("after compaction: want no notice, got %q", out)
	}
	if out := h.mustRun(t.TempDir(), s2, hookJSON(s2, t.TempDir(), "startup"), "hook", "session-start"); out != "" {
		t.Errorf("a folder without drafts: want no notice, got %q", out)
	}
	if _, _, code := h.run(dir, s2, "not json", "hook", "session-start"); code == 0 {
		t.Error("malformed hook input: want an error")
	}
}

func TestConcurrentFinishesKeepEveryLine(t *testing.T) {
	h := newHarness(t)
	dir := h.project()
	const n = 40
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			id := fmt.Sprintf("%08d-aaaa-4bbb-8ccc-000000000000", i)
			if _, errOut, code := h.run(dir, id, "body", "finish", "--slug", fmt.Sprintf("s%d", i), "--title", "T", "--summary", "S."); code != 0 {
				t.Errorf("session %d: %s", i, errOut)
			}
		})
	}
	wg.Wait()
	if got := len(entryLines(read(t, filepath.Join(dir, "llm-sessions", "INDEX.md")))); got != n {
		t.Errorf("index has %d entries after %d parallel finishes", got, n)
	}
}
