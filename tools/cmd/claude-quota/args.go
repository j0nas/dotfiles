package main

// A small imitation of the argparse parser the Python version used, so the command line,
// the help text and the usage errors (exit 2) stay the same.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const topUsage = `usage: claude-quota [-h]
                    {status,start,end,plan,learn,guard,watch,suspend,queue,wait,resume,reset,log}
                    ...
`

const topHelp = topUsage + `
claude-quota: read the account's Claude usage, log it, learn what agents cost,
plan spend.

positional arguments:
  {status,start,end,plan,learn,guard,watch,suspend,queue,wait,resume,reset,log}
    status              snapshot and show the windows (the default)
    start               log an agent starting; prints its id
    end                 log an agent finished, with the tokens it reported
    plan                price the work still to do against the budget left
    learn               show the fitted rates, sizes and outcomes
    guard               ok, hold or suspend: will the 5h window reach its
                        limit before it resets?
    watch               poll while agents run; exit when they should be
                        suspended, or none run
    suspend             log an agent paused (or stopped by the limit), to
                        resume later
    queue               work waiting for a later window; list it
    wait                sleep until the 5h window resets, then hand out the
                        work
    resume              hand out the suspended agents and queued work now
    reset               log a limit reset to spend before it expires; list
                        them
    log                 show the latest log entries

optional arguments:
  -h, --help            show this help message and exit
`

var commandNames = []string{"status", "start", "end", "plan", "learn", "guard", "watch", "suspend", "queue", "wait", "resume", "reset", "log"}

type posSpec struct {
	dest    string
	plus    bool // one or more (argparse nargs="+")
	choices []string
}

type optSpec struct {
	flags []string
	dest  string
	typ   string // str, float, int, bool, list, help
}

type cmdSpec struct {
	prog    string
	usage   string
	help    string
	pos     []posSpec
	opts    []optSpec
	subs    []string // subcommand names, for queue and reset
	subHelp string
}

var helpOpt = optSpec{flags: []string{"-h", "--help"}, dest: "help", typ: "help"}

func opt(dest, typ string, flags ...string) optSpec {
	return optSpec{flags: flags, dest: dest, typ: typ}
}

func spec(prog, usage, help string, pos []posSpec, opts ...optSpec) *cmdSpec {
	return &cmdSpec{prog: prog, usage: usage, help: usage + "\n" + help, pos: pos, opts: append([]optSpec{helpOpt}, opts...)}
}

func p1(dest string) posSpec { return posSpec{dest: dest} }

var specs = map[string]*cmdSpec{
	"status": spec("claude-quota status", "usage: claude-quota status [-h] [--target TARGET]\n", `optional arguments:
  -h, --help       show this help message and exit
  --target TARGET
`, nil, opt("target", "float", "--target")),
	"start": spec("claude-quota start", "usage: claude-quota start [-h] [--kind KIND] label model\n", `positional arguments:
  label
  model        one of haiku, sonnet, opus, fable

optional arguments:
  -h, --help   show this help message and exit
  --kind KIND  implement, review, research, design, mechanical…
`, []posSpec{p1("label"), p1("model")}, opt("kind", "str", "--kind")),
	"end": spec("claude-quota end", "usage: claude-quota end [-h] [--outcome OUTCOME] id tokens\n", `positional arguments:
  id
  tokens             e.g. 229k

optional arguments:
  -h, --help         show this help message and exit
  --outcome OUTCOME  one of merged, fixed, partial, discarded
`, []posSpec{p1("id"), p1("tokens")}, opt("outcome", "str", "--outcome")),
	"plan": spec("claude-quota plan", `usage: claude-quota plan [-h] [--size [SIZE ...]] [--target TARGET] [--serial]
                         agents [agents ...]
`, `positional arguments:
  agents             model[:kind]=count, e.g. sonnet:implement=3 opus=1

optional arguments:
  -h, --help         show this help message and exit
  --size [SIZE ...]  model=tokens per agent, overriding the learned size
  --target TARGET
  --serial           the agents run one after another
`, []posSpec{{dest: "agents", plus: true}}, opt("size", "list", "--size"), opt("target", "float", "--target"), opt("serial", "bool", "--serial")),
	"learn": spec("claude-quota learn", "usage: claude-quota learn [-h]\n", `optional arguments:
  -h, --help  show this help message and exit
`, nil),
	"guard": spec("claude-quota guard", "usage: claude-quota guard [-h] [--at AT] [--lead LEAD]\n", `optional arguments:
  -h, --help   show this help message and exit
  --at AT      % of the 5h window to suspend at
  --lead LEAD  minutes before the limit to suspend at
`, nil, opt("at", "float", "--at"), opt("lead", "float", "--lead")),
	"watch": spec("claude-quota watch", "usage: claude-quota watch [-h] [--at AT] [--lead LEAD] [--every EVERY]\n", `optional arguments:
  -h, --help     show this help message and exit
  --at AT        % of the 5h window to suspend at
  --lead LEAD    minutes before the limit to suspend at
  --every EVERY  seconds between readings
`, nil, opt("at", "float", "--at"), opt("lead", "float", "--lead"), opt("every", "float", "--every")),
	"suspend": spec("claude-quota suspend", "usage: claude-quota suspend [-h] [--ref REF] [--note NOTE] id\n", `positional arguments:
  id

optional arguments:
  -h, --help   show this help message and exit
  --ref REF    the session's handle on it, e.g. its agent id for SendMessage
  --note NOTE  where it was, for whoever resumes it
`, []posSpec{p1("id")}, opt("ref", "str", "--ref"), opt("note", "str", "--note")),
	"queue add": spec("claude-quota queue add", "usage: claude-quota queue add [-h] [--note NOTE] label spec\n", `positional arguments:
  label
  spec         model[:kind], e.g. opus:implement

optional arguments:
  -h, --help   show this help message and exit
  --note NOTE
`, []posSpec{p1("label"), p1("spec")}, opt("note", "str", "--note")),
	"queue drop": spec("claude-quota queue drop", "usage: claude-quota queue drop [-h] id\n", `positional arguments:
  id

optional arguments:
  -h, --help  show this help message and exit
`, []posSpec{p1("id")}),
	"queue list": spec("claude-quota queue list", "usage: claude-quota queue list [-h]\n", `optional arguments:
  -h, --help  show this help message and exit
`, nil),
	"wait": spec("claude-quota wait", "usage: claude-quota wait [-h] [--margin MARGIN]\n", `optional arguments:
  -h, --help       show this help message and exit
  --margin MARGIN  minutes past the reset
`, nil, opt("margin", "float", "--margin")),
	"resume": spec("claude-quota resume", "usage: claude-quota resume [-h] [--dry-run]\n", `optional arguments:
  -h, --help  show this help message and exit
  --dry-run   list them without marking them handed out
`, nil, opt("dry_run", "bool", "--dry-run")),
	"reset add": spec("claude-quota reset add", "usage: claude-quota reset add [-h] [--note NOTE] {week,5h,both} expires\n", `positional arguments:
  {week,5h,both}  what it refills
  expires         local date or date and time, e.g. 2026-10-22 or
                  2026-10-22T18:00

optional arguments:
  -h, --help      show this help message and exit
  --note NOTE
`, []posSpec{{dest: "window", choices: []string{"week", "5h", "both"}}, p1("expires")}, opt("note", "str", "--note")),
	"reset used": spec("claude-quota reset used", "usage: claude-quota reset used [-h] id\n", `positional arguments:
  id

optional arguments:
  -h, --help  show this help message and exit
`, []posSpec{p1("id")}),
	"reset list": spec("claude-quota reset list", "usage: claude-quota reset list [-h]\n", `optional arguments:
  -h, --help  show this help message and exit
`, nil),
	"log": spec("claude-quota log", "usage: claude-quota log [-h] [-n N]\n", `optional arguments:
  -h, --help  show this help message and exit
  -n N
`, nil, opt("n", "int", "-n")),
}

// Parents of the two commands that take a subcommand.
var groups = map[string]*cmdSpec{
	"queue": {prog: "claude-quota queue", usage: "usage: claude-quota queue [-h] {add,drop,list} ...\n", subs: []string{"add", "drop", "list"},
		help: `usage: claude-quota queue [-h] {add,drop,list} ...

positional arguments:
  {add,drop,list}
    add            queue an agent not started yet; prints its id
    drop           drop a queued item
    list           the queued work

optional arguments:
  -h, --help       show this help message and exit
`},
	"reset": {prog: "claude-quota reset", usage: "usage: claude-quota reset [-h] {add,used,list} ...\n", subs: []string{"add", "used", "list"},
		help: `usage: claude-quota reset [-h] {add,used,list} ...

positional arguments:
  {add,used,list}
    add            a reset from Settings > Usage; prints its id
    used           log a reset as redeemed (else read off the snapshots)
    list           every logged reset and what became of it

optional arguments:
  -h, --help       show this help message and exit
`},
}

type parsed struct {
	cmd  string
	vals map[string][]string
	set  map[string]bool
}

func (p *parsed) str(dest string) *string {
	if v, ok := p.vals[dest]; ok && len(v) > 0 {
		return &v[0]
	}
	return nil
}

func (p *parsed) float(dest string, def float64) float64 {
	if s := p.str(dest); s != nil {
		f, _ := pyFloat(*s)
		return f
	}
	return def
}

func (p *parsed) flag(dest string) bool { return p.set[dest] }

// parseError is a usage error: usage text and message on stderr, exit 2. A help request is
// the same with the help text on stdout and exit 0.
type parseError struct {
	out  string
	err  string
	code int
}

func (e *parseError) Error() string { return e.err + e.out }

func perr(sp *cmdSpec, format string, a ...any) *parseError {
	return &parseError{err: sp.usage + sp.prog + ": error: " + fmt.Sprintf(format, a...) + "\n", code: 2}
}

func topErr(format string, a ...any) *parseError {
	return &parseError{err: topUsage + "claude-quota: error: " + fmt.Sprintf(format, a...) + "\n", code: 2}
}

var negNumber = regexp.MustCompile(`^-\d+$|^-\d*\.\d+$`)

// classify says whether a token is an option of sp (flag and any value attached to it), and
// whether it looks like one at all (an unknown option).
func classify(sp *cmdSpec, t string) (o *optSpec, attached *string, optionLike bool, err *parseError) {
	if t == "" || t[0] != '-' {
		return nil, nil, false, nil
	}
	for i := range sp.opts {
		for _, f := range sp.opts[i].flags {
			if t == f {
				return &sp.opts[i], nil, true, nil
			}
		}
	}
	if len(t) == 1 {
		return nil, nil, false, nil
	}
	if k := strings.Index(t, "="); k > 0 {
		for i := range sp.opts {
			for _, f := range sp.opts[i].flags {
				if t[:k] == f {
					v := t[k+1:]
					return &sp.opts[i], &v, true, nil
				}
			}
		}
	}
	// abbreviations of long options, and -nVALUE
	var hits []string
	var hitOpt *optSpec
	var hitVal *string
	for i := range sp.opts {
		for _, f := range sp.opts[i].flags {
			switch {
			case strings.HasPrefix(t, "--"):
				name, val, hasVal := strings.Cut(t, "=")
				if strings.HasPrefix(f, name) && strings.HasPrefix(f, "--") {
					hits = append(hits, f)
					hitOpt = &sp.opts[i]
					hitVal = nil
					if hasVal {
						hitVal = &val
					}
				}
			case len(f) == 2 && strings.HasPrefix(t, f):
				v := t[2:]
				hits = append(hits, f)
				hitOpt = &sp.opts[i]
				hitVal = &v
			}
		}
	}
	if len(hits) > 1 {
		return nil, nil, true, perr(sp, "ambiguous option: %s could match %s", strings.SplitN(t, "=", 2)[0], strings.Join(hits, ", "))
	}
	if len(hits) == 1 {
		return hitOpt, hitVal, true, nil
	}
	if negNumber.MatchString(t) || strings.Contains(t, " ") {
		return nil, nil, false, nil
	}
	return nil, nil, true, nil
}

func checkType(sp *cmdSpec, o *optSpec, v string) *parseError {
	switch o.typ {
	case "float":
		if _, ok := pyFloat(v); !ok {
			return perr(sp, "argument %s: invalid float value: %s", strings.Join(o.flags, "/"), pyRepr(v))
		}
	case "int":
		if _, err := strconv.Atoi(strings.TrimSpace(v)); err != nil {
			return perr(sp, "argument %s: invalid int value: %s", strings.Join(o.flags, "/"), pyRepr(v))
		}
	}
	return nil
}

// parse reads toks against sp. Tokens it can't place come back as extras, which the top
// level reports as unrecognized arguments.
func parse(sp *cmdSpec, toks []string) (*parsed, []string, *parseError) {
	p := &parsed{cmd: sp.prog, vals: map[string][]string{}, set: map[string]bool{}}
	var extras []string
	next := 0 // positionals consumed
	afterDD := false
	for i := 0; i < len(toks); {
		t := toks[i]
		var o *optSpec
		var attached *string
		like := false
		if !afterDD {
			if t == "--" {
				afterDD = true
				i++
				continue
			}
			var err *parseError
			o, attached, like, err = classify(sp, t)
			if err != nil {
				return nil, nil, err
			}
		}
		if !like {
			j := i
			for j < len(toks) {
				if !afterDD && toks[j] == "--" {
					afterDD = true
					j++
					continue
				}
				if !afterDD {
					if _, _, l, err := classify(sp, toks[j]); err != nil {
						return nil, nil, err
					} else if l {
						break
					}
				}
				j++
			}
			run := toks[i:j]
			if afterDD {
				run = nil
				for _, x := range toks[i:j] {
					if x != "--" {
						run = append(run, x)
					}
				}
			}
			take := 0
			if next < len(sp.pos) {
				// as many of the remaining positionals as the run satisfies; a "+" one takes the rest
				left := sp.pos[next:]
				n := min(len(left), len(run))
				take = n
				for _, ps := range left[:n] {
					if ps.plus {
						take = len(run)
					}
				}
			}
			idx := 0
			for idx < take && next < len(sp.pos) {
				ps := sp.pos[next]
				cnt := 1
				if ps.plus {
					cnt = take - idx
				}
				vals := run[idx : idx+cnt]
				for _, v := range vals {
					if len(ps.choices) > 0 && !contains(ps.choices, v) {
						reprs := make([]string, len(ps.choices))
						for c, ch := range ps.choices {
							reprs[c] = pyRepr(ch)
						}
						return nil, nil, perr(sp, "argument %s: invalid choice: %s (choose from %s)", ps.dest, pyRepr(v), strings.Join(reprs, ", "))
					}
				}
				p.vals[ps.dest] = append([]string(nil), vals...)
				idx += cnt
				next++
			}
			extras = append(extras, run[take:]...)
			i = j
			continue
		}
		i++
		if o == nil {
			extras = append(extras, t)
			continue
		}
		switch o.typ {
		case "help":
			return nil, nil, &parseError{out: sp.help, code: 0}
		case "bool":
			p.set[o.dest] = true
		case "list":
			var vals []string
			if attached != nil {
				vals = append(vals, *attached)
			}
			for i < len(toks) {
				if _, _, l, err := classify(sp, toks[i]); err != nil {
					return nil, nil, err
				} else if l || toks[i] == "--" {
					break
				}
				vals = append(vals, toks[i])
				i++
			}
			p.vals[o.dest] = append(p.vals[o.dest], vals...)
			p.set[o.dest] = true
		default:
			var v string
			if attached != nil {
				v = *attached
			} else {
				if i >= len(toks) {
					return nil, nil, perr(sp, "argument %s: expected one argument", strings.Join(o.flags, "/"))
				}
				if _, _, l, err := classify(sp, toks[i]); err != nil {
					return nil, nil, err
				} else if l || toks[i] == "--" {
					return nil, nil, perr(sp, "argument %s: expected one argument", strings.Join(o.flags, "/"))
				}
				v = toks[i]
				i++
			}
			if err := checkType(sp, o, v); err != nil {
				return nil, nil, err
			}
			p.vals[o.dest] = []string{v}
			p.set[o.dest] = true
		}
	}
	if next < len(sp.pos) {
		var names []string
		for _, ps := range sp.pos[next:] {
			if len(ps.choices) > 0 {
				names = append(names, "{"+strings.Join(ps.choices, ",")+"}")
			} else {
				names = append(names, ps.dest)
			}
		}
		return nil, nil, perr(sp, "the following arguments are required: %s", strings.Join(names, ", "))
	}
	return p, extras, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// parseArgs resolves the command line to a command and its values.
func parseArgs(args []string) (*parsed, *parseError) {
	var extras []string
	rest := args
	for len(rest) > 0 {
		t := rest[0]
		if t == "-h" || (len(t) > 2 && strings.HasPrefix("--help", t)) {
			return nil, &parseError{out: topHelp}
		}
		if t == "" || t[0] != '-' || t == "-" || negNumber.MatchString(t) || strings.Contains(t, " ") {
			break
		}
		extras = append(extras, t)
		rest = rest[1:]
	}
	if len(rest) == 0 {
		if len(extras) > 0 {
			return nil, topErr("unrecognized arguments: %s", strings.Join(extras, " "))
		}
		return &parsed{cmd: "status", vals: map[string][]string{}, set: map[string]bool{}}, nil
	}
	name, tail := rest[0], rest[1:]
	if g, ok := groups[name]; ok {
		return parseGroup(g, name, tail, extras)
	}
	sp, ok := specs[name]
	if !ok || strings.Contains(name, " ") {
		return nil, topErr("invalid choice: %s (choose from %s)", pyRepr(name), reprList(commandNames))
	}
	p, ex, err := parse(sp, tail)
	if err != nil {
		return nil, err
	}
	return finish(p, name, append(extras, ex...))
}

func parseGroup(g *cmdSpec, name string, toks, extras []string) (*parsed, *parseError) {
	var sub string
	var tail []string
	for i, t := range toks {
		if t == "-h" || (len(t) > 2 && strings.HasPrefix("--help", t)) {
			return nil, &parseError{out: g.help}
		}
		if t == "" || t[0] != '-' || t == "-" || negNumber.MatchString(t) || strings.Contains(t, " ") {
			sub, tail = t, toks[i+1:]
			break
		}
		extras = append(extras, t)
	}
	if sub == "" {
		return finish(&parsed{vals: map[string][]string{}, set: map[string]bool{}}, name+" list", extras)
	}
	if !contains(g.subs, sub) {
		return nil, &parseError{err: g.usage + g.prog + ": error: invalid choice: " + pyRepr(sub) + " (choose from " + reprList(g.subs) + ")\n", code: 2}
	}
	p, ex, err := parse(specs[name+" "+sub], tail)
	if err != nil {
		return nil, err
	}
	return finish(p, name+" "+sub, append(extras, ex...))
}

func finish(p *parsed, cmd string, extras []string) (*parsed, *parseError) {
	if len(extras) > 0 {
		return nil, topErr("unrecognized arguments: %s", strings.Join(extras, " "))
	}
	p.cmd = cmd
	return p, nil
}

func reprList(l []string) string {
	r := make([]string, len(l))
	for i, s := range l {
		r[i] = pyRepr(s)
	}
	return strings.Join(r, ", ")
}

// pyRepr is Python's repr() of a string.
func pyRepr(s string) string {
	q := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		q = '"'
	}
	var b strings.Builder
	b.WriteByte(q)
	for _, r := range s {
		switch {
		case r == rune(q) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(q)
	return b.String()
}

// pyFloat is float(): surrounding whitespace is fine, "inf" and "nan" parse.
func pyFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(s, "_", "")), 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, true
		}
		return 0, false
	}
	return f, true
}
