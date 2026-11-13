// Command codebase-baseline measures an unfamiliar codebase without an LLM, so
// the numbers can be taken again later and compared. It backs the
// codebase-baseline skill: export a clean copy of the repo, discover and run its
// feedback loops (tests, lint, typecheck, build) in an empty environment,
// snapshot its size, git history, dependencies and hygiene, back-fill the same
// numbers for past months, build a hotspot treemap, diff two snapshots, build
// the HTML report from a folder of source files, and check the generated report
// for undefined jargon and leaked session talk.
//
// Everything except run-loops is static: it never executes the analysed repo's
// code, and none of it writes inside the repo it analyses. Every JSON output
// carries schema_version and the tool version, so a diff can flag measurements
// taken by different builds.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"
)

// version changes whenever a measurement could come out differently.
const version = "0.1.0"

// schemaVersion is the shape of every JSON file this tool writes.
const schemaVersion = 1

const usage = `codebase-baseline: deterministic measurements for onboarding to a codebase.

  codebase-baseline export <repo> <dest> [--strip-docs]
        git archive HEAD into dest (tracked files only). --strip-docs also deletes
        prose docs and agent-instruction files and writes <dest>.removed.txt.
  codebase-baseline discover <repo> --out loops.json
        find candidate feedback loops (install, typecheck, lint, test, build, ...)
        without running them, plus the run: steps of the CI configs.
  codebase-baseline run-loops <loops.json> --workdir <export-dir> --out results.json
        [--repeat-tests 2] [--only id,id]
        run the enabled loops in an empty environment, with timeouts. With --only,
        the selected entries are merged into an existing --out file.
  codebase-baseline measure <repo> --out snapshot.json [--files-out files.json]
        [--loop-results results.json] [--metrics run.json] [--months 24] [--as-of YYYY-MM-DD] [--worktree]
        size, git, dependencies and hygiene of HEAD.
  codebase-baseline history <repo> --months 12 --out backfill.json [--step 1]
        the cheap metrics at the last commit of each month.
  codebase-baseline hotspots --files files.json --repo <repo> --days 180 --out treemap.json [--max-leaves 600]
        size joined with git churn, as a hierarchy plus top-20 lists.
  codebase-baseline scoreboard --run run.json --snapshots <dir> --out scoreboard.json
        the report's scoreboard rows, from every snapshot in a directory.
  codebase-baseline scrub <dir> [--report <html>] [--format text|json]
        scan a workspace and the report for secret-looking values; exit 1 lists file:line, values masked.
  codebase-baseline diff <old.json> <new.json> [--format md|json] [--author <regex>]
        [--repo <path>]
        deltas between two snapshots.
  codebase-baseline build-report --src <ws>/report-src --out <ws>/report/index.html
        [--template <file>] [--draft]
        build the report page from a folder of source files (one per section and
        data block); --init --src <dir> writes a starter folder from the template.
  codebase-baseline check-report <report.html> [--repo <repo>] [--allow <file>]
        [--format text|json] [--strict-each]
        cold-reader checks on the generated report; exit 1 when any fail.

Every subcommand takes --help.
`

type app struct {
	stdout, stderr io.Writer
	now            func() time.Time
}

// errFailed is returned when a command already printed its findings and only
// the exit code is left to set.
var errFailed = errors.New("checks failed")

func main() {
	a := &app{stdout: os.Stdout, stderr: os.Stderr, now: time.Now}
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "export":
		err = a.cmdExport(args[1:])
	case "discover":
		err = a.cmdDiscover(args[1:])
	case "run-loops":
		err = a.cmdRunLoops(args[1:])
	case "measure":
		err = a.cmdMeasure(args[1:])
	case "history":
		err = a.cmdHistory(args[1:])
	case "hotspots":
		err = a.cmdHotspots(args[1:])
	case "scoreboard":
		err = a.cmdScoreboard(args[1:])
	case "scrub":
		err = a.cmdScrub(args[1:])
	case "diff":
		err = a.cmdDiff(args[1:])
	case "build-report":
		err = a.cmdBuildReport(args[1:])
	case "check-report":
		err = a.cmdCheckReport(args[1:])
	case "version", "--version":
		fmt.Fprintln(a.stdout, version, vcsRevision())
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, usage)
	default:
		err = fmt.Errorf("unknown command %q (see codebase-baseline --help)", args[0])
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errFailed):
		return 1
	}
	fmt.Fprintln(a.stderr, "codebase-baseline:", err)
	return 1
}

// toolInfo is stamped into every output file.
type toolInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	VCSRevision string `json:"vcs_revision,omitempty"`
	VCSModified bool   `json:"vcs_modified,omitempty"`
}

func currentTool() toolInfo {
	t := toolInfo{Name: "codebase-baseline", Version: version}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				t.VCSRevision = s.Value
			case "vcs.modified":
				t.VCSModified = s.Value == "true"
			}
		}
	}
	return t
}

func vcsRevision() string { return currentTool().VCSRevision }
