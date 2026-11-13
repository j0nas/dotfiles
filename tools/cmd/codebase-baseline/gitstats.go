package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type gitCommit struct {
	Sha     string
	Author  string
	Email   string
	Date    string // RFC 3339, author date
	Parents int
	Subject string
	Body    string
}

func (c gitCommit) month() string {
	if len(c.Date) >= 7 {
		return c.Date[:7]
	}
	return ""
}

// agentEmails recognise the addresses coding agents commit and co-author with.
// A display name alone never counts: anyone can be called "Claude".
var agentEmails = []struct {
	Name string
	Re   *regexp.Regexp
}{
	{"claude", regexp.MustCompile(`(?i)@anthropic\.com$|^claude(\[bot\])?@|claude[^@]*\[bot\]@users\.noreply\.github\.com$`)},
	{"copilot", regexp.MustCompile(`(?i)copilot[^@]*@(users\.noreply\.)?github\.com$`)},
	{"cursor", regexp.MustCompile(`(?i)^cursoragent@|@cursor\.(com|sh)$`)},
	{"devin", regexp.MustCompile(`(?i)devin-ai-integration[^@]*@`)},
	{"codex_openai", regexp.MustCompile(`(?i)@openai\.com$|^codex@|codex[^@]*@users\.noreply\.github\.com$`)},
	{"gemini", regexp.MustCompile(`(?i)gemini[^@]*\[bot\]@users\.noreply\.github\.com$|gemini-code-assist`)},
	{"aider", regexp.MustCompile(`(?i)@aider\.chat$|^aider@`)},
}

// agentSessionTrailers are trailers that coding agents add to a commit to link
// it to their session. A commit carrying one is agent-made. Add an agent here
// (name, then a regexp for the whole trailer line, case-insensitive) and it is
// counted everywhere.
var agentSessionTrailers = []struct {
	Name string
	Re   *regexp.Regexp
}{
	{"claude", regexp.MustCompile(`(?im)^\s*claude-session:\s*\S+`)},
}

var (
	reCoAuthor    = regexp.MustCompile(`(?im)^\s*co-authored-by:\s*(.+)$`)
	reAngleEmail  = regexp.MustCompile(`<([^<>\s]+@[^<>\s]+)>`)
	reBotAuthor   = regexp.MustCompile(`(?i)\[bot\]|^dependabot|^renovate`)
	reClaudeFoot  = regexp.MustCompile(`(?i)generated with .{0,12}claude code`)
	reFixSubject  = regexp.MustCompile(`(?i)\b(fix(es|ed|ing)?|bug(fix)?|hotfix|regression)\b`)
	reRevertSubj  = regexp.MustCompile(`(?i)^revert\b`)
	reRevertBody  = regexp.MustCompile(`(?m)^This reverts commit [0-9a-f]{7,40}`)
	reAiderPrefix = regexp.MustCompile(`^aider:`)
)

func agentByEmail(email string) string {
	for _, p := range agentEmails {
		if p.Re.MatchString(email) {
			return p.Name
		}
	}
	return ""
}

// classifyCommit reports which agents a commit names by address (co-author
// trailer or author), by session trailer (Claude-Session: <url>), by the Claude
// Code footer or an aider: subject, and whether a bot authored it. The count is a lower
// bound: agents that leave no trailer are invisible.
func classifyCommit(c gitCommit) (agents []string, bot bool) {
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			agents = append(agents, name)
		}
	}
	for _, m := range reCoAuthor.FindAllStringSubmatch(c.Body, -1) {
		if e := reAngleEmail.FindStringSubmatch(m[1]); e != nil {
			if n := agentByEmail(e[1]); n != "" {
				add(n + ":co-author")
			} else if reBotAuthor.MatchString(e[1]) {
				add("bot:co-author")
			}
		}
	}
	if n := agentByEmail(c.Email); n != "" {
		add(n + ":author")
	}
	for _, t := range agentSessionTrailers {
		if t.Re.MatchString(c.Body) {
			add(t.Name + ":session-trailer")
		}
	}
	if reClaudeFoot.MatchString(c.Body) {
		add("claude:footer")
	}
	if reAiderPrefix.MatchString(c.Subject) {
		add("aider:subject")
	}
	// bot:co-author is a bot, not a coding agent: keep it out of the agent count.
	var real []string
	for _, a := range agents {
		if !strings.HasPrefix(a, "bot:") {
			real = append(real, a)
		}
	}
	return real, reBotAuthor.MatchString(c.Author) || reBotAuthor.MatchString(c.Email)
}

func parseGitLog(out string) []gitCommit {
	var commits []gitCommit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x1f")
		if len(f) < 7 {
			continue
		}
		parents := len(strings.Fields(f[4]))
		commits = append(commits, gitCommit{Sha: f[0], Author: f[1], Email: strings.ToLower(f[2]), Date: f[3], Parents: parents, Subject: f[5], Body: f[6]})
	}
	return commits
}

func gitLogAll(repo, rev string, until time.Time) ([]gitCommit, error) {
	out, err := git(repo, scoped(repo, "log", rev, "--until="+until.Format(time.RFC3339), "--format=%x1e%H%x1f%aN%x1f%aE%x1f%aI%x1f%P%x1f%s%x1f%B%x1f")...)
	if err != nil {
		return nil, err
	}
	return parseGitLog(out), nil
}

type fileChurn struct {
	Path    string `json:"path"`
	Commits int    `json:"commits"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

type numstatCommit struct {
	Sha   string
	Date  string
	Files []numstatFile
}

type numstatFile struct {
	Path           string
	Added, Deleted int
}

// gitNumstat lists the non-merge commits since a date with the files each touched.
func gitNumstat(repo, rev string, since, until time.Time) ([]numstatCommit, error) {
	out, err := git(repo, scoped(repo, "log", rev, "--no-merges", "--no-renames", "--numstat", "--relative",
		"--since="+since.Format(time.RFC3339), "--until="+until.Format(time.RFC3339), "--format=%x1e%H%x1f%aI%x1f")...)
	if err != nil {
		return nil, err
	}
	var res []numstatCommit
	for _, rec := range strings.Split(out, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x1f", 3)
		if len(parts) < 3 {
			continue
		}
		c := numstatCommit{Sha: strings.TrimSpace(parts[0]), Date: parts[1]}
		rest2 := parts[2]
		for _, line := range strings.Split(rest2, "\n") {
			p := strings.SplitN(line, "\t", 3)
			if len(p) != 3 {
				continue
			}
			a, _ := strconv.Atoi(p[0])
			d, _ := strconv.Atoi(p[1])
			c.Files = append(c.Files, numstatFile{Path: p[2], Added: a, Deleted: d})
		}
		res = append(res, c)
	}
	return res, nil
}

type authorShare struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

type gitSection struct {
	Unavailable          string         `json:"unavailable,omitempty"`
	Shallow              bool           `json:"shallow_clone,omitempty"`
	CommitsTotal         int            `json:"commits_total"`
	FirstCommit          string         `json:"first_commit_date,omitempty"`
	LastCommit           string         `json:"last_commit_date,omitempty"`
	CommitsByMonth       map[string]int `json:"commits_by_month"`
	ContributorsTotal    int            `json:"contributors_total"`
	Contributors12m      int            `json:"contributors_last_12m"`
	Commits12m           int            `json:"commits_last_12m"`
	NonMergeCommits12m   int            `json:"non_merge_commits_last_12m"`
	NonMergeByMonth      map[string]int `json:"non_merge_commits_by_month"`
	HumanCommits12m      int            `json:"human_commits_last_12m"`
	TopAuthorShare12m    float64        `json:"top_author_share_last_12m"`
	AuthorsFor50pct12m   int            `json:"authors_for_50pct_last_12m"`
	AuthorsFor80pct12m   int            `json:"authors_for_80pct_last_12m"`
	TopAuthors12m        []authorShare  `json:"top_authors_last_12m"`
	AuthorsNote          string         `json:"authors_note"`
	AgentCommitsByMonth  map[string]int `json:"agent_commits_by_month"`
	BotCommitsByMonth    map[string]int `json:"bot_authored_commits_by_month"`
	AgentCommitsTotal    int            `json:"agent_commits_total"`             // non-merge commits only
	AgentCommits12m      int            `json:"agent_commits_last_12m"`          // non-merge commits only
	AgentTrailerShare    float64        `json:"agent_trailer_share_lower_bound"` // agent_commits_last_12m / non_merge_commits_last_12m
	BotCommitsTotal      int            `json:"bot_authored_commits_total"`
	AgentPatternsMatched map[string]int `json:"agent_patterns_matched"`
	AgentNote            string         `json:"agent_note"`
	MergeCommits         int            `json:"merge_commits"`
	RevertCommits        int            `json:"revert_commits"`
	RevertCommits12m     int            `json:"revert_commits_last_12m"`
	FixShare             float64        `json:"fix_commit_share"`
	FixCommits           int            `json:"fix_commits"`
	MedianFilesChanged   float64        `json:"median_files_changed_per_commit_12m"`
	ChurnWindowDays      int            `json:"churn_window_days"`
	ChurnTop             []fileChurn    `json:"churn_top_files"`
}

// collectGit gathers the history metrics of the repo at HEAD as of a date.
func collectGit(repo, rev string, months int, asOf time.Time, tracked map[string]bool) gitSection {
	g := gitSection{AuthorsNote: "authors are counted by mailmap-resolved email; bot authors are left out of the shares", ChurnWindowDays: 180}
	if !hasGit(repo) {
		g.Unavailable = "not a git checkout"
		return g
	}
	if out, err := git(repo, "rev-parse", "--is-shallow-repository"); err == nil && strings.TrimSpace(out) == "true" {
		g.Shallow = true
	}
	commits, err := gitLogAll(repo, rev, asOf)
	if err != nil {
		g.Unavailable = err.Error()
		return g
	}
	g.CommitsTotal = len(commits)
	if len(commits) == 0 {
		g.Unavailable = "no commits"
		return g
	}
	g.LastCommit = commits[0].Date
	g.FirstCommit = commits[len(commits)-1].Date

	window := monthsBack(asOf, months)
	inWindow := map[string]bool{}
	g.CommitsByMonth, g.AgentCommitsByMonth, g.BotCommitsByMonth, g.NonMergeByMonth = map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, m := range window {
		inWindow[m] = true
		g.CommitsByMonth[m], g.AgentCommitsByMonth[m], g.BotCommitsByMonth[m], g.NonMergeByMonth[m] = 0, 0, 0, 0
	}
	cut12 := asOf.AddDate(-1, 0, 0)
	g.AgentPatternsMatched = map[string]int{}
	all := map[string]bool{}
	recent := map[string]bool{}
	byAuthor := map[string]int{}
	names := map[string]string{}
	fixes, nonMerge := 0, 0
	for _, c := range commits {
		agents, bot := classifyCommit(c)
		if !bot {
			all[c.Email] = true
		}
		t, terr := time.Parse(time.RFC3339, c.Date)
		is12 := terr == nil && t.After(cut12)
		if c.Parents > 1 {
			g.MergeCommits++
		} else {
			nonMerge++
			if reRevertSubj.MatchString(c.Subject) || reRevertBody.MatchString(c.Body) {
				g.RevertCommits++
				if is12 {
					g.RevertCommits12m++
				}
			} else if reFixSubject.MatchString(c.Subject) {
				fixes++
			}
		}
		isMerge := c.Parents > 1
		if is12 && !isMerge {
			g.NonMergeCommits12m++
		}
		if inWindow[c.month()] {
			g.CommitsByMonth[c.month()]++
			if !isMerge {
				g.NonMergeByMonth[c.month()]++
			}
			if len(agents) > 0 && !isMerge {
				g.AgentCommitsByMonth[c.month()]++
			}
			if bot {
				g.BotCommitsByMonth[c.month()]++
			}
		}
		if len(agents) > 0 && !isMerge {
			g.AgentCommitsTotal++
			if is12 {
				g.AgentCommits12m++
			}
			for _, a := range agents {
				g.AgentPatternsMatched[a]++
			}
		}
		if bot {
			g.BotCommitsTotal++
			g.AgentPatternsMatched["bot:author"]++
		}
		if is12 {
			g.Commits12m++
			if !bot {
				recent[c.Email] = true
				g.HumanCommits12m++
				byAuthor[c.Email]++
				if names[c.Email] == "" {
					names[c.Email] = c.Author
				}
			}
		}
	}
	g.ContributorsTotal = len(all)
	g.Contributors12m = len(recent)
	g.FixCommits = fixes
	if nonMerge > 0 {
		g.FixShare = round2(float64(fixes) / float64(nonMerge))
	}
	type kv struct {
		email string
		n     int
	}
	var ranked []kv
	for e, n := range byAuthor {
		ranked = append(ranked, kv{e, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].n != ranked[j].n {
			return ranked[i].n > ranked[j].n
		}
		return ranked[i].email < ranked[j].email
	})
	cum := 0
	for i, r := range ranked {
		cum += r.n
		if i < 10 {
			g.TopAuthors12m = append(g.TopAuthors12m, authorShare{Name: names[r.email], Commits: r.n})
		}
		if g.AuthorsFor50pct12m == 0 && float64(cum) >= 0.5*float64(g.HumanCommits12m) {
			g.AuthorsFor50pct12m = i + 1
		}
		if g.AuthorsFor80pct12m == 0 && float64(cum) >= 0.8*float64(g.HumanCommits12m) {
			g.AuthorsFor80pct12m = i + 1
		}
	}
	if len(ranked) > 0 && g.HumanCommits12m > 0 {
		g.TopAuthorShare12m = round2(float64(ranked[0].n) / float64(g.HumanCommits12m))
	}
	if g.NonMergeCommits12m > 0 {
		g.AgentTrailerShare = round2(float64(g.AgentCommits12m) / float64(g.NonMergeCommits12m))
	}
	g.AgentNote = "agent_trailer_share_lower_bound is the share of non-merge commits in the last 12 months (agent_commits_last_12m / non_merge_commits_last_12m; merge commits are left out of both) that carry a session trailer of a known coding agent (Claude-Session: <url>), a Co-authored-by trailer or author address of one (Claude/Anthropic, Copilot, Cursor, Devin, Codex/OpenAI, Gemini, Aider; display names alone do not count), the Claude Code footer or an aider: subject; agents that leave no trace are not counted. " +
		"Keys in agent_patterns_matched read <agent>:<where it matched>. bot_authored counts [bot] authors (and dependabot/renovate); a commit can be both."

	// Per-commit file counts and churn come from one numstat pass over 12 months.
	since := asOf.AddDate(-1, 0, 0)
	ns, err := gitNumstat(repo, rev, since, asOf)
	if err != nil {
		g.AgentNote += " (numstat unavailable: " + err.Error() + ")"
		return g
	}
	var counts []int
	churn := map[string]*fileChurn{}
	cut180 := asOf.AddDate(0, 0, -180)
	for _, c := range ns {
		counts = append(counts, len(c.Files))
		t, err := time.Parse(time.RFC3339, c.Date)
		if err != nil || !t.After(cut180) {
			continue
		}
		for _, f := range c.Files {
			if tracked != nil && !tracked[f.Path] {
				continue
			}
			fc := churn[f.Path]
			if fc == nil {
				fc = &fileChurn{Path: f.Path}
				churn[f.Path] = fc
			}
			fc.Commits++
			fc.Added += f.Added
			fc.Deleted += f.Deleted
		}
	}
	g.MedianFilesChanged = median(counts)
	for _, fc := range churn {
		g.ChurnTop = append(g.ChurnTop, *fc)
	}
	sort.Slice(g.ChurnTop, func(i, j int) bool {
		a, b := g.ChurnTop[i], g.ChurnTop[j]
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		if a.Added+a.Deleted != b.Added+b.Deleted {
			return a.Added+a.Deleted > b.Added+b.Deleted
		}
		return a.Path < b.Path
	})
	if len(g.ChurnTop) > 100 {
		g.ChurnTop = g.ChurnTop[:100]
	}
	return g
}

func describeRef(repo string) string {
	out, err := git(repo, "describe", "--tags", "--always")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func fmtPct(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }
