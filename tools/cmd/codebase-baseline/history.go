package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

type historyMetrics struct {
	CodeLines     int64            `json:"code_lines"`
	CodeLinesCode int64            `json:"code_lines_excl_docs_data"`
	CommentLines  int64            `json:"comment_lines"`
	Files         int64            `json:"files"`
	Complexity    int64            `json:"complexity"`
	TestFiles     int              `json:"test_files"`
	DirectDeps    int              `json:"direct_dependencies"`
	Todos         int              `json:"todo_markers"`
	CodeByLang    map[string]int64 `json:"code_by_language"`
}

type historyPoint struct {
	Month      string          `json:"month"`
	Sha        string          `json:"sha"`
	CommitDate string          `json:"commit_date"`
	Carried    bool            `json:"carried_from_earlier_month,omitempty"`
	Metrics    *historyMetrics `json:"metrics,omitempty"`
	Error      string          `json:"unavailable,omitempty"`
}

type seriesPoint struct {
	Month string `json:"month"`
	Value int64  `json:"value"`
}

type historyOut struct {
	SchemaVersion int                      `json:"schema_version"`
	Tool          toolInfo                 `json:"tool"`
	Repo          string                   `json:"repo"`
	Head          string                   `json:"head_sha"`
	GeneratedAt   string                   `json:"generated_at"`
	Months        int                      `json:"months"`
	Step          int                      `json:"step"`
	Note          string                   `json:"note"`
	Points        []historyPoint           `json:"points"`
	Series        map[string][]seriesPoint `json:"series"`
	SeriesByLang  map[string][]seriesPoint `json:"series_code_by_language"`
}

func (a *app) cmdHistory(args []string) error {
	fl := flag.NewFlagSet("history", flag.ContinueOnError)
	months := fl.Int("months", 12, "how many months back, ending with the current one")
	step := fl.Int("step", 1, "take every Nth month")
	out := fl.String("out", "", "backfill.json to write (required)")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline history <repo> --months 12 --out backfill.json [--step 1]")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *out == "" {
		fl.Usage()
		return fmt.Errorf("history needs <repo> and --out")
	}
	repo := pos[0]
	if !hasGit(repo) {
		return fmt.Errorf("%s is not a git checkout", repo)
	}
	if err := refuseInside(*out, repo); err != nil {
		return err
	}
	if *step < 1 {
		*step = 1
	}
	head, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	log, err := git(repo, scoped(repo, "log", "--first-parent", "--format=%H%x1f%cI")...)
	if err != nil {
		return err
	}
	type cm struct{ sha, date string }
	lastOfMonth := map[string]cm{}
	var newestFirst []cm
	for _, ln := range strings.Split(strings.TrimSpace(log), "\n") {
		p := strings.Split(ln, "\x1f")
		if len(p) != 2 {
			continue
		}
		c := cm{p[0], p[1]}
		newestFirst = append(newestFirst, c)
		if _, ok := lastOfMonth[c.date[:7]]; !ok {
			lastOfMonth[c.date[:7]] = c
		}
	}
	all := monthsBack(a.now(), *months)
	var picked []string
	for i := len(all) - 1; i >= 0; i -= *step {
		picked = append([]string{all[i]}, picked...)
	}
	// A month with no commit carries the previous state forward.
	stateAt := func(month string) (cm, bool, bool) {
		if c, ok := lastOfMonth[month]; ok {
			return c, false, true
		}
		for _, c := range newestFirst { // newest first: the first one before this month
			if c.date[:7] < month {
				return c, true, true
			}
		}
		return cm{}, false, false
	}
	res := historyOut{SchemaVersion: schemaVersion, Tool: currentTool(), Repo: absPath(repo), Head: strings.TrimSpace(head), GeneratedAt: a.now().UTC().Format(time.RFC3339),
		Months: *months, Step: *step, Series: map[string][]seriesPoint{}, SeriesByLang: map[string][]seriesPoint{},
		Note: "each point is the last commit of the month on HEAD's first-parent history, exported with git archive; nothing is executed"}
	cache := map[string]*historyMetrics{}
	for _, month := range picked {
		c, carried, ok := stateAt(month)
		if !ok {
			continue
		}
		pt := historyPoint{Month: month, Sha: c.sha, CommitDate: c.date, Carried: carried}
		if m, hit := cache[c.sha]; hit {
			pt.Metrics = m
		} else {
			fmt.Fprintf(a.stderr, "history: %s at %s\n", month, shortSha(c.sha))
			m, err := measureRev(repo, c.sha)
			if err != nil {
				pt.Error = err.Error()
			} else {
				cache[c.sha] = m
				pt.Metrics = m
			}
		}
		res.Points = append(res.Points, pt)
		if pt.Metrics == nil {
			continue
		}
		m := pt.Metrics
		add := func(name string, v int64) { res.Series[name] = append(res.Series[name], seriesPoint{month, v}) }
		add("code_lines", m.CodeLines)
		add("code_lines_excl_docs_data", m.CodeLinesCode)
		add("comment_lines", m.CommentLines)
		add("files", m.Files)
		add("complexity", m.Complexity)
		add("test_files", int64(m.TestFiles))
		add("direct_dependencies", int64(m.DirectDeps))
		add("todo_markers", int64(m.Todos))
		for lang, n := range m.CodeByLang {
			res.SeriesByLang[lang] = append(res.SeriesByLang[lang], seriesPoint{month, n})
		}
	}
	if err := writeJSON(*out, res); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s: %d points\n", *out, len(res.Points))
	for _, p := range res.Points {
		if p.Metrics != nil {
			fmt.Fprintf(a.stdout, "  %s %s  code=%d tests=%d deps=%d todos=%d\n", p.Month, shortSha(p.Sha), p.Metrics.CodeLines, p.Metrics.TestFiles, p.Metrics.DirectDeps, p.Metrics.Todos)
		} else {
			fmt.Fprintf(a.stdout, "  %s %s  unavailable: %s\n", p.Month, shortSha(p.Sha), p.Error)
		}
	}
	return nil
}

// measureRev exports one commit to a temp dir, measures it and removes it.
func measureRev(repo, sha string) (*historyMetrics, error) {
	tmp, err := os.MkdirTemp("", "cb-history-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if _, err := exportTree(repo, sha, tmp); err != nil {
		return nil, err
	}
	scc, err := runScc(tmp, false)
	if err != nil {
		return nil, err
	}
	size := summarizeSize(scc)
	files := walkFiles(tmp)
	m := &historyMetrics{
		CodeLines: size.Totals.Code, CodeLinesCode: size.TotalsCodeOnly.Code, CommentLines: size.Totals.Comment,
		Files: size.Totals.Files, Complexity: size.Totals.Complexity, CodeByLang: map[string]int64{},
		TestFiles: countTestFiles(files).Total, DirectDeps: scanDeps(tmp, files).DirectTotal, Todos: countTodos(tmp, scc).Total,
	}
	for lang, l := range size.ByLanguage {
		m.CodeByLang[lang] = l.Code
	}
	return m, nil
}
