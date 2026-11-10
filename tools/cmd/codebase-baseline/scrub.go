package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type scrubFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Pattern string `json:"pattern"`
	Masked  string `json:"masked"`
}

var scrubPatterns = []struct {
	Name  string
	Re    *regexp.Regexp
	Group int // submatch to mask (0 = the whole match)
	Keep  func(string) bool
}{
	{"AWS access key id", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), 0, nil},
	{"Stripe live key", regexp.MustCompile(`\b[sr]k_live_[0-9A-Za-z]{10,}`), 0, nil},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})`), 0, nil},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`), 0, nil},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`), 0, nil},
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), 0, nil},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), 0, nil},
	{"database URL with password", regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|amqps?)://[^/\s:@'"]+:([^@\s/'"]{3,})@`), 1, func(v string) bool {
		return !strings.ContainsAny(v, "${}<>") && !strings.Contains(strings.ToLower(v), "redacted") && !strings.EqualFold(v, "password") && !strings.EqualFold(v, "pass")
	}},
	{"key, secret, token or password assignment", regexp.MustCompile(`(?i)(?:key|secret|token|passw(?:or)?d)["']?\s*[:=]\s*["']?([^\s"',;]{12,})`), 1, func(v string) bool {
		l := strings.ToLower(v)
		var letter, digit bool
		for _, c := range v {
			letter = letter || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
			digit = digit || c >= '0' && c <= '9'
		}
		if !letter || !digit { // words and identifiers (GUEST_USER_PASSWORD, test-secret-for-signup) are not secrets
			return false
		}
		return !(strings.ContainsAny(v, "${}<>()[]") || strings.Contains(l, "redacted") || strings.HasPrefix(l, "process.") || strings.HasPrefix(l, "os.") || strings.HasPrefix(l, "env.") ||
			strings.HasPrefix(l, "http") || strings.Contains(l, "example") || strings.Contains(l, "xxxx") || strings.Count(v, "*") > 3 || strings.Contains(l, "..."))
	}},
}

// maskSecret shows enough to find the value, not enough to use it.
func maskSecret(v string) string {
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:2] + strings.Repeat("*", 4) + v[len(v)-2:] + fmt.Sprintf(" (%d chars)", len(v))
}

var scrubSkipDirs = map[string]bool{"node_modules": true, ".git": true, ".venv": true, "venv": true, "__pycache__": true}

// scrubFile reads one text file and returns what looks like a secret in it.
func scrubFile(p, label string) []scrubFinding {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Size() > 8<<20 {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil || strings.IndexByte(string(b[:min(len(b), 4096)]), 0) >= 0 {
		return nil
	}
	var out []scrubFinding
	for i, ln := range strings.Split(string(b), "\n") {
		if len(ln) > 20000 {
			ln = ln[:20000]
		}
		for _, sp := range awsSecretSpans(ln) {
			out = append(out, scrubFinding{File: label, Line: i + 1, Pattern: "AWS secret access key", Masked: maskSecret(ln[sp[0]:sp[1]])})
		}
		for _, pat := range scrubPatterns {
			for _, m := range pat.Re.FindAllStringSubmatch(ln, -1) {
				v := m[pat.Group]
				if pat.Keep != nil && !pat.Keep(v) {
					continue
				}
				out = append(out, scrubFinding{File: label, Line: i + 1, Pattern: pat.Name, Masked: maskSecret(v)})
			}
		}
	}
	return out
}

func (a *app) cmdScrub(args []string) error {
	fl := flag.NewFlagSet("scrub", flag.ContinueOnError)
	var reports stringList
	fl.Var(&reports, "report", "an extra file (the HTML report) to scan; repeatable")
	format := fl.String("format", "text", "text or json")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline scrub <dir> [--report <html>] [--format text|json]")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fl.Usage()
		return fmt.Errorf("scrub needs <dir>")
	}
	root := pos[0]
	if !isDir(root) {
		return fmt.Errorf("%s is not a directory", root)
	}
	var findings []scrubFinding
	scanned := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (scrubSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".home-")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		scanned++
		findings = append(findings, scrubFile(p, filepath.ToSlash(rel))...)
		return nil
	})
	for _, r := range reports {
		scanned++
		findings = append(findings, scrubFile(r, r)...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	if *format == "json" {
		b, _ := json.MarshalIndent(map[string]any{"schema_version": schemaVersion, "tool": currentTool(), "files_scanned": scanned, "findings": findings}, "", "  ")
		fmt.Fprintln(a.stdout, string(b))
	} else if len(findings) == 0 {
		fmt.Fprintf(a.stdout, "scrub: %d files scanned, no secret-looking values\n", scanned)
	} else {
		fmt.Fprintf(a.stdout, "scrub: %d files scanned, %d secret-looking values:\n", scanned, len(findings))
		for _, f := range findings {
			fmt.Fprintf(a.stdout, "  %s:%d: %s: %s\n", f.File, f.Line, f.Pattern, f.Masked)
		}
	}
	if len(findings) > 0 {
		return errFailed
	}
	return nil
}
