package main

import (
	"bytes"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// config is where everything lives; tests point it at temp folders.
type config struct {
	root      string // the projects folder: <root>/<category>/<name>
	registry  string // projects.json
	vault     string // the Obsidian vault; skipped when it doesn't exist
	claudeDir string // ~/.claude
	home      string // expands ~ in vault frontmatter
	now       time.Time
}

const (
	scanWorkers  = 8
	walkCap      = 20000 // files looked at per project in a non-git folder
	dirtyWalkCap = 2000  // files looked at inside untracked folders of a repo
	maxRecent    = 5     // recent commits
	maxLogRecent = 3     // recent session-log entries
	maxVaultTask = 8
)

var (
	skipDirs = []string{".git", "node_modules", ".venv", "venv", "dist", "build", "target", ".wrangler", ".next", ".cache", "__pycache__"}
	docNames = []string{"README.md", "HANDOFF.md", "TODO.md", "NOTES.md", "PLAN.md", "CLAUDE.md", "AGENTS.md"}
	// vaultDirs are the vault folders searched for a note whose frontmatter
	// names a project folder.
	vaultDirs = []string{"Projects", "Archive", "MtG"}
)

// board is the /api/board payload and the snapshot's data.
type board struct {
	Generated  string   `json:"generated"`
	Root       string   `json:"root"`
	Registry   string   `json:"registry"`
	Statuses   []status `json:"statuses"`
	Categories []string `json:"categories"`
	Projects   []*card  `json:"projects"`
}

// card is one project on the board: the registry fields (all present, status
// "" when untriaged), plus what the scan found.
type card struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	Summary   string            `json:"summary"`
	LeftOff   string            `json:"left_off"`
	Next      string            `json:"next"`
	BlockedOn string            `json:"blocked_on"`
	OpenLoops []string          `json:"open_loops"`
	Tags      []string          `json:"tags"`
	Links     map[string]string `json:"links"`
	Related   []string          `json:"related"`
	Hidden    bool              `json:"hidden"`
	Pinned    bool              `json:"pinned"`
	Updated   string            `json:"updated"`
	Untriaged bool              `json:"untriaged"`
	Facts     facts             `json:"facts"`
	Warnings  []string          `json:"warnings"`
}

type facts struct {
	Path         string       `json:"path"`
	Category     string       `json:"category"`
	Exists       bool         `json:"exists"`
	LastActivity string       `json:"last_activity,omitempty"`
	DaysIdle     *int         `json:"days_idle,omitempty"`
	Git          *gitFacts    `json:"git,omitempty"`
	Docs         []string     `json:"docs"`
	Log          *logFacts    `json:"log,omitempty"`
	Claude       *claudeFacts `json:"claude,omitempty"`
	Vault        *vaultFacts  `json:"vault,omitempty"`

	last time.Time
}

type commit struct {
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

type gitFacts struct {
	Branch     string   `json:"branch"`
	Dirty      int      `json:"dirty"`
	Ahead      int      `json:"ahead"`
	Behind     int      `json:"behind"`
	Upstream   bool     `json:"upstream"`
	Remote     string   `json:"remote,omitempty"`
	LastCommit *commit  `json:"last_commit,omitempty"`
	Recent     []commit `json:"recent"`
	Commits30d int      `json:"commits_30d"`
}

type logEntry struct {
	Date    string `json:"date"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	File    string `json:"file,omitempty"`
}

type logFacts struct {
	Entries int        `json:"entries"`
	Recent  []logEntry `json:"recent"`
	Open    []string   `json:"open"`
}

type claudeFacts struct {
	Sessions int    `json:"sessions"`
	Last     string `json:"last,omitempty"`
	Memory   bool   `json:"memory"`
}

type vaultFacts struct {
	Note       string   `json:"note"`
	Heading    string   `json:"heading,omitempty"` // tasks come from this section only
	URL        string   `json:"url"`
	OpenTasks  []string `json:"open_tasks"`
	NextAction string   `json:"next_action,omitempty"`
}

// folders lists the project ids under the root (<category>/<name>, neither
// starting with "." and the category not llm-sessions) and the categories.
func folders(root string) (ids, categories []string, err error) {
	cats, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range cats {
		if !isDir(root, c) || skipName(c.Name()) || c.Name() == "llm-sessions" {
			continue
		}
		categories = append(categories, c.Name())
		dir := filepath.Join(root, c.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			// A category that is itself a repo has its session log beside the projects.
			if isDir(dir, e) && !skipName(e.Name()) && e.Name() != "llm-sessions" {
				ids = append(ids, c.Name()+"/"+e.Name())
			}
		}
	}
	return ids, categories, nil
}

func skipName(name string) bool { return strings.HasPrefix(name, ".") }

// isDir follows a symlink, so a linked project folder counts.
func isDir(parent string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink != 0 {
		st, err := os.Stat(filepath.Join(parent, e.Name()))
		return err == nil && st.IsDir()
	}
	return false
}

func (c *config) projectPath(id string) string {
	return filepath.Join(c.root, filepath.FromSlash(id))
}

// buildBoard scans every project folder and registry entry. With only set, it
// scans just those ids (show, and the POST response).
func (c *config) buildBoard(only ...string) (*board, error) {
	reg, err := loadRegistry(c.registry)
	if err != nil {
		return nil, err
	}
	ids, cats, err := folders(c.root)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, id := range ids {
		present[id] = true
	}
	entries := map[string]*project{}
	for _, p := range reg.Projects {
		if _, dup := entries[p.ID]; dup || p.ID == "" {
			continue // validate reports it; the first entry wins
		}
		entries[p.ID] = p
		if !present[p.ID] {
			ids = append(ids, p.ID)
			present[p.ID] = true
			if cat, _, ok := strings.Cut(p.ID, "/"); ok && !slices.Contains(cats, cat) {
				cats = append(cats, cat)
			}
		}
	}
	if len(only) > 0 {
		ids = slices.DeleteFunc(ids, func(id string) bool { return !slices.Contains(only, id) })
	}
	slices.Sort(ids)
	slices.Sort(cats)

	notes := c.vaultIndex()
	cards := make([]*card, len(ids))
	sem := make(chan struct{}, scanWorkers)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			cards[i] = c.makeCard(id, entries[id], notes)
		})
	}
	wg.Wait()
	if cats == nil {
		cats = []string{}
	}
	return &board{
		Generated:  c.now.Format(time.RFC3339),
		Root:       c.root,
		Registry:   c.registry,
		Statuses:   statuses,
		Categories: cats,
		Projects:   cards,
	}, nil
}

func (c *config) makeCard(id string, p *project, notes map[string]string) *card {
	cd := &card{ID: id, Untriaged: p == nil}
	if p == nil {
		cd.Name = filepath.Base(filepath.FromSlash(id))
	} else {
		cd.Name, cd.Status, cd.Summary, cd.LeftOff, cd.Next, cd.BlockedOn = p.Name, p.Status, p.Summary, p.LeftOff, p.Next, p.BlockedOn
		cd.OpenLoops, cd.Tags, cd.Related = p.OpenLoops, p.Tags, p.Related
		cd.Links = maps(p.Links)
		cd.Hidden, cd.Pinned, cd.Updated = p.Hidden, p.Pinned, p.Updated
	}
	cd.OpenLoops, cd.Tags, cd.Related = nonNil(cd.OpenLoops), nonNil(cd.Tags), nonNil(cd.Related)
	if cd.Links == nil {
		cd.Links = map[string]string{}
	}
	cd.Facts = c.scanFacts(id, cd.Links["vault"], notes)
	cd.Warnings = warnings(cd)
	return cd
}

func maps(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// warnings flags what needs attention, in a fixed order.
func warnings(cd *card) []string {
	w := []string{}
	f := &cd.Facts
	if cd.Untriaged {
		w = append(w, "untriaged")
	} else if !f.Exists {
		w = append(w, "missing")
	}
	if g := f.Git; g != nil {
		if g.Dirty > 0 {
			w = append(w, "uncommitted")
		}
		if g.Ahead > 0 || (!g.Upstream && g.LastCommit != nil) {
			w = append(w, "unpushed")
		}
	}
	if cd.Status == "active" && f.DaysIdle != nil && *f.DaysIdle > 21 {
		w = append(w, "stale")
	}
	// The board shows the vault's next action in place of next, so either counts.
	hasNext := cd.Next != "" || (f.Vault != nil && f.Vault.NextAction != "")
	if (cd.Status == "active" || cd.Status == "paused" || cd.Status == "waiting") && !hasNext {
		w = append(w, "no-next")
	}
	return w
}

func (c *config) scanFacts(id, vaultLink string, notes map[string]string) facts {
	path := c.projectPath(id)
	cat, _, _ := strings.Cut(id, "/")
	f := facts{Path: path, Category: cat, Docs: []string{}}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		f.Exists = true
	}
	if f.Exists {
		for _, d := range docNames {
			if st, err := os.Stat(filepath.Join(path, d)); err == nil && !st.IsDir() {
				f.Docs = append(f.Docs, d)
			}
		}
		// A project is its own repo, or a folder in its category's (mtg/zeta-proxies
		// in the repo mtg), which has the session log too.
		top := path
		if !exists(filepath.Join(path, ".git")) && exists(filepath.Join(filepath.Dir(path), ".git")) {
			top = filepath.Dir(path)
		}
		if exists(filepath.Join(top, ".git")) {
			var newest time.Time
			f.Git, newest = scanGit(path, top)
			f.bump(newest)
		}
		if f.Git == nil {
			f.bump(newestFile(path, walkCap))
		}
		var newest time.Time
		if f.Log, newest = scanLog(filepath.Join(path, "llm-sessions")); f.Log == nil && top != path {
			f.Log, newest = scanLog(filepath.Join(top, "llm-sessions"))
		}
		f.bump(newest)
	}
	var newest time.Time
	f.Claude, newest = c.scanClaude(path)
	f.bump(newest)
	if f.last.IsZero() && f.Exists {
		// An empty folder: when it was made or last emptied.
		if st, err := os.Stat(path); err == nil {
			f.bump(st.ModTime())
		}
	}
	f.Vault = c.scanVault(path, vaultLink, notes)
	if !f.last.IsZero() {
		f.LastActivity = f.last.Format(time.RFC3339)
		days := daysBetween(f.last, c.now)
		f.DaysIdle = &days
	}
	return f
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (f *facts) bump(t time.Time) {
	if t.After(f.last) {
		f.last = t
	}
}

// daysBetween counts calendar days from t to now in local time; 0 is today.
func daysBetween(t, now time.Time) int {
	t, now = t.In(time.Local), now.In(time.Local)
	a := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	b := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	days := int(b.Sub(a).Hours()/24 + 0.5)
	return max(days, 0)
}

// newestFile walks dir for the newest file's mtime, skipping build output and
// dependency folders, and stops after limit files.
func newestFile(dir string, limit int) time.Time {
	var newest time.Time
	n := 0
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && slices.Contains(skipDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".DS_Store" {
			return nil
		}
		if n++; n > limit {
			return filepath.SkipAll
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

// gitCmd runs git in dir with every GIT_* variable removed: inside a git hook
// (validate runs the tests from pre-commit) GIT_DIR and GIT_INDEX_FILE point at
// the hook's repo and would override -C. GIT_OPTIONAL_LOCKS=0 keeps git status
// from refreshing the index, so a scan never writes to a project.
func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := gitCmd(dir, args...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

// scanGit reads a repo's state; newest is the latest of its last commit and the
// mtimes of its changed and untracked files. top is the repo's root; for a dir below it, changed files and
// commits are those under dir, while ahead and behind are the whole repo's.
func scanGit(dir, top string) (*gitFacts, time.Time) {
	var under []string
	if dir != top {
		under = []string{"--", "."}
	}
	out, err := gitCmd(dir, append([]string{"status", "--porcelain=v2", "--branch", "-z"}, under...)...).Output()
	if err != nil {
		return nil, time.Time{}
	}
	g := &gitFacts{Recent: []commit{}}
	st := parseStatus(out)
	g.Branch, g.Ahead, g.Behind, g.Upstream, g.Dirty = st.branch, st.ahead, st.behind, st.upstream, len(st.paths)
	var newest time.Time
	budget := dirtyWalkCap
	for _, p := range st.paths {
		full := filepath.Join(top, filepath.FromSlash(strings.TrimSuffix(p, "/")))
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if info.IsDir() && budget > 0 {
			if t := newestFile(full, budget); t.After(newest) {
				newest = t
			}
			budget /= 2
		}
	}
	if log, err := gitOut(dir, append([]string{"log", "-n", strconv.Itoa(maxRecent), "--format=%cI%x09%s"}, under...)...); err == nil {
		for _, line := range strings.Split(log, "\n") {
			date, subject, ok := strings.Cut(line, "\t")
			if !ok {
				continue
			}
			g.Recent = append(g.Recent, commit{Date: date, Subject: subject})
		}
	}
	if len(g.Recent) > 0 {
		last := g.Recent[0]
		g.LastCommit = &last
		if t, err := time.Parse(time.RFC3339, last.Date); err == nil && t.After(newest) {
			newest = t
		}
		if n, err := gitOut(dir, append([]string{"rev-list", "--count", "--since=30.days.ago", "HEAD"}, under...)...); err == nil {
			g.Commits30d, _ = strconv.Atoi(strings.TrimSpace(n))
		}
	}
	if remote, err := gitOut(dir, "config", "--get", "remote.origin.url"); err == nil {
		g.Remote = httpsRemote(strings.TrimSpace(remote))
	}
	return g, newest
}

type gitStatus struct {
	branch        string
	ahead, behind int
	upstream      bool
	paths         []string // changed and untracked
}

// parseStatus reads `git status --porcelain=v2 --branch -z`.
func parseStatus(out []byte) gitStatus {
	var st gitStatus
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case strings.HasPrefix(f, "# branch.head "):
			st.branch = strings.TrimPrefix(f, "# branch.head ")
		case strings.HasPrefix(f, "# branch.upstream "):
			st.upstream = true
		case strings.HasPrefix(f, "# branch.ab "):
			for _, n := range strings.Fields(strings.TrimPrefix(f, "# branch.ab ")) {
				v, _ := strconv.Atoi(n[1:])
				if n[0] == '+' {
					st.ahead = v
				} else {
					st.behind = v
				}
			}
		case strings.HasPrefix(f, "1 "):
			if parts := strings.SplitN(f, " ", 9); len(parts) == 9 {
				st.paths = append(st.paths, parts[8])
			}
		case strings.HasPrefix(f, "2 "):
			if parts := strings.SplitN(f, " ", 10); len(parts) == 10 {
				st.paths = append(st.paths, parts[9])
			}
			i++ // the rename's original path follows as its own field
		case strings.HasPrefix(f, "u "):
			if parts := strings.SplitN(f, " ", 11); len(parts) == 11 {
				st.paths = append(st.paths, parts[10])
			}
		case strings.HasPrefix(f, "? "):
			st.paths = append(st.paths, f[2:])
		}
	}
	return st
}

var scpRemote = regexp.MustCompile(`^(?:ssh://)?git@github\.com[:/](.+?)(?:\.git)?/?$`)

// httpsRemote turns a GitHub ssh remote into its https URL; others pass through.
func httpsRemote(r string) string {
	if m := scpRemote.FindStringSubmatch(r); m != nil {
		return "https://github.com/" + m[1]
	}
	if strings.HasPrefix(r, "https://github.com/") {
		return strings.TrimSuffix(r, ".git")
	}
	return r
}

var indexLine = regexp.MustCompile(`^- (\S+) \[((?:\\.|[^\]\\])*)\]\(([^)]*)\):?\s*(.*)$`)

// parseIndex reads an llm-sessions INDEX.md: one "- DATE [TITLE](FILE): SUMMARY"
// line per session, newest first.
func parseIndex(b []byte) []logEntry {
	var out []logEntry
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if m := indexLine.FindStringSubmatch(line); m != nil {
			title := strings.NewReplacer(`\[`, "[", `\]`, "]").Replace(m[2])
			out = append(out, logEntry{Date: m[1], Title: title, File: m[3], Summary: m[4]})
		} else {
			out = append(out, logEntry{Title: strings.TrimPrefix(line, "- ")})
		}
	}
	return out
}

var (
	openHeading = regexp.MustCompile(`^#{2,3}\s+Open\s*$`)
	anyHeading  = regexp.MustCompile(`^#{1,6}\s`)
	bulletStart = regexp.MustCompile(`^[-*+]\s+`)
)

// openNotes returns the bullets under the "## Open" headings of a session file
// (a resumed session repeats them as "### Open"), joining wrapped lines.
func openNotes(b []byte) []string {
	var out []string
	in, cur := false, -1
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case anyHeading.MatchString(line):
			in, cur = openHeading.MatchString(line), -1
		case !in:
		case strings.TrimSpace(line) == "":
			cur = -1
		case bulletStart.MatchString(line):
			out = append(out, bulletStart.ReplaceAllString(line, ""))
			cur = len(out) - 1
		case cur >= 0 && (line[0] == ' ' || line[0] == '\t'):
			out[cur] += " " + strings.TrimSpace(line)
		}
	}
	return cleanList(out)
}

var datedFile = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-.*\.md$`)

// scanLog reads a project's llm-sessions folder. newest is the newest mtime in
// it, drafts included.
func scanLog(dir string) (*logFacts, time.Time) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, time.Time{}
	}
	lf := &logFacts{Recent: []logEntry{}, Open: []string{}}
	newest := newestFile(dir, walkCap)
	b, _ := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	entries := parseIndex(b)
	lf.Entries = len(entries)
	lf.Recent = append(lf.Recent, entries[:min(len(entries), maxLogRecent)]...)

	// The newest session file is the one INDEX.md lists first; failing that,
	// the latest dated file.
	file := ""
	if len(entries) > 0 && entries[0].File != "" && !strings.Contains(entries[0].File, "/") {
		if _, err := os.Stat(filepath.Join(dir, entries[0].File)); err == nil {
			file = entries[0].File
		}
	}
	if file == "" {
		names, _ := os.ReadDir(dir)
		for _, n := range names {
			if !n.IsDir() && datedFile.MatchString(n.Name()) && n.Name() > file {
				file = n.Name()
			}
		}
	}
	if file != "" {
		if b, err := os.ReadFile(filepath.Join(dir, file)); err == nil {
			lf.Open = nonNil(openNotes(b))
		}
	}
	return lf, newest
}

// claudeSlug is the name Claude Code gives a folder under ~/.claude/projects:
// the absolute path with every character outside [A-Za-z0-9] replaced by "-".
func claudeSlug(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (c *config) scanClaude(path string) (*claudeFacts, time.Time) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // Claude Code names the folder after the resolved path
	}
	dir := filepath.Join(c.claudeDir, "projects", claudeSlug(path))
	cf := &claudeFacts{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return cf, time.Time{}
	}
	var newest time.Time
	for _, e := range entries {
		if e.IsDir() {
			if e.Name() == "memory" {
				cf.Memory = true
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		cf.Sessions++
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if !newest.IsZero() {
		cf.Last = newest.Format(time.RFC3339)
	}
	return cf, newest
}

// vaultIndex maps project folders to the vault notes whose frontmatter names
// them with `folder: <path>`. Empty when there is no vault.
func (c *config) vaultIndex() map[string]string {
	idx := map[string]string{}
	if c.vault == "" {
		return idx
	}
	for _, sub := range vaultDirs {
		base := filepath.Join(c.vault, sub)
		filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != base && skipName(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			b, err := readHead(p, 8192)
			if err != nil {
				return nil
			}
			folder := frontmatter(b)["folder"]
			if folder == "" {
				return nil
			}
			rel, err := filepath.Rel(c.vault, p)
			if err != nil {
				return nil
			}
			key := c.expandHome(folder)
			if _, taken := idx[key]; !taken {
				idx[key] = filepath.ToSlash(rel)
			}
			return nil
		})
	}
	return idx
}

func (c *config) expandHome(p string) string {
	if p == "~" {
		p = c.home
	} else if rest, ok := strings.CutPrefix(p, "~/"); ok {
		p = filepath.Join(c.home, rest)
	}
	return filepath.Clean(p)
}

// readHead reads at most n bytes of a file: enough for its frontmatter.
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	k, err := f.Read(buf)
	if err != nil && k == 0 {
		return nil, err
	}
	return buf[:k], nil
}

// frontmatter parses the top-level key: value lines between a file's opening
// --- lines; nested and list lines are skipped.
func frontmatter(b []byte) map[string]string {
	m := map[string]string{}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return m
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return m
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '-' || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		m[strings.TrimSpace(k)] = v
	}
	return m
}

var openTask = regexp.MustCompile(`^\s*[-*+] \[ \] (.*\S)`)

// scanVault finds the project's vault note: the registry's links.vault, else a
// note whose frontmatter names the folder. It lists the note's open tasks. A
// links.vault of "Note.md#Heading" takes the tasks under that heading only, for
// a note several projects share.
func (c *config) scanVault(path, link string, notes map[string]string) *vaultFacts {
	if c.vault == "" {
		return nil
	}
	if st, err := os.Stat(c.vault); err != nil || !st.IsDir() {
		return nil
	}
	note := ""
	link, heading, _ := strings.Cut(link, "#")
	heading = strings.TrimSpace(heading)
	if link != "" && !strings.Contains(link, "://") {
		rel := strings.TrimPrefix(filepath.ToSlash(link), "/")
		if !strings.HasSuffix(rel, ".md") {
			rel += ".md"
		}
		if !strings.Contains("/"+rel, "/../") {
			if _, err := os.Stat(filepath.Join(c.vault, filepath.FromSlash(rel))); err == nil {
				note = rel
			}
		}
	}
	if note == "" {
		note, heading = notes[filepath.Clean(path)], ""
	}
	if note == "" {
		return nil
	}
	vf := &vaultFacts{Note: note, Heading: heading, URL: obsidianURL(filepath.Base(c.vault), note)}
	b, err := os.ReadFile(filepath.Join(c.vault, filepath.FromSlash(note)))
	if err != nil {
		vf.OpenTasks = []string{}
		return vf
	}
	vf.OpenTasks = openTasks(b, heading)
	if len(vf.OpenTasks) > 0 {
		vf.NextAction = vf.OpenTasks[0]
	}
	return vf
}

// openTasks lists a note's "- [ ] " tasks (the text after the box), at most
// maxVaultTask; with heading set, only those in that heading's section,
// subheadings included.
func openTasks(b []byte, heading string) []string {
	tasks := []string{}
	in, level := heading == "", 0
	for _, line := range strings.Split(string(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))), "\n") {
		if heading != "" {
			if m := headingLine.FindStringSubmatch(line); m != nil {
				switch {
				case in && len(m[1]) <= level:
					in = false
				case !in && strings.EqualFold(strings.TrimSpace(m[2]), heading):
					in, level = true, len(m[1])
				}
				continue
			}
		}
		if m := openTask.FindStringSubmatch(line); in && m != nil && len(tasks) < maxVaultTask {
			tasks = append(tasks, m[1])
		}
	}
	return tasks
}

var headingLine = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)

// obsidianURL opens a note in Obsidian: obsidian://open?vault=…&file=… with the
// file named without its .md, both percent-encoded (%20 for spaces).
func obsidianURL(vault, note string) string {
	return "obsidian://open?vault=" + queryEscape(vault) + "&file=" + queryEscape(strings.TrimSuffix(note, ".md"))
}

// queryEscape escapes a query value with %20 for spaces; QueryEscape turns a
// literal + into %2B, so every + it leaves was a space.
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
