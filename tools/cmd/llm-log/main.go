// Command llm-log keeps Claude Code's per-project session log. Each project
// gets an llm-sessions/ folder at its root with one Markdown file per session
// and INDEX.md, one line per session, newest first. Every command acts on the
// project of the folder it's run in, so a session that changes three projects
// leaves its notes and an entry in each. While a session runs, `note` appends
// one-line notes to that project's draft; /session-end turns each draft into the
// project's entry with `finish`. ~/.claude/rules/llm-sessions.md tells Claude
// when to call it. The folder is gitignored globally, so it stays local.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const usage = `llm-log: Claude Code's per-project session log (llm-sessions/).

Every command acts on the project of the current folder: its git repo (a worktree
counts as its main checkout), or else the nearest folder up with an llm-sessions/.
Anywhere else is an error, so cd to the project a note is about.

  llm-log note <kind> [text]   append a note to this session's draft here; reads
                               the text from stdin when it isn't given.
                               kinds: decision, learning, dead-end, open
  llm-log show                 print this session's drafts and entries, in every
                               project it has logged in
  llm-log finish --slug S --title T --summary X
                               write this project's entry (body on stdin), add or
                               update its INDEX.md line, delete its draft, and name
                               any other project still holding this session's notes
  llm-log drafts               list drafts other sessions left unfinished here
  llm-log path                 print this project's llm-sessions folder
  llm-log init [folder]        make a folder that isn't a git repo a project
  llm-log hook session-start   the SessionStart hook: report leftover drafts

show and finish take --session ID to work on another session's draft.
The session id comes from $CLAUDE_CODE_SESSION_ID.
`

const (
	dirName     = "llm-sessions"
	indexName   = "INDEX.md"
	indexHeader = "# Session log\n\nOne line per Claude Code session, newest first. Each links to that session's file in this folder.\n"
	maxSlug     = 60
	maxTitle    = 80
	maxSummary  = 240
	stateMaxAge = 90 * 24 * time.Hour
)

var (
	kinds  = []string{"decision", "learning", "dead-end", "open"}
	slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	idRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{7,63}$`)
)

type app struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	getenv         func(string) string
	cwd            string
	now            func() time.Time
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llm-log:", err)
		os.Exit(1)
	}
	a := &app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, getenv: os.Getenv, cwd: cwd, now: time.Now}
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "note":
		err = a.note(args[1:])
	case "show":
		err = a.show(args[1:])
	case "finish":
		err = a.finish(args[1:])
	case "drafts":
		err = a.drafts()
	case "path":
		var dir string
		if dir, err = a.logDir(); err == nil {
			fmt.Fprintln(a.stdout, dir)
		}
	case "init":
		err = a.initProject(args[1:])
	case "hook":
		err = a.hook(args[1:])
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, usage)
	default:
		err = fmt.Errorf("unknown command %q (see llm-log --help)", args[0])
	}
	if err != nil {
		fmt.Fprintln(a.stderr, "llm-log:", err)
		return 1
	}
	return 0
}

func (a *app) note(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llm-log note <%s> [text]", strings.Join(kinds, "|"))
	}
	kind := args[0]
	if !slices.Contains(kinds, kind) {
		return fmt.Errorf("unknown kind %q: use one of %s", kind, strings.Join(kinds, ", "))
	}
	text := strings.Join(args[1:], " ")
	if len(args) == 1 {
		b, err := io.ReadAll(a.stdin)
		if err != nil {
			return err
		}
		text = string(b)
	}
	text = oneLine(text)
	if text == "" {
		return errors.New("empty note")
	}
	id, err := a.sessionID("")
	if err != nil {
		return err
	}
	dir, err := a.logDir()
	if err != nil {
		return err
	}
	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	if err := a.remember(id, dir); err != nil {
		return err
	}

	path := draftPath(dir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var b strings.Builder
	if st, err := f.Stat(); err == nil && st.Size() == 0 {
		fmt.Fprintf(&b, "---\nsession: %s\nstarted: %s\n---\n", id, a.now().Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "- %s %s: %s\n", a.now().Format("15:04"), kind, text)
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (a *app) show(args []string) error {
	flags := flag.NewFlagSet("show", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	session := flags.String("session", "", "session id (default: this session)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	id, err := a.sessionID(*session)
	if err != nil {
		return err
	}
	dirs := a.sessionDirs(id)
	found := false
	for _, dir := range dirs {
		if b, err := os.ReadFile(draftPath(dir, id)); err == nil {
			fmt.Fprintf(a.stdout, "== draft %s\n%s", draftPath(dir, id), withNewline(b))
			found = true
		}
		name, _, err := findEntry(dir, id)
		if err != nil {
			return err
		}
		if name != "" {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "== entry %s\n%s", filepath.Join(dir, name), withNewline(b))
			found = true
		}
	}
	if !found {
		fmt.Fprintf(a.stdout, "no notes or entry yet for session %s in %s\n", id, strings.Join(dirs, ", "))
	}
	return nil
}

func (a *app) finish(args []string) error {
	flags := flag.NewFlagSet("finish", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	session := flags.String("session", "", "session id (default: this session)")
	slug := flags.String("slug", "", "names what the session did or decided")
	title := flags.String("title", "", "the entry's title")
	summary := flags.String("summary", "", "one sentence for INDEX.md")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q (quote the --title and --summary values)", flags.Arg(0))
	}
	id, err := a.sessionID(*session)
	if err != nil {
		return err
	}
	if len(*slug) > maxSlug || !slugRe.MatchString(*slug) {
		return fmt.Errorf("--slug must be lowercase words joined by hyphens, at most %d characters", maxSlug)
	}
	t, s := oneLine(*title), oneLine(*summary)
	if n := utf8.RuneCountInString(t); n == 0 || n > maxTitle {
		return fmt.Errorf("--title must be 1 to %d characters (got %d)", maxTitle, n)
	}
	if n := utf8.RuneCountInString(s); n == 0 || n > maxSummary {
		return fmt.Errorf("--summary must be one sentence of at most %d characters (got %d)", maxSummary, n)
	}
	raw, err := io.ReadAll(a.stdin)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return errors.New("empty entry: pipe the entry's body to stdin")
	}

	dir, err := a.logDir()
	if err != nil {
		return err
	}
	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	if err := a.remember(id, dir); err != nil {
		return err
	}

	oldName, oldFM, err := findEntry(dir, id)
	if err != nil {
		return err
	}
	// The entry keeps the date the session started: the draft's, or the date
	// an earlier /session-end of this (resumed) session gave it.
	date := ""
	if b, err := os.ReadFile(draftPath(dir, id)); err == nil {
		date = frontmatter(b)["started"]
	}
	if date == "" {
		date = oldFM["date"]
	}
	if date == "" {
		date = a.now().Format("2006-01-02")
	}
	name := date + "-" + *slug + ".md"
	if name != oldName {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return fmt.Errorf("%s already exists (session %q); pick another slug", name, frontmatter(b)["session"])
		}
	}

	content := fmt.Sprintf("---\ntitle: %s\ndate: %s\nsession: %s\n---\n\n%s\n", strconv.Quote(t), date, id, body)
	if err := writeAtomic(filepath.Join(dir, name), []byte(content)); err != nil {
		return err
	}
	line := fmt.Sprintf("- %s [%s](%s): %s", date, escapeLink(t), name, s)
	if err := updateIndex(dir, oldName, name, line); err != nil {
		return err
	}
	if oldName != "" && oldName != name {
		if err := os.Remove(filepath.Join(dir, oldName)); err != nil {
			return err
		}
	}
	if err := os.Remove(draftPath(dir, id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s\nindex: %s\n", filepath.Join(dir, name), line)
	for _, other := range a.sessionDirs(id) {
		if _, err := os.Stat(draftPath(other, id)); err == nil && other != dir {
			fmt.Fprintf(a.stdout, "this session still has notes in %s: finish there too\n", filepath.Dir(other))
		}
	}
	return nil
}

func (a *app) drafts() error {
	dir, err := a.logDir()
	if err != nil {
		return err
	}
	ds, err := listDrafts(dir, a.getenv("CLAUDE_CODE_SESSION_ID"))
	if err != nil {
		return err
	}
	for _, d := range ds {
		fmt.Fprintln(a.stdout, d.describe(a.now()))
	}
	return nil
}

type hookInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Source    string `json:"source"`
}

// hook runs as Claude Code's SessionStart hook. When other sessions left drafts
// in this project it prints one line, which Claude Code adds to the session's
// context. That line is the only thing the log ever puts there.
func (a *app) hook(args []string) error {
	if len(args) != 1 || args[0] != "session-start" {
		return errors.New("usage: llm-log hook session-start (hook JSON on stdin)")
	}
	var in hookInput
	if err := json.NewDecoder(a.stdin).Decode(&in); err != nil {
		return fmt.Errorf("reading hook input: %w", err)
	}
	if !idRe.MatchString(in.SessionID) || in.Cwd == "" {
		return errors.New("hook input lacks a session_id or cwd")
	}
	state := a.stateDir()
	if record := filepath.Join(state, in.SessionID); fileExists(record) {
		now := a.now()
		os.Chtimes(record, now, now) // keeps pruneState off a session still in use
	}
	pruneState(state, a.now())
	if in.Source == "compact" {
		return nil // the same session continuing; it was told at startup
	}
	root, ok := findProject(in.Cwd)
	if !ok {
		return nil
	}
	ds, err := listDrafts(filepath.Join(root, dirName), in.SessionID)
	if err != nil || len(ds) == 0 {
		return err
	}
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = d.describe(a.now())
	}
	noun := "draft"
	if len(ds) > 1 {
		noun = "drafts"
	}
	fmt.Fprintf(a.stdout, "llm-sessions: %d unfinished session %s in this project: %s. Mention this to the user once; `/session-end <session id>` finishes one from its notes, unless that session is still running.\n",
		len(ds), noun, strings.Join(parts, "; "))
	return nil
}

// initProject makes a folder a project, for one that isn't in a git repo; a repo
// is one already. Safe to run again.
func (a *app) initProject(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: llm-log init [folder]")
	}
	folder := a.cwd
	if len(args) == 1 {
		folder = args[0]
		if !filepath.IsAbs(folder) {
			folder = filepath.Join(a.cwd, folder)
		}
	}
	if st, err := os.Stat(folder); err != nil || !st.IsDir() {
		return fmt.Errorf("%s is not a folder", folder)
	}
	root, ok := gitRoot(folder)
	if !ok {
		root = folder
	}
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	index := filepath.Join(dir, indexName)
	if !fileExists(index) {
		if err := writeAtomic(index, []byte(indexHeader)); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.stdout, "logging in %s\n", dir)
	return nil
}

func (a *app) sessionID(override string) (string, error) {
	id := override
	if id == "" {
		id = a.getenv("CLAUDE_CODE_SESSION_ID")
	}
	if id == "" {
		return "", errors.New("no session id: $CLAUDE_CODE_SESSION_ID is unset (run this from Claude Code, or pass --session)")
	}
	if !idRe.MatchString(id) {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	return id, nil
}

func (a *app) stateDir() string {
	if d := a.getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "llm-log", "sessions")
	}
	return filepath.Join(a.getenv("HOME"), ".local", "state", "llm-log", "sessions")
}

// logDir is the llm-sessions folder of the project the current folder is in.
func (a *app) logDir() (string, error) {
	root, ok := findProject(a.cwd)
	if !ok {
		return "", fmt.Errorf("%s isn't in a project: cd to the project this is about, or run `llm-log init` to make this folder one", a.cwd)
	}
	return filepath.Join(root, dirName), nil
}

// findProject finds the project a folder is in: its git repo, or else the nearest
// folder at or above it that already has an llm-sessions/ folder.
func findProject(dir string) (string, bool) {
	if root, ok := gitRoot(dir); ok {
		return root, true
	}
	for d := dir; ; d = filepath.Dir(d) {
		if st, err := os.Stat(filepath.Join(d, dirName)); err == nil && st.IsDir() {
			return d, true
		}
		if d == filepath.Dir(d) {
			return "", false
		}
	}
}

// gitRoot lifts a folder inside a git repo to the main checkout's root, so a
// session working in a subfolder or a linked worktree logs with the rest of the
// repo. An uncommitted log inside a worktree would be deleted along with the
// worktree.
func gitRoot(dir string) (string, bool) {
	out, err := gitCmd(dir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", false
	}
	// The first entry is always the main worktree. A bare main repo has no
	// checkout to log into, so fall back to this worktree's own root.
	first := strings.SplitN(string(out), "\n\n", 2)[0]
	main := ""
	for _, line := range strings.Split(first, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			main = p
		}
		if line == "bare" {
			main = ""
			break
		}
	}
	if main != "" {
		return main, true
	}
	if top, err := gitCmd(dir, "rev-parse", "--show-toplevel").Output(); err == nil {
		return strings.TrimSpace(string(top)), true
	}
	return "", false
}

// remember records that session id has something in the project of log folder
// dir, so show and finish can find all of a session's projects from any of them.
func (a *app) remember(id, dir string) error {
	root := filepath.Dir(dir)
	if slices.Contains(a.recorded(id), root) {
		return nil
	}
	state := a.stateDir()
	if err := os.MkdirAll(state, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(state, id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(root + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (a *app) recorded(id string) []string {
	b, err := os.ReadFile(filepath.Join(a.stateDir(), id))
	if err != nil {
		return nil
	}
	var roots []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			roots = append(roots, l)
		}
	}
	return roots
}

// sessionDirs lists the log folders to look in for a session: the current
// project's, then those of the projects it has logged in.
func (a *app) sessionDirs(id string) []string {
	var dirs []string
	if dir, err := a.logDir(); err == nil {
		dirs = append(dirs, dir)
	}
	for _, root := range a.recorded(id) {
		// A session from before notes followed the current folder recorded the
		// folder it was launched in, which may be a subfolder.
		if r, ok := findProject(root); ok {
			root = r
		}
		if dir := filepath.Join(root, dirName); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// gitCmd runs git in dir with every GIT_* variable removed. Inside a git hook
// (validate runs this tool's tests from pre-commit) GIT_DIR and GIT_INDEX_FILE
// point at the hook's repo and would override -C.
func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	return cmd
}

func draftPath(dir, id string) string {
	return filepath.Join(dir, ".drafts", id+".md")
}

// lock serializes writers in one llm-sessions folder: parallel sessions in the
// same project, and parallel tool calls within one session. Closing the file
// releases the lock.
func lock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// findEntry returns the file in dir that holds session id's entry, if any.
func findEntry(dir, id string) (string, map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == indexName || !strings.HasSuffix(n, ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return "", nil, err
		}
		if fm := frontmatter(b); fm["session"] == id {
			return n, fm, nil
		}
	}
	return "", nil, nil
}

// updateIndex replaces the session's line (found by the file it links to) or,
// for a new session, inserts it above the newest entry.
func updateIndex(dir, oldName, name, line string) error {
	path := filepath.Join(dir, indexName)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b = []byte(indexHeader)
	} else if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	links := []string{"](" + name + ")"}
	if oldName != "" {
		links = append(links, "]("+oldName+")")
	}
	replaced := false
	for i, l := range lines {
		if strings.HasPrefix(l, "- ") && slices.ContainsFunc(links, func(link string) bool { return strings.Contains(l, link) }) {
			lines[i] = line
			replaced = true
			break
		}
	}
	if !replaced {
		at := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "- ") })
		if at < 0 {
			if lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			at = len(lines)
		}
		lines = slices.Insert(lines, at, line)
	}
	return writeAtomic(path, []byte(strings.Join(lines, "\n")+"\n"))
}

type draft struct {
	id, started string
	notes       int
	modified    time.Time
}

func (d draft) describe(now time.Time) string {
	notes := fmt.Sprintf("%d notes", d.notes)
	if d.notes == 1 {
		notes = "1 note"
	}
	return fmt.Sprintf("%s (started %s, %s, last note %s ago)", d.id, d.started, notes, ago(now.Sub(d.modified)))
}

// listDrafts returns the drafts in dir except exclude's, most recent first.
func listDrafts(dir, exclude string) ([]draft, error) {
	entries, err := os.ReadDir(filepath.Join(dir, ".drafts"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []draft
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() || id == exclude {
			continue
		}
		path := filepath.Join(dir, ".drafts", e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		d := draft{id: id, started: frontmatter(b)["started"], modified: info.ModTime()}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "- ") {
				d.notes++
			}
		}
		out = append(out, d)
	}
	slices.SortFunc(out, func(x, y draft) int { return y.modified.Compare(x.modified) })
	return out, nil
}

// pruneState drops sessions' project records older than stateMaxAge; Claude Code
// deletes transcripts after 30 days, so such a session can't be resumed.
func pruneState(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > stateMaxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// frontmatter parses the key: value lines between a file's opening --- lines.
func frontmatter(b []byte) map[string]string {
	m := map[string]string{}
	rest, ok := strings.CutPrefix(string(b), "---\n")
	if !ok {
		return m
	}
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return m
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if uq, err := strconv.Unquote(v); err == nil {
			v = uq
		}
		m[strings.TrimSpace(k)] = v
	}
	return m
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename has happened
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func escapeLink(s string) string {
	return strings.NewReplacer("[", `\[`, "]", `\]`).Replace(s)
}

func withNewline(b []byte) string {
	s := string(b)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
