package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type metaSection struct {
	Path       string `json:"path"`
	Head       string `json:"head_sha"`
	HeadDate   string `json:"head_commit_date,omitempty"`
	Branch     string `json:"branch"`
	Describe   string `json:"git_describe,omitempty"`
	MeasuredAt string `json:"measured_at"`
	AsOf       string `json:"as_of"`
	Subdir     string `json:"subdir,omitempty"`
	Tree       string `json:"tree"`
	Dirty      bool   `json:"worktree_dirty"`
	Months     int    `json:"months_window"`
}

type snapshot struct {
	SchemaVersion int            `json:"schema_version"`
	Tool          toolInfo       `json:"tool"`
	Meta          metaSection    `json:"meta"`
	Size          sizeSection    `json:"size"`
	Git           gitSection     `json:"git"`
	Deps          depsSection    `json:"deps"`
	Hygiene       hygieneSection `json:"hygiene"`
	Docs          docsSection    `json:"docs"`
	Shallow       bool           `json:"shallow"`
	Branch        string         `json:"branch"`
	LoopsNotRun   *loopsNotRun   `json:"loops_not_run,omitempty"`
	Truncated     []string       `json:"truncated,omitempty"`
	Custom        []customResult `json:"custom,omitempty"`
	Loops         any            `json:"loops,omitempty"`
}

type loopsNotRun struct {
	Count   int            `json:"count"`
	Reasons []notRunReason `json:"reasons"`
}

type notRunReason struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// notRunOf lists the loops of a results file that did not produce a result.
func notRunOf(lr any) *loopsNotRun {
	root, ok := lr.(map[string]any)
	if !ok {
		return nil
	}
	list, _ := root["loops"].([]any)
	n := &loopsNotRun{Reasons: []notRunReason{}}
	for _, e := range list {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		switch st := str(em["status"]); st {
		case "disabled", "skipped", "not_selected", "tool-missing":
			n.Reasons = append(n.Reasons, notRunReason{ID: str(em["id"]), Status: st, Reason: str(em["reason"])})
		}
	}
	n.Count = len(n.Reasons)
	return n
}

type filesOut struct {
	SchemaVersion int       `json:"schema_version"`
	Tool          toolInfo  `json:"tool"`
	Root          string    `json:"root"`
	Head          string    `json:"head_sha"`
	Files         []sccFile `json:"files"`
}

func (a *app) cmdMeasure(args []string) error {
	fl := flag.NewFlagSet("measure", flag.ContinueOnError)
	out := fl.String("out", "", "snapshot JSON to write (required)")
	filesOutPath := fl.String("files-out", "", "write scc's per-file counts here (input of hotspots)")
	loopResults := fl.String("loop-results", "", "results.json from run-loops, embedded under \"loops\"")
	months := fl.Int("months", 24, "months of per-month commit counts")
	asOfFlag := fl.String("as-of", "", "measure history up to this date, YYYY-MM-DD (default: now)")
	metricsPath := fl.String("metrics", "", "run.json whose metrics array defines extra line-count metrics to take, embedded under custom")
	revFlag := fl.String("rev", "", "measure this commit instead of HEAD (via a temp export), e.g. to recompute an old snapshot with a newer tool")
	worktree := fl.Bool("worktree", false, "scan the working tree instead of a clean export of HEAD")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline measure <repo> --out snapshot.json [flags]")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *out == "" {
		fl.Usage()
		return fmt.Errorf("measure needs <repo> and --out")
	}
	repo := pos[0]
	if !isDir(repo) {
		return fmt.Errorf("%s is not a directory", repo)
	}
	if err := refuseInside(*out, repo); err != nil {
		return err
	}
	if err := refuseInside(*filesOutPath, repo); err != nil {
		return err
	}
	if *revFlag != "" && *worktree {
		return fmt.Errorf("--rev and --worktree cannot be combined")
	}
	now := a.now()
	asOf := now
	if *asOfFlag != "" {
		t, err := time.Parse("2006-01-02", *asOfFlag)
		if err != nil {
			return fmt.Errorf("--as-of: %w", err)
		}
		asOf = t.Add(24*time.Hour - time.Second)
	}
	snap := snapshot{SchemaVersion: schemaVersion, Tool: currentTool()}
	snap.Meta = metaSection{Path: absPath(repo), MeasuredAt: now.UTC().Format(time.RFC3339), AsOf: asOf.UTC().Format("2006-01-02"), Months: *months}

	isGit := hasGit(repo)
	root := repo
	var tracked map[string]bool
	respectIgnore := true
	rev := "HEAD"
	var exp exportResult
	if isGit {
		if *revFlag != "" {
			sha, err := git(repo, "rev-parse", "--verify", *revFlag+"^{commit}")
			if err != nil {
				return fmt.Errorf("--rev %s: %w", *revFlag, err)
			}
			rev = strings.TrimSpace(sha)
		}
		head, err := git(repo, "rev-parse", rev)
		if err != nil {
			return fmt.Errorf("%s has no commits yet: %w", repo, err)
		}
		snap.Meta.Head = strings.TrimSpace(head)
		if *revFlag != "" {
			snap.Meta.Branch = "(rev " + *revFlag + ")"
		} else if b, err := git(repo, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
			snap.Meta.Branch = strings.TrimSpace(b)
		} else {
			snap.Meta.Branch = "(detached)"
		}
		if d, err := git(repo, "log", "-1", "--format=%cI", rev); err == nil {
			snap.Meta.HeadDate = strings.TrimSpace(d)
			if *revFlag != "" && *asOfFlag == "" {
				if t, err := time.Parse(time.RFC3339, snap.Meta.HeadDate); err == nil {
					asOf = t
					snap.Meta.AsOf = asOf.UTC().Format("2006-01-02")
				}
			}
		}
		if out, err := git(repo, "describe", "--tags", "--always", rev); err == nil {
			snap.Meta.Describe = strings.TrimSpace(out)
		}
		snap.Meta.Subdir = strings.TrimSuffix(gitPrefix(repo), "/")
		if *revFlag == "" {
			if st, err := git(repo, "status", "--porcelain", "--untracked-files=no", "--", "."); err == nil {
				snap.Meta.Dirty = strings.TrimSpace(st) != ""
			}
		}
		if out, err := git(repo, "rev-parse", "--is-shallow-repository"); err == nil && strings.TrimSpace(out) == "true" {
			snap.Shallow = true
		}
		snap.Meta.Tree = "head-export"
		if *revFlag != "" {
			snap.Meta.Tree = "rev-export"
		}
		if *worktree {
			snap.Meta.Tree = "worktree"
		} else {
			tmp, err := os.MkdirTemp("", "cb-measure-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			if exp, err = exportTree(repo, rev, tmp); err != nil {
				return err
			}
			root = tmp
			respectIgnore = false
		}
		if *worktree {
			tracked = map[string]bool{}
			list, _ := gitTrackedFiles(repo)
			for _, f := range list {
				tracked[f] = true
			}
		}
	} else {
		snap.Meta.Tree = "directory (not a git checkout)"
	}
	snap.Branch = snap.Meta.Branch

	var files []string
	if *worktree && isGit {
		files, _ = gitTrackedFiles(repo)
	} else {
		files = walkFiles(root)
	}
	if isGit && !*worktree {
		tracked = map[string]bool{}
		for _, f := range files {
			tracked[f] = true
		}
	}

	scc, sccErr := runScc(root, respectIgnore)
	if sccErr != nil {
		snap.Size = sizeSection{Unavailable: sccErr.Error()}
	} else {
		snap.Size = summarizeSize(scc)
	}
	var defs []metricDef
	if *metricsPath != "" {
		rf, err := readRunFile(*metricsPath)
		if err != nil {
			return err
		}
		defs = rf.Metrics
	}
	snap.Git = collectGit(repo, rev, *months, asOf, tracked)
	snap.Deps = scanDeps(root, files)
	snap.Hygiene = scanHygiene(root, files, scc)
	for _, f := range exp.EnvFiles { // env files are not exported, but their presence is a finding (names only)
		snap.Hygiene.TrackedEnvFiles = append(snap.Hygiene.TrackedEnvFiles, f)
	}
	for _, s := range exp.Secrets {
		if s.Reason != "env file" {
			snap.Hygiene.TrackedSecretLike = append(snap.Hygiene.TrackedSecretLike, s.Path+" ("+s.Reason+")")
		}
	}
	if len(exp.Symlinks) > 0 {
		snap.Hygiene.Symlinks = exp.Symlinks
	}
	sort.Strings(snap.Hygiene.TrackedEnvFiles)
	snap.Hygiene.SecretScan = runSecretScan(repo)
	fromExport := !(*worktree && isGit)
	read := readerFor(root, repo, fromExport)
	snap.Docs = scanDocs(files, read, gitLastTouched(repo, rev))
	if len(defs) > 0 {
		snap.Custom = runCustomMetrics(defs, files, read)
	}

	if *loopResults != "" {
		var lr any
		if err := readJSON(*loopResults, &lr); err != nil {
			return err
		}
		snap.Loops = lr
		snap.LoopsNotRun = notRunOf(lr)
		if root, ok := lr.(map[string]any); ok {
			if tr, ok := root["truncated"].([]any); ok {
				for _, t := range tr {
					snap.Truncated = append(snap.Truncated, str(t))
				}
			}
		}
	}
	if *filesOutPath != "" && sccErr == nil {
		if err := writeJSON(*filesOutPath, filesOut{SchemaVersion: schemaVersion, Tool: currentTool(), Root: snap.Meta.Path, Head: snap.Meta.Head, Files: scc}); err != nil {
			return err
		}
	}
	if err := writeJSON(*out, snap); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s (%s, %s)\n", *out, shortSha(snap.Meta.Head), snap.Meta.Tree)
	if snap.Size.Totals != nil {
		fmt.Fprintf(a.stdout, "  size: %d files, %d code lines, %d languages\n", snap.Size.Totals.Files, snap.Size.Totals.Code, len(snap.Size.ByLanguage))
	} else {
		fmt.Fprintf(a.stdout, "  size: unavailable (%s)\n", snap.Size.Unavailable)
	}
	if snap.Git.Unavailable == "" {
		fmt.Fprintf(a.stdout, "  git: %d commits, %d contributors in the last 12 months, %d test files, %d direct dependencies\n",
			snap.Git.CommitsTotal, snap.Git.Contributors12m, snap.Hygiene.Tests.Total, snap.Deps.Total())
	} else {
		fmt.Fprintf(a.stdout, "  git: unavailable (%s)\n", snap.Git.Unavailable)
	}
	return nil
}

func shortSha(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}
