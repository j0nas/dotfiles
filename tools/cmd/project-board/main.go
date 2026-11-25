// Command project-board is a kanban board over every project folder under
// ~/Desktop/projects/<category>/<name>. For each project it shows what it is,
// which state it is in, where Jonas left off and the next concrete step, plus
// facts scanned fresh from disk: git state, Claude Code sessions, the
// llm-sessions log and open tasks in the linked Obsidian note.
//
// The curated part lives in <root>/projects.json, the registry, which people
// and Claude edit (by hand, with `project-board set`, or from the board in the
// browser). Scanned facts are never stored. `serve` runs the board on
// localhost from the embedded board.html; `html` writes a read-only snapshot
// of it with the data inlined.
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const usage = `project-board: a kanban board over ~/Desktop/projects/<category>/<name>.

The curated state (status, summary, where you left off, next step) lives in
<root>/projects.json; git, Claude sessions, the llm-sessions log and Obsidian
tasks are scanned fresh every time.

  project-board [serve] [--port N] [--no-open]
                         serve the board on 127.0.0.1 (default port 7777) and
                         open it in the browser
  project-board list [--all]
                         the board as text, grouped by status; hidden, done and
                         dropped projects only with --all
  project-board show [ID|PATH]
                         one project's card as text
  project-board set [ID|PATH] [--status S] [--summary T] [--left-off T]
                    [--next T] [--blocked-on T] [--name T] [--add-loop T]
                    [--drop-loop N|TEXT] [--tag T] [--pin|--unpin] [--hide|--unhide]
                         change curated fields. --add-loop, --drop-loop and --tag
                         repeat; --tag -T removes a tag; --drop-loop takes the
                         loop's number (as show lists it) or its text. A folder
                         with no entry gets one (status paused unless given).
  project-board scan [--json]
                         print the scan; --json prints the /api/board payload
  project-board html [-o FILE]
                         write the static snapshot (default <root>/board.html)
  project-board validate check projects.json: parse, unique ids, known statuses,
                         ids that match no folder, field lengths

A registry links.vault of "Note.md#Heading" takes the note's open tasks from
that section only, for a vault note several projects share.

ID|PATH is an id (code/edh.land), a unique folder or project name (edh.land),
or a path; any path inside a project means that project. show and set default
to the project of the current folder.

Statuses, in column order: active, waiting, paused, idea, live, done, dropped.

Every command takes --root DIR (default $PROJECT_BOARD_ROOT, else
~/Desktop/projects) and --registry FILE (default $PROJECT_BOARD_REGISTRY, else
<root>/projects.json). The vault is $PROJECT_BOARD_VAULT, else Jonas' vault in
iCloud; it is skipped when absent.
`

//go:embed board.html
var boardHTML []byte

// placeholder is where the snapshot inlines the board's JSON; the live board
// leaves it null and fetches /api/board.
const placeholder = `<script id="board-data" type="application/json">null</script>`

type app struct {
	stdout, stderr io.Writer
	getenv         func(string) string
	cwd            string
	now            func() time.Time
	openURL        func(string) error

	root, registry string // --root, --registry
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "project-board:", err)
		os.Exit(1)
	}
	a := &app{stdout: os.Stdout, stderr: os.Stderr, getenv: os.Getenv, cwd: cwd, now: time.Now, openURL: openBrowser}
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	args, err := a.leadingGlobals(args)
	cmd := "serve"
	if err == nil && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if err == nil {
		switch cmd {
		case "serve":
			err = a.serve(args)
		case "list":
			err = a.list(args)
		case "show":
			err = a.show(args)
		case "set":
			err = a.set(args)
		case "scan":
			err = a.scan(args)
		case "html":
			err = a.html(args)
		case "validate":
			err = a.validate(args)
		case "help":
			fmt.Fprint(a.stdout, usage)
		default:
			err = fmt.Errorf("unknown command %q (see project-board --help)", cmd)
		}
	}
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.stdout, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintln(a.stderr, "project-board:", err)
		return 1
	}
	return 0
}

// leadingGlobals takes --root and --registry given before the command.
func (a *app) leadingGlobals(args []string) ([]string, error) {
	for len(args) > 0 {
		name, val, hasVal := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
		if !strings.HasPrefix(args[0], "-") || (name != "root" && name != "registry") {
			break
		}
		if !hasVal {
			if len(args) < 2 {
				return nil, fmt.Errorf("flag needs an argument: -%s", name)
			}
			val, args = args[1], args[1:]
		}
		args = args[1:]
		if name == "root" {
			a.root = val
		} else {
			a.registry = val
		}
	}
	return args, nil
}

// flags makes a command's flag set with the global flags on it.
func (a *app) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&a.root, "root", a.root, "projects folder")
	fs.StringVar(&a.registry, "registry", a.registry, "projects.json")
	return fs
}

// parse parses flags and positional arguments in any order.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, fmt.Errorf("%s: %w", fs.Name(), err)
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func noArgs(fs *flag.FlagSet, args []string) error {
	pos, err := parse(fs, args)
	if err == nil && len(pos) > 0 {
		err = fmt.Errorf("%s: unexpected argument %q", fs.Name(), pos[0])
	}
	return err
}

func (a *app) config() *config {
	home := a.getenv("HOME")
	c := &config{home: home, now: a.now()}
	c.root = firstOf(a.root, a.getenv("PROJECT_BOARD_ROOT"), filepath.Join(home, "Desktop", "projects"))
	c.root = a.abs(c.expandHome(c.root))
	c.registry = a.abs(c.expandHome(firstOf(a.registry, a.getenv("PROJECT_BOARD_REGISTRY"), filepath.Join(c.root, "projects.json"))))
	c.vault = firstOf(a.getenv("PROJECT_BOARD_VAULT"), filepath.Join(home, "Library", "Mobile Documents", "iCloud~md~obsidian", "Documents", "Jonas' vault"))
	c.vault = a.abs(c.expandHome(c.vault))
	c.claudeDir = a.abs(c.expandHome(firstOf(a.getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude"))))
	return c
}

func (a *app) abs(p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(a.cwd, p)
	}
	return filepath.Clean(p)
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolve turns ID|PATH into a project id: an id, a path (".", relative,
// absolute; anywhere inside a project), or a unique folder or project name.
// An empty arg is the current folder.
func (a *app) resolve(c *config, reg *registry, arg string) (string, error) {
	ids, _, err := folders(c.root)
	if err != nil {
		return "", err
	}
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	for _, p := range reg.Projects {
		if p.ID != "" {
			known[p.ID] = true
		}
	}
	if arg == "" {
		return a.pathID(c, a.cwd)
	}
	if known[arg] {
		return arg, nil
	}
	if strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "~") || filepath.IsAbs(arg) {
		return a.pathID(c, c.expandHome(arg))
	}
	if strings.Contains(arg, "/") {
		if _, err := os.Stat(a.abs(arg)); err == nil {
			return a.pathID(c, arg)
		}
		return "", fmt.Errorf("no project %q under %s", arg, c.root)
	}
	var matches []string
	for _, fold := range []bool{false, true} {
		for id := range known {
			name := id[strings.LastIndex(id, "/")+1:]
			if p := reg.find(id); p != nil && (p.Name == arg || fold && strings.EqualFold(p.Name, arg)) {
				name = arg
			}
			if name == arg || fold && strings.EqualFold(name, arg) {
				matches = append(matches, id)
			}
		}
		if len(matches) > 0 {
			break
		}
	}
	slices.Sort(matches)
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return "", fmt.Errorf("%q matches several projects: %s", arg, strings.Join(matches, ", "))
	}
	if _, err := os.Stat(a.abs(arg)); err == nil {
		return a.pathID(c, arg)
	}
	return "", fmt.Errorf("no project %q under %s", arg, c.root)
}

// pathID maps any path inside <root>/<category>/<name> to that project's id.
func (a *app) pathID(c *config, p string) (string, error) {
	p = a.abs(p)
	rel, ok := within(c.root, p)
	if !ok {
		rp, err1 := filepath.EvalSymlinks(p)
		rr, err2 := filepath.EvalSymlinks(c.root)
		if err1 == nil && err2 == nil {
			rel, ok = within(rr, rp)
		}
	}
	if !ok {
		return "", fmt.Errorf("%s isn't inside %s", p, c.root)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("%s is a category folder, not a project: cd into a project or name one", p)
	}
	if skipName(parts[0]) || skipName(parts[1]) || parts[0] == "llm-sessions" {
		return "", fmt.Errorf("%s isn't a project folder", p)
	}
	return parts[0] + "/" + parts[1], nil
}

// within returns p relative to root when p is strictly inside it.
func within(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func (a *app) list(args []string) error {
	fs := a.flags("list")
	all := fs.Bool("all", false, "include hidden, done and dropped")
	if err := noArgs(fs, args); err != nil {
		return err
	}
	b, err := a.config().buildBoard()
	if err != nil {
		return err
	}
	type column struct {
		id, label string
		cards     []*card
	}
	cols := []*column{{id: "", label: "Inbox"}}
	for _, s := range statuses {
		cols = append(cols, &column{id: s.ID, label: s.Label})
	}
	hiddenCount := 0
	for _, cd := range b.Projects {
		show := *all || !(cd.Hidden || cd.Status == "done" || cd.Status == "dropped")
		if !show {
			hiddenCount++
			continue
		}
		i := slices.IndexFunc(cols, func(c *column) bool { return c.id == cd.Status })
		if i < 0 {
			cols = append(cols, &column{id: cd.Status, label: cd.Status + " (unknown status)"})
			i = len(cols) - 1
		}
		cols[i].cards = append(cols[i].cards, cd)
	}
	width := 0
	for _, col := range cols {
		for _, cd := range col.cards {
			width = max(width, min(utf8.RuneCountInString(cd.ID), 34))
		}
	}
	first := true
	for _, col := range cols {
		if len(col.cards) == 0 {
			continue
		}
		sortCards(col.cards)
		if !first {
			fmt.Fprintln(a.stdout)
		}
		first = false
		fmt.Fprintf(a.stdout, "%s (%d)\n", col.label, len(col.cards))
		for _, cd := range col.cards {
			mark := " "
			if cd.Pinned {
				mark = "*"
			}
			text := cd.Summary
			if text == "" && !cd.Untriaged {
				text = cd.Name
			}
			line := fmt.Sprintf(" %s %-*s  %-5s  %s", mark, width, cd.ID, idle(cd.Facts.DaysIdle), text)
			if cd.Hidden {
				line += " (hidden)"
			}
			if w := slices.DeleteFunc(slices.Clone(cd.Warnings), func(s string) bool { return s == "untriaged" }); len(w) > 0 {
				line += "  [" + strings.Join(w, ", ") + "]"
			}
			fmt.Fprintln(a.stdout, strings.TrimRight(line, " "))
			if next := nextStep(cd); next != "" {
				fmt.Fprintf(a.stdout, "   %*s  next: %s\n", width+7, "", next)
			}
		}
	}
	if hiddenCount > 0 {
		fmt.Fprintf(a.stdout, "\n%d hidden, done or dropped (--all shows them)\n", hiddenCount)
	}
	return nil
}

// sortCards orders a column as the board does: pinned first, then by last
// activity, newest first.
func sortCards(cards []*card) {
	slices.SortStableFunc(cards, func(x, y *card) int {
		if x.Pinned != y.Pinned {
			if x.Pinned {
				return -1
			}
			return 1
		}
		if c := y.Facts.last.Compare(x.Facts.last); c != 0 {
			return c
		}
		return strings.Compare(x.ID, y.ID)
	})
}

// nextStep is what the board shows as next: the registry's next, else the
// vault note's first open task. The registry wins because it is rewritten at
// every session end, while a vault note's top task can sit there for months.
func nextStep(cd *card) string {
	if cd.Next != "" {
		return cd.Next
	}
	if v := cd.Facts.Vault; v != nil {
		return v.NextAction
	}
	return ""
}

// idle is the board's idle badge: today, 3d, 5w, 4mo.
func idle(days *int) string {
	switch {
	case days == nil:
		return "?"
	case *days == 0:
		return "today"
	case *days < 14:
		return fmt.Sprintf("%dd", *days)
	case *days < 60:
		return fmt.Sprintf("%dw", *days/7)
	default:
		return fmt.Sprintf("%dmo", *days/30)
	}
}

func (a *app) show(args []string) error {
	fs := a.flags("show")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("show: unexpected argument %q", pos[1])
	}
	c := a.config()
	reg, err := loadRegistry(c.registry)
	if err != nil {
		return err
	}
	id, err := a.resolve(c, reg, firstOf(pos...))
	if err != nil {
		return err
	}
	b, err := c.buildBoard(id)
	if err != nil {
		return err
	}
	if len(b.Projects) == 0 {
		return fmt.Errorf("no project %s", id)
	}
	printCard(a.stdout, b.Projects[0])
	return nil
}

var warningText = map[string]string{
	"untriaged":   "no entry in projects.json yet",
	"missing":     "the folder is gone",
	"uncommitted": "uncommitted changes",
	"unpushed":    "commits not pushed",
	"stale":       "active but untouched for over 3 weeks",
	"no-next":     "no next step",
}

func printCard(w io.Writer, cd *card) {
	f := cd.Facts
	st := cd.Status
	if cd.Untriaged {
		st = "untriaged"
	}
	head := fmt.Sprintf("%s (%s)  %s", cd.Name, cd.ID, st)
	if cd.Pinned {
		head += ", pinned"
	}
	if cd.Hidden {
		head += ", hidden"
	}
	fmt.Fprintln(w, head)
	if cd.Summary != "" {
		fmt.Fprintln(w, cd.Summary)
	}
	fmt.Fprintln(w)
	field := func(label, v string) {
		if v != "" {
			fmt.Fprintf(w, "%-15s%s\n", label+":", v)
		}
	}
	field("left off", cd.LeftOff)
	field("next", cd.Next)
	if v := f.Vault; v != nil && v.NextAction != "" && v.NextAction != cd.Next {
		field("vault next", v.NextAction)
	}
	field("blocked on", cd.BlockedOn)
	if len(cd.OpenLoops) > 0 {
		fmt.Fprintln(w, "open loops:")
		for i, l := range cd.OpenLoops {
			fmt.Fprintf(w, "  %d. %s\n", i+1, l)
		}
	}
	field("tags", strings.Join(cd.Tags, ", "))
	if len(cd.Links) > 0 {
		keys := make([]string, 0, len(cd.Links))
		for k := range cd.Links {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		fmt.Fprintln(w, "links:")
		for _, k := range keys {
			fmt.Fprintf(w, "  %-8s %s\n", k, cd.Links[k])
		}
	}
	field("related", strings.Join(cd.Related, ", "))
	field("updated", cd.Updated)
	fmt.Fprintln(w)

	field("path", f.Path)
	if !f.Exists {
		fmt.Fprintln(w, "(the folder doesn't exist)")
	}
	if f.LastActivity != "" {
		field("last activity", fmt.Sprintf("%s (%s)", shortTime(f.LastActivity), idle(f.DaysIdle)))
	}
	if g := f.Git; g != nil {
		desc := fmt.Sprintf("%s, %d changed, %d ahead, %d behind", g.Branch, g.Dirty, g.Ahead, g.Behind)
		if !g.Upstream {
			desc += ", no upstream"
		}
		desc += fmt.Sprintf(", %d commits in 30 days", g.Commits30d)
		field("git", desc)
		field("remote", g.Remote)
		for _, c := range g.Recent {
			fmt.Fprintf(w, "  %s  %s\n", shortTime(c.Date), c.Subject)
		}
	}
	field("docs", strings.Join(f.Docs, ", "))
	if l := f.Log; l != nil {
		field("session log", fmt.Sprintf("%d entries", l.Entries))
		for _, e := range l.Recent {
			fmt.Fprintf(w, "  %s  %s\n", e.Date, e.Title)
		}
		if len(l.Open) > 0 {
			fmt.Fprintln(w, "  open in the latest session:")
			for _, o := range l.Open {
				fmt.Fprintf(w, "  - %s\n", o)
			}
		}
	}
	if cl := f.Claude; cl != nil && cl.Sessions > 0 {
		desc := fmt.Sprintf("%d sessions, last %s", cl.Sessions, shortTime(cl.Last))
		if cl.Memory {
			desc += ", has memory"
		}
		field("claude", desc)
	}
	if v := f.Vault; v != nil {
		note := v.Note
		if v.Heading != "" {
			note += " # " + v.Heading
		}
		field("vault", note)
		for _, t := range v.OpenTasks {
			fmt.Fprintf(w, "  [ ] %s\n", t)
		}
	}
	if len(cd.Warnings) > 0 {
		var ws []string
		for _, x := range cd.Warnings {
			ws = append(ws, firstOf(warningText[x], x))
		}
		field("warnings", strings.Join(ws, "; "))
	}
	if f.Exists {
		field("resume", fmt.Sprintf("cd %s && claude --continue", shellQuote(f.Path)))
	}
}

func shortTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.In(time.Local).Format("2006-01-02 15:04")
}

func shellQuote(s string) string {
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '~' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// listFlag collects a repeated string flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ", ") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func (a *app) set(args []string) error {
	fs := a.flags("set")
	str := map[string]*string{}
	for flagName, field := range map[string]string{"status": "status", "summary": "summary", "left-off": "left_off", "next": "next", "blocked-on": "blocked_on", "name": "name"} {
		str[field] = fs.String(flagName, "", field)
	}
	var addLoops, dropLoops, tags listFlag
	fs.Var(&addLoops, "add-loop", "add an open loop")
	fs.Var(&dropLoops, "drop-loop", "drop an open loop by number or text")
	fs.Var(&tags, "tag", "add a tag (-T removes it)")
	pin := fs.Bool("pin", false, "")
	unpin := fs.Bool("unpin", false, "")
	hide := fs.Bool("hide", false, "")
	unhide := fs.Bool("unhide", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("set: unexpected argument %q (quote text values)", pos[1])
	}
	if *pin && *unpin || *hide && *unhide {
		return errors.New("set: --pin/--unpin and --hide/--unhide each take one side")
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	c := a.config()
	reg, err := loadRegistry(c.registry)
	if err != nil {
		return err
	}
	id, err := a.resolve(c, reg, firstOf(pos...))
	if err != nil {
		return err
	}
	p := reg.find(id)
	created := false
	if p == nil {
		if st, err := os.Stat(c.projectPath(id)); err != nil || !st.IsDir() {
			return fmt.Errorf("no folder %s", c.projectPath(id))
		}
		p = &project{ID: id, Name: filepath.Base(c.projectPath(id)), Status: "paused"}
		reg.Projects = append(reg.Projects, p)
		created = true
	}

	patch := map[string]json.RawMessage{}
	var changed []string
	for _, field := range []string{"name", "status", "summary", "left_off", "next", "blocked_on"} {
		flagName := strings.ReplaceAll(field, "_", "-")
		if given[flagName] {
			patch[field], _ = json.Marshal(*str[field])
			changed = append(changed, field)
		}
	}
	for flagName, v := range map[string]bool{"pin": true, "unpin": false} {
		if given[flagName] {
			patch["pinned"], _ = json.Marshal(v)
			changed = append(changed, "pinned")
		}
	}
	for flagName, v := range map[string]bool{"hide": true, "unhide": false} {
		if given[flagName] {
			patch["hidden"], _ = json.Marshal(v)
			changed = append(changed, "hidden")
		}
	}
	if len(dropLoops) > 0 || len(addLoops) > 0 {
		loops, err := editLoops(p.OpenLoops, dropLoops, addLoops)
		if err != nil {
			return err
		}
		patch["open_loops"], _ = json.Marshal(loops)
		changed = append(changed, "open_loops")
	}
	if len(tags) > 0 {
		t := slices.Clone(p.Tags)
		for _, tag := range tags {
			if rm, ok := strings.CutPrefix(tag, "-"); ok {
				t = slices.DeleteFunc(t, func(x string) bool { return x == rm })
			} else {
				t = append(t, tag)
			}
		}
		patch["tags"], _ = json.Marshal(t)
		changed = append(changed, "tags")
	}
	if len(patch) == 0 && !created {
		return errors.New("set: nothing to change (see project-board --help)")
	}
	if err := applyPatch(p, patch); err != nil {
		return err
	}
	p.Updated = a.now().Format("2006-01-02")
	if err := reg.save(c.registry); err != nil {
		return err
	}
	for _, w := range lengthWarnings(p) {
		fmt.Fprintf(a.stderr, "project-board: warning: %s: %s\n", id, w)
	}
	verb := "updated"
	if created {
		verb = "added"
		changed = append([]string{"status " + p.Status}, changed...)
	}
	fmt.Fprintf(a.stdout, "%s %s: %s\n", verb, id, strings.Join(slices.Compact(changed), ", "))
	return nil
}

// editLoops drops loops (by 1-based number, exact text or a unique piece of
// text, all matched against the list before any change) and appends new ones.
func editLoops(loops []string, drops, adds []string) ([]string, error) {
	remove := map[int]bool{}
	for _, d := range drops {
		if n, err := strconv.Atoi(d); err == nil {
			if n < 1 || n > len(loops) {
				return nil, fmt.Errorf("--drop-loop %d: there are %d open loops", n, len(loops))
			}
			remove[n-1] = true
			continue
		}
		i := slices.Index(loops, d)
		if i < 0 {
			var hits []int
			for j, l := range loops {
				if strings.Contains(strings.ToLower(l), strings.ToLower(d)) {
					hits = append(hits, j)
				}
			}
			switch len(hits) {
			case 0:
				return nil, fmt.Errorf("--drop-loop %q matches no open loop", d)
			case 1:
				i = hits[0]
			default:
				return nil, fmt.Errorf("--drop-loop %q matches %d open loops; use its number", d, len(hits))
			}
		}
		remove[i] = true
	}
	var out []string
	for i, l := range loops {
		if !remove[i] {
			out = append(out, l)
		}
	}
	return append(out, adds...), nil
}

func (a *app) scan(args []string) error {
	fs := a.flags("scan")
	asJSON := fs.Bool("json", false, "print the /api/board payload")
	if err := noArgs(fs, args); err != nil {
		return err
	}
	start := time.Now()
	b, err := a.config().buildBoard()
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(b)
	}
	for _, cd := range b.Projects {
		st := cd.Status
		if cd.Untriaged {
			st = "-"
		}
		g := ""
		if cd.Facts.Git != nil {
			g = fmt.Sprintf("git %s %d±", cd.Facts.Git.Branch, cd.Facts.Git.Dirty)
		}
		fmt.Fprintf(a.stdout, "%-36s %-8s %-6s %-24s %s\n", cd.ID, st, idle(cd.Facts.DaysIdle), g, strings.Join(cd.Warnings, ","))
	}
	fmt.Fprintf(a.stderr, "scanned %d projects in %.1fs\n", len(b.Projects), time.Since(start).Seconds())
	return nil
}

func (a *app) html(args []string) error {
	fs := a.flags("html")
	out := fs.String("o", "", "output file (default <root>/board.html)")
	if err := noArgs(fs, args); err != nil {
		return err
	}
	c := a.config()
	b, err := c.buildBoard()
	if err != nil {
		return err
	}
	page, err := snapshot(boardHTML, b)
	if err != nil {
		return err
	}
	path := filepath.Join(c.root, "board.html")
	if *out != "" {
		path = a.abs(*out)
	}
	if err := writeAtomic(path, page); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s (%d projects)\n", path, len(b.Projects))
	return nil
}

// snapshot inlines the board's JSON into the page's placeholder. json.Marshal
// escapes <, > and & (as \u003c and so on), so no string in the data can close
// the script element.
func snapshot(tmpl []byte, b *board) ([]byte, error) {
	if n := strings.Count(string(tmpl), placeholder); n != 1 {
		return nil, fmt.Errorf("board.html has %d copies of the snapshot placeholder %s, want 1", n, placeholder)
	}
	data, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	filled := strings.Replace(placeholder, ">null<", ">"+string(data)+"<", 1)
	return []byte(strings.Replace(string(tmpl), placeholder, filled, 1)), nil
}

func (a *app) validate(args []string) error {
	fs := a.flags("validate")
	if err := noArgs(fs, args); err != nil {
		return err
	}
	c := a.config()
	if _, err := os.Stat(c.registry); err != nil {
		return fmt.Errorf("no registry at %s", c.registry)
	}
	reg, err := loadRegistry(c.registry)
	if err != nil {
		return err
	}
	ids, _, err := folders(c.root)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, id := range ids {
		present[id] = true
	}
	errs, warns := validateRegistry(reg, present)
	for _, e := range errs {
		fmt.Fprintln(a.stdout, "error:", e)
	}
	for _, w := range warns {
		fmt.Fprintln(a.stdout, "warning:", w)
	}
	var untriaged []string
	for _, id := range ids {
		if reg.find(id) == nil {
			untriaged = append(untriaged, id)
		}
	}
	if len(untriaged) > 0 {
		fmt.Fprintf(a.stdout, "untriaged: %d folders have no entry: %s\n", len(untriaged), strings.Join(untriaged, ", "))
	}
	fmt.Fprintf(a.stdout, "%s: %d projects, %d errors, %d warnings\n", c.registry, len(reg.Projects), len(errs), len(warns))
	if len(errs) > 0 {
		return fmt.Errorf("%d errors in %s", len(errs), c.registry)
	}
	return nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	}
	return nil
}
