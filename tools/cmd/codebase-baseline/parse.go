package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// parsed is what a tool's output says about itself: counts and which tool it was.
type parsed map[string]any

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

var (
	reCount       = regexp.MustCompile(`(\d+) (failed|skipped|todo|passed|total|pending|expected fail)`)
	reJestTests   = regexp.MustCompile(`(?m)^Tests:\s+(.*\btotal)\s*$`)
	reJestSuites  = regexp.MustCompile(`(?m)^Test Suites:\s+(.*)$`)
	reVitestTests = regexp.MustCompile(`(?m)^\s*Tests\s{2,}(.*)$`)
	reVitestFiles = regexp.MustCompile(`(?m)^\s*Test Files\s{2,}(.*)$`)
	reNodeTAP     = regexp.MustCompile(`(?m)^\s*(?:#|ℹ)\s+(tests|suites|pass|fail|cancelled|skipped|todo)\s+(\d+)\s*$`)
	reMochaN      = regexp.MustCompile(`(?m)^\s*(\d+) (passing|failing|pending)\b`)
	rePytestLine  = regexp.MustCompile(`(?m)^(?:=+ )?((?:\d+ [a-z]+(?: [a-z]+)?(?:, )?)+) in [\d.:]+s?(?: \([^)]*\))?(?: =+)?\s*$`)
	rePytestItem  = regexp.MustCompile(`(\d+) (passed|failed|skipped|xfailed|xpassed|errors?|warnings?|deselected|rerun)`)
	reGoPkg       = regexp.MustCompile(`(?m)^(ok|FAIL|\?)[ \t]+\S+`)
	reGoTopTest   = regexp.MustCompile(`(?m)^--- (PASS|FAIL|SKIP): `)
	reGoSubTest   = regexp.MustCompile(`(?m)^\s+--- (PASS|FAIL|SKIP): `)
	reCargoResult = regexp.MustCompile(`(?m)^test result: (?:ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored`)
	reRspec       = regexp.MustCompile(`(?m)(\d+) examples?, (\d+) failures?(?:, (\d+) pending)?`)
	rePhpOK       = regexp.MustCompile(`(?m)OK \((\d+) tests?, (\d+) assertions?\)`)
	rePhpFail     = regexp.MustCompile(`(?m)Tests: (\d+), Assertions: (\d+)((?:, [A-Za-z]+: \d+)*)`)
	reSurefire    = regexp.MustCompile(`(?m)^(?:\[(?:INFO|WARNING|ERROR)\] )?Tests run: (\d+), Failures: (\d+), Errors: (\d+), Skipped: (\d+)\s*$`)
	reGradleTests = regexp.MustCompile(`(?m)(\d+) tests? completed, (\d+) failed`)
	reDotnetTest  = regexp.MustCompile(`(?m)(?:Passed|Failed)!\s+-\s+Failed:\s+(\d+), Passed:\s+(\d+), Skipped:\s+(\d+), Total:\s+(\d+)`)
	reTscErr      = regexp.MustCompile(`(?m)error TS\d+:`)
	reTscFound    = regexp.MustCompile(`Found (\d+) errors?`)
	reEslintSum   = regexp.MustCompile(`✖ (\d+) problems? \((\d+) errors?, (\d+) warnings?\)`)
	reEslintLine  = regexp.MustCompile(`(?m)^\s+\d+:\d+\s+(error|warning)\s`)
	reNextLintLn  = regexp.MustCompile(`(?m)^\d+:\d+\s+(Warning|Error):`)
	reRuffFound   = regexp.MustCompile(`Found (\d+) errors?\.`)
	reFlake8      = regexp.MustCompile(`(?m)^[^\s:]+:\d+:\d+: [A-Z]\d+ `)
	reMypyFound   = regexp.MustCompile(`Found (\d+) errors? in (\d+) files?`)
	rePyright     = regexp.MustCompile(`(\d+) errors?, (\d+) warnings?`)
	reRubocop     = regexp.MustCompile(`(\d+) offenses? detected`)
	reGoIssue     = regexp.MustCompile(`(?m)^[^\s:]+\.go:\d+:\d+: `)
	reClippy      = regexp.MustCompile(`(?m)^(warning|error)(\[[A-Za-z0-9_]+\])?: `)
	reDotnetWarn  = regexp.MustCompile(`(?m)^\s*(\d+) Warning\(s\)`)
	reDotnetErr   = regexp.MustCompile(`(?m)^\s*(\d+) Error\(s\)`)
	rePhpstan     = regexp.MustCompile(`Found (\d+) errors?`)
)

func countsFromLine(line string) map[string]int {
	out := map[string]int{}
	for _, m := range reCount.FindAllStringSubmatch(line, -1) {
		out[m[2]] += atoi(m[1])
	}
	return out
}

func parseJest(t string) parsed {
	m := reJestTests.FindStringSubmatch(t)
	if m == nil {
		return nil
	}
	c := countsFromLine(m[1])
	p := parsed{"tool": "jest", "passed": c["passed"], "failed": c["failed"], "skipped": c["skipped"] + c["todo"], "total": c["total"]}
	if s := reJestSuites.FindStringSubmatch(t); s != nil {
		sc := countsFromLine(s[1])
		p["suites_total"], p["suites_failed"] = sc["total"], sc["failed"]
	}
	return p
}

func parseVitest(t string) parsed {
	ms := reVitestTests.FindAllStringSubmatch(t, -1)
	if len(ms) == 0 {
		return nil
	}
	m := ms[len(ms)-1]
	c := countsFromLine(m[1])
	total := c["passed"] + c["failed"] + c["skipped"] + c["todo"]
	if tm := regexp.MustCompile(`\((\d+)\)`).FindStringSubmatch(m[1]); tm != nil {
		total = atoi(tm[1])
	}
	p := parsed{"tool": "vitest", "passed": c["passed"], "failed": c["failed"], "skipped": c["skipped"] + c["todo"], "total": total}
	if fm := reVitestFiles.FindStringSubmatch(t); fm != nil {
		fc := countsFromLine(fm[1])
		p["suites_failed"] = fc["failed"]
	}
	return p
}

func parseNodeTAP(t string) parsed {
	vals := map[string]int{}
	found := false
	for _, m := range reNodeTAP.FindAllStringSubmatch(t, -1) {
		vals[m[1]] = atoi(m[2])
		found = true
	}
	if !found || (vals["tests"] == 0 && vals["pass"] == 0 && vals["fail"] == 0) {
		if !found {
			return nil
		}
	}
	return parsed{"tool": "node:test", "passed": vals["pass"], "failed": vals["fail"] + vals["cancelled"], "skipped": vals["skipped"] + vals["todo"], "total": vals["tests"]}
}

func parseMocha(t string) parsed {
	ms := reMochaN.FindAllStringSubmatch(t, -1)
	if len(ms) == 0 {
		return nil
	}
	v := map[string]int{}
	for _, m := range ms {
		v[m[2]] = atoi(m[1])
	}
	return parsed{"tool": "mocha", "passed": v["passing"], "failed": v["failing"], "skipped": v["pending"], "total": v["passing"] + v["failing"] + v["pending"]}
}

func parsePytest(t string) parsed {
	ms := rePytestLine.FindAllStringSubmatch(t, -1)
	if len(ms) == 0 {
		if strings.Contains(t, "no tests ran") {
			return parsed{"tool": "pytest", "passed": 0, "failed": 0, "skipped": 0, "total": 0}
		}
		return nil
	}
	v := map[string]int{}
	for _, m := range rePytestItem.FindAllStringSubmatch(ms[len(ms)-1][1], -1) {
		k := strings.TrimSuffix(m[2], "s")
		v[k] += atoi(m[1])
	}
	failed := v["failed"] + v["error"]
	skipped := v["skipped"] + v["xfailed"]
	return parsed{"tool": "pytest", "passed": v["passed"] + v["xpassed"], "failed": failed, "skipped": skipped, "total": v["passed"] + v["xpassed"] + failed + skipped}
}

func parseGoTest(t string) parsed {
	pk := reGoPkg.FindAllStringSubmatch(t, -1)
	if len(pk) == 0 {
		return nil
	}
	p := parsed{"tool": "go test"}
	ok, fail, none := 0, 0, 0
	for _, m := range pk {
		switch m[1] {
		case "ok":
			ok++
		case "FAIL":
			fail++
		default:
			none++
		}
	}
	p["packages_ok"], p["packages_failed"], p["packages_without_tests"] = ok, fail, none
	top := map[string]int{}
	for _, m := range reGoTopTest.FindAllStringSubmatch(t, -1) {
		top[m[1]]++
	}
	sub := map[string]int{}
	for _, m := range reGoSubTest.FindAllStringSubmatch(t, -1) {
		sub[m[1]]++
	}
	if top["PASS"]+top["FAIL"]+top["SKIP"] > 0 {
		p["passed"], p["failed"], p["skipped"] = top["PASS"], top["FAIL"], top["SKIP"]
		p["total"] = top["PASS"] + top["FAIL"] + top["SKIP"]
		p["subtests_total"] = sub["PASS"] + sub["FAIL"] + sub["SKIP"]
	} else {
		p["failed"] = fail
	}
	return p
}

func parseCargoTest(t string) parsed {
	ms := reCargoResult.FindAllStringSubmatch(t, -1)
	if len(ms) == 0 {
		return nil
	}
	pass, fail, ign := 0, 0, 0
	for _, m := range ms {
		pass += atoi(m[1])
		fail += atoi(m[2])
		ign += atoi(m[3])
	}
	return parsed{"tool": "cargo test", "passed": pass, "failed": fail, "skipped": ign, "total": pass + fail + ign}
}

func parseRspec(t string) parsed {
	ms := reRspec.FindAllStringSubmatch(t, -1)
	if len(ms) == 0 {
		return nil
	}
	m := ms[len(ms)-1]
	total, fail, pend := atoi(m[1]), atoi(m[2]), atoi(m[3])
	return parsed{"tool": "rspec", "passed": total - fail - pend, "failed": fail, "skipped": pend, "total": total}
}

func parsePhpunit(t string) parsed {
	if m := rePhpOK.FindStringSubmatch(t); m != nil {
		n := atoi(m[1])
		return parsed{"tool": "phpunit", "passed": n, "failed": 0, "skipped": 0, "total": n, "assertions": atoi(m[2])}
	}
	if m := rePhpFail.FindStringSubmatch(t); m != nil {
		v := map[string]int{}
		for _, kv := range regexp.MustCompile(`([A-Za-z]+): (\d+)`).FindAllStringSubmatch(m[3], -1) {
			v[kv[1]] = atoi(kv[2])
		}
		total := atoi(m[1])
		failed := v["Failures"] + v["Errors"]
		return parsed{"tool": "phpunit", "passed": total - failed - v["Skipped"], "failed": failed, "skipped": v["Skipped"], "total": total}
	}
	return nil
}

func parseSurefire(t string) parsed {
	ms := reSurefire.FindAllStringSubmatch(t, -1)
	if len(ms) == 0 {
		return nil
	}
	m := ms[len(ms)-1]
	total, fail, errs, skip := atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4])
	return parsed{"tool": "surefire", "passed": total - fail - errs - skip, "failed": fail + errs, "skipped": skip, "total": total}
}

func parseGradleTests(t string) parsed {
	m := reGradleTests.FindStringSubmatch(t)
	if m == nil {
		return nil
	}
	total, fail := atoi(m[1]), atoi(m[2])
	return parsed{"tool": "gradle test", "passed": total - fail, "failed": fail, "skipped": 0, "total": total}
}

func parseDotnetTest(t string) parsed {
	ms := reDotnetTest.FindAllStringSubmatch(t, -1)
	if len(ms) == 0 {
		return nil
	}
	f, p, s, tot := 0, 0, 0, 0
	for _, m := range ms {
		f += atoi(m[1])
		p += atoi(m[2])
		s += atoi(m[3])
		tot += atoi(m[4])
	}
	return parsed{"tool": "dotnet test", "passed": p, "failed": f, "skipped": s, "total": tot}
}

func parseTsc(t, cmd string, exit int) parsed {
	n := len(reTscErr.FindAllString(t, -1))
	if m := reTscFound.FindStringSubmatch(t); m != nil && atoi(m[1]) > n && (n > 0 || strings.Contains(cmd, "tsc")) {
		n = atoi(m[1])
	}
	if n > 0 {
		return parsed{"tool": "tsc", "errors": n}
	}
	if strings.Contains(cmd, "tsc") && exit == 0 {
		return parsed{"tool": "tsc", "errors": 0}
	}
	return nil
}

func parseEslint(t, cmd string, exit int) parsed {
	if m := reEslintSum.FindStringSubmatch(t); m != nil {
		return parsed{"tool": "eslint", "errors": atoi(m[2]), "warnings": atoi(m[3])}
	}
	errs, warns := 0, 0
	for _, m := range reEslintLine.FindAllStringSubmatch(t, -1) {
		if m[1] == "error" {
			errs++
		} else {
			warns++
		}
	}
	tool := "eslint"
	for _, m := range reNextLintLn.FindAllStringSubmatch(t, -1) {
		tool = "next lint"
		if m[1] == "Error" {
			errs++
		} else {
			warns++
		}
	}
	if errs+warns > 0 {
		return parsed{"tool": tool, "errors": errs, "warnings": warns}
	}
	if strings.Contains(t, "No ESLint warnings or errors") || (strings.Contains(cmd, "lint") && exit == 0 && strings.Contains(t, "eslint")) {
		return parsed{"tool": tool, "errors": 0, "warnings": 0}
	}
	return nil
}

func parseRuff(t string) parsed {
	if m := reRuffFound.FindStringSubmatch(t); m != nil {
		return parsed{"tool": "ruff", "errors": atoi(m[1])}
	}
	if strings.Contains(t, "All checks passed!") {
		return parsed{"tool": "ruff", "errors": 0}
	}
	return nil
}

func parseMypy(t string) parsed {
	if m := reMypyFound.FindStringSubmatch(t); m != nil {
		return parsed{"tool": "mypy", "errors": atoi(m[1]), "files_with_errors": atoi(m[2])}
	}
	if strings.Contains(t, "Success: no issues found") {
		return parsed{"tool": "mypy", "errors": 0}
	}
	return nil
}

func parseLint(kind, cmd, t string, exit int) parsed {
	lc := strings.ToLower(cmd)
	switch {
	case strings.Contains(lc, "flake8"):
		return parsed{"tool": "flake8", "errors": len(reFlake8.FindAllString(t, -1))}
	case strings.Contains(lc, "pyright"):
		if m := rePyright.FindStringSubmatch(t); m != nil {
			return parsed{"tool": "pyright", "errors": atoi(m[1]), "warnings": atoi(m[2])}
		}
	case strings.Contains(lc, "rubocop"):
		if m := reRubocop.FindStringSubmatch(t); m != nil {
			return parsed{"tool": "rubocop", "errors": atoi(m[1])}
		}
		if strings.Contains(t, "no offenses detected") {
			return parsed{"tool": "rubocop", "errors": 0}
		}
	case strings.Contains(lc, "vet") || strings.Contains(lc, "golangci"):
		return parsed{"tool": "go lint", "errors": len(reGoIssue.FindAllString(t, -1))}
	case strings.Contains(lc, "clippy"):
		n := 0
		for _, line := range strings.Split(t, "\n") {
			if reClippy.MatchString(line) && !strings.Contains(line, "generated") && !strings.Contains(line, "could not compile") && !strings.Contains(line, "aborting due") {
				n++
			}
		}
		return parsed{"tool": "clippy", "warnings_and_errors": n}
	case strings.Contains(lc, "phpstan"):
		if m := rePhpstan.FindStringSubmatch(t); m != nil {
			return parsed{"tool": "phpstan", "errors": atoi(m[1])}
		}
		if strings.Contains(t, "[OK] No errors") {
			return parsed{"tool": "phpstan", "errors": 0}
		}
	}
	return nil
}

func parseDotnetBuild(t string) parsed {
	w := reDotnetWarn.FindStringSubmatch(t)
	e := reDotnetErr.FindStringSubmatch(t)
	if w == nil && e == nil {
		return nil
	}
	p := parsed{"tool": "dotnet build"}
	if w != nil {
		p["warnings"] = atoi(w[1])
	}
	if e != nil {
		p["errors"] = atoi(e[1])
	}
	return p
}

// parseOutput reads a loop's combined output for the counts its tool prints.
// kind steers which parsers run first; nil means nothing recognisable.
func parseOutput(kind, cmd, text string, exit int) parsed {
	t := stripANSI(text)
	tests := []func() parsed{
		func() parsed { return parseVitest(t) }, func() parsed { return parseJest(t) }, func() parsed { return parseNodeTAP(t) },
		func() parsed { return parseMocha(t) }, func() parsed { return parsePytest(t) }, func() parsed { return parseGoTest(t) },
		func() parsed { return parseCargoTest(t) }, func() parsed { return parseRspec(t) }, func() parsed { return parsePhpunit(t) },
		func() parsed { return parseSurefire(t) }, func() parsed { return parseGradleTests(t) }, func() parsed { return parseDotnetTest(t) },
	}
	checks := []func() parsed{
		func() parsed { return parseTsc(t, cmd, exit) }, func() parsed { return parseMypy(t) },
		func() parsed { return parseEslint(t, cmd, exit) }, func() parsed { return parseRuff(t) },
		func() parsed { return parseLint(kind, cmd, t, exit) }, func() parsed { return parseDotnetBuild(t) },
	}
	if kind == "audit" {
		return parseAudit(cmd, text)
	}
	if kind == "install" {
		return nil // an install prints no counts, and its output mentions other tools' words
	}
	var order []func() parsed
	switch kind {
	case "test", "e2e":
		order = append(append(order, tests...), checks...)
	default:
		order = append(append(order, checks...), tests...)
	}
	for _, f := range order {
		if p := f(); p != nil {
			return p
		}
	}
	return nil
}

var reFailureHint = regexp.MustCompile(`(?i)\bFAIL\b|\bfailed\b|✗|✕|✖|\berror\b|panic:|AssertionError|Traceback`)

// failureExcerpt picks the first lines that look like a failure.
func failureExcerpt(text string, max int) []string {
	var out []string
	for _, ln := range strings.Split(stripANSI(text), "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "✔") || strings.HasPrefix(t, "✓") || strings.HasPrefix(t, "ok ") || strings.HasPrefix(t, "PASS") || strings.HasPrefix(t, "--- PASS") {
			continue
		}
		if reFailureHint.MatchString(ln) {
			if len(ln) > 300 {
				ln = ln[:300]
			}
			out = append(out, ln)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

func tailLines(text string, n int) []string {
	lines := strings.Split(strings.TrimRight(stripANSI(text), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return []string{}
	}
	for i, l := range lines {
		if len(l) > 400 {
			lines[i] = l[:400] + "..."
		}
	}
	return lines
}

var auditSeverities = []string{"info", "low", "moderate", "high", "critical"}

// parseAudit reads the advisory counts by severity from npm, pnpm and yarn
// audit JSON, and the vulnerability counts from govulncheck's text.
func parseAudit(cmd, text string) parsed {
	t := stripANSI(text)
	lc := strings.ToLower(cmd)
	if strings.Contains(lc, "govulncheck") {
		return parseGovulncheck(t)
	}
	tool := "audit"
	if f := strings.Fields(cmd); len(f) > 0 {
		tool = f[0] + " audit"
	}
	counts := map[string]int{}
	found := false
	add := func(sev string, n int) {
		sev = strings.ToLower(sev)
		if sev == "medium" {
			sev = "moderate"
		}
		if contains(auditSeverities, sev) {
			counts[sev] += n
			found = true
		}
	}
	var whole map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(t)), &whole) == nil && whole != nil {
		if md, ok := whole["metadata"].(map[string]any); ok {
			if v, ok := md["vulnerabilities"].(map[string]any); ok {
				for k, n := range v {
					if f, ok := n.(float64); ok {
						add(k, int(f))
					}
				}
			}
		}
		if !found {
			if adv, ok := whole["advisories"].(map[string]any); ok {
				for _, a := range adv {
					if am, ok := a.(map[string]any); ok {
						add(str(am["severity"]), 1)
					}
				}
				found = true
			}
		}
		if !found {
			if e, ok := whole["error"]; ok {
				msg := ""
				switch ev := e.(type) {
				case map[string]any:
					msg = firstNonEmpty(str(ev["code"]), str(ev["message"]))
				case string:
					msg = ev
				}
				return parsed{"tool": tool, "error": truncate(msg, 120)}
			}
		}
	} else {
		for _, ln := range strings.Split(t, "\n") {
			ln = strings.TrimSpace(ln)
			if !strings.HasPrefix(ln, "{") {
				continue
			}
			var m map[string]any
			if json.Unmarshal([]byte(ln), &m) != nil {
				continue
			}
			if m["type"] == "auditSummary" {
				if d, ok := m["data"].(map[string]any); ok {
					if v, ok := d["vulnerabilities"].(map[string]any); ok {
						for k, n := range v {
							if f, ok := n.(float64); ok {
								add(k, int(f))
							}
						}
						found = true
					}
				}
			} else if ch, ok := m["children"].(map[string]any); ok { // yarn berry: one advisory per line
				add(str(ch["Severity"]), 1)
			}
		}
	}
	if !found {
		return nil
	}
	p := parsed{"tool": tool}
	total := 0
	for _, s := range auditSeverities {
		p[s] = counts[s]
		total += counts[s]
	}
	p["total"] = total
	return p
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

var (
	reGovulnItem = regexp.MustCompile(`(?m)^Vulnerability #\d+:`)
	reGovulnNone = regexp.MustCompile(`No vulnerabilities found`)
)

// parseGovulncheck counts the vulnerabilities in the symbol results (code that
// calls the vulnerable function) and, separately, those only in package or
// module results. govulncheck reports no severity.
func parseGovulncheck(t string) parsed {
	if !reGovulnItem.MatchString(t) && !reGovulnNone.MatchString(t) && !strings.Contains(t, "govulncheck") {
		return nil
	}
	sym, rest := t, ""
	if i := strings.Index(t, "=== Package Results ==="); i >= 0 {
		sym, rest = t[:i], t[i:]
	} else if i := strings.Index(t, "=== Module Results ==="); i >= 0 {
		sym, rest = t[:i], t[i:]
	}
	called := len(reGovulnItem.FindAllString(sym, -1))
	other := len(reGovulnItem.FindAllString(rest, -1))
	return parsed{"tool": "govulncheck", "total": called, "called": called, "imported_or_required_only": other}
}
