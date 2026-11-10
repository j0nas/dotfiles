package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// sccFile is one source file as counted by scc.
type sccFile struct {
	Path       string `json:"path"`
	Lang       string `json:"lang"`
	Code       int64  `json:"code"`
	Comment    int64  `json:"comment"`
	Blank      int64  `json:"blank"`
	Lines      int64  `json:"lines"`
	Complexity int64  `json:"complexity"`
	Bytes      int64  `json:"bytes"`
	Generated  bool   `json:"generated,omitempty"`
	Minified   bool   `json:"minified,omitempty"`
}

type sccJSON []struct {
	Name  string `json:"Name"`
	Files []struct {
		Language   string `json:"Language"`
		Location   string `json:"Location"`
		Bytes      int64  `json:"Bytes"`
		Lines      int64  `json:"Lines"`
		Code       int64  `json:"Code"`
		Comment    int64  `json:"Comment"`
		Blank      int64  `json:"Blank"`
		Complexity int64  `json:"Complexity"`
		Minified   bool   `json:"Minified"`
		Generated  bool   `json:"Generated"`
	} `json:"Files"`
}

// docDataLangs are languages that describe or hold data rather than behaviour;
// they stay out of the code-only totals, TODO counts and hotspots.
var docDataLangs = map[string]bool{
	"Markdown": true, "Text": true, "Plain Text": true, "JSON": true, "YAML": true,
	"TOML": true, "CSV": true, "TSV": true, "SVG": true, "License": true,
	"gitignore": true, "ReStructuredText": true, "AsciiDoc": true, "Org": true,
	"Ignore": true, "Systemd": false,
}

func sccVersion() string {
	res, err := runCmd("", 10*time.Second, nil, "scc", "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(res.Stdout), "scc version"))
}

// runScc counts every file under root with scc, flagging generated and minified
// ones. respectIgnore keeps scc's .gitignore handling (right for a live working
// tree; an export holds tracked files only, so it turns it off).
func runScc(root string, respectIgnore bool) ([]sccFile, error) {
	if _, err := exec.LookPath("scc"); err != nil {
		return nil, fmt.Errorf("scc is not installed")
	}
	excl := make([]string, 0, len(vendorDirs))
	for d := range vendorDirs {
		excl = append(excl, d)
	}
	sort.Strings(excl)
	args := []string{"--by-file", "--format", "json", "-z", "--gen", "--no-cocomo", "--no-config", "--exclude-dir", strings.Join(excl, ",")}
	if !respectIgnore {
		args = append(args, "--no-gitignore", "--no-ignore", "--no-gitmodule")
	}
	args = append(args, ".")
	res, err := runCmd(root, 15*time.Minute, nil, "scc", args...)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("scc exited %d: %s", res.Code, firstLine(res.Stderr))
	}
	var parsed sccJSON
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		return nil, fmt.Errorf("scc output: %w", err)
	}
	var files []sccFile
	defer func() { setProjectRoots(sccPaths(files)) }()
	for _, l := range parsed {
		for _, f := range l.Files {
			files = append(files, sccFile{
				Path: strings.TrimPrefix(f.Location, "./"), Lang: f.Language,
				Code: f.Code, Comment: f.Comment, Blank: f.Blank, Lines: f.Lines,
				Complexity: f.Complexity, Bytes: f.Bytes, Generated: f.Generated, Minified: f.Minified,
			})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	setProjectRoots(sccPaths(files))
	return files, nil
}

func sccPaths(files []sccFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

type langSize struct {
	Files      int64 `json:"files"`
	Code       int64 `json:"code"`
	Comment    int64 `json:"comment"`
	Blank      int64 `json:"blank"`
	Complexity int64 `json:"complexity"`
}

func (l *langSize) add(f sccFile) {
	l.Files++
	l.Code += f.Code
	l.Comment += f.Comment
	l.Blank += f.Blank
	l.Complexity += f.Complexity
}

type sizeSection struct {
	Unavailable    string              `json:"unavailable,omitempty"`
	Tool           string              `json:"tool,omitempty"`
	ByLanguage     map[string]langSize `json:"by_language,omitempty"`
	Totals         *langSize           `json:"totals,omitempty"`
	TotalsCodeOnly *langSize           `json:"totals_code_only,omitempty"`
	Excluded       *excludedCounts     `json:"excluded,omitempty"`
	ExcludedDirs   []string            `json:"excluded_dirs,omitempty"`
	Note           string              `json:"note,omitempty"`
}

type excludedCounts struct {
	Generated     int   `json:"generated_files"`
	Minified      int   `json:"minified_files"`
	Vendored      int   `json:"vendored_files"`
	GeneratedCode int64 `json:"generated_code_lines"`
	MinifiedCode  int64 `json:"minified_code_lines"`
}

// summarizeSize totals scc's files, leaving generated and minified ones out and
// counting them separately.
func summarizeSize(files []sccFile) sizeSection {
	s := sizeSection{
		Tool: "scc " + sccVersion(), ByLanguage: map[string]langSize{},
		Totals: &langSize{}, TotalsCodeOnly: &langSize{}, Excluded: &excludedCounts{},
		ExcludedDirs: sortedKeys(vendorDirs),
		Note:         "generated and minified files (as flagged by scc) are left out of every figure here",
	}
	for _, f := range files {
		switch {
		case isVendoredPath(f.Path):
			s.Excluded.Vendored++
			continue
		case f.Generated:
			s.Excluded.Generated++
			s.Excluded.GeneratedCode += f.Code
			continue
		case f.Minified:
			s.Excluded.Minified++
			s.Excluded.MinifiedCode += f.Code
			continue
		}
		l := s.ByLanguage[f.Lang]
		l.add(f)
		s.ByLanguage[f.Lang] = l
		s.Totals.add(f)
		if !docDataLangs[f.Lang] {
			s.TotalsCodeOnly.add(f)
		}
	}
	return s
}

// countedFiles drops generated, minified and vendored files.
func countedFiles(files []sccFile) []sccFile {
	var out []sccFile
	for _, f := range files {
		if f.Generated || f.Minified || isVendoredPath(f.Path) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// testPatterns recognise test files across stacks; a file counts once, under
// the first pattern that matches.
var testPatterns = []struct {
	Label string
	Re    *regexp.Regexp
}{
	{"js_test_spec", regexp.MustCompile(`\.(test|spec)\.[cm]?[jt]sx?$`)},
	{"js_tests_dir", regexp.MustCompile(`(^|/)__tests__/.+\.[cm]?[jt]sx?$`)},
	{"js_test_folder", regexp.MustCompile(`(^|/)tests?/.+\.[cm]?[jt]sx?$`)},
	{"e2e_dir", regexp.MustCompile(`(^|/)(e2e|cypress|playwright)/.+\.(ts|tsx|js|jsx|mjs|py|rb|java|kt|go|cs)$`)},
	{"python", regexp.MustCompile(`(^|/)test_[^/]+\.py$|_test\.py$|(^|/)tests?/.+\.py$`)},
	{"go", regexp.MustCompile(`_test\.go$`)},
	{"rust_tests_dir", regexp.MustCompile(`(^|/)tests/[^/]+\.rs$`)},
	{"jvm", regexp.MustCompile(`(Tests?|IT|Spec)\.(java|kt|scala|groovy)$|(^|/)src/test/.+\.(java|kt|scala|groovy)$`)},
	{"dotnet", regexp.MustCompile(`(Tests?|Spec)\.(cs|fs|vb)$`)},
	{"ruby", regexp.MustCompile(`_spec\.rb$|_test\.rb$|(^|/)(spec|test)/.+\.rb$`)},
	{"php", regexp.MustCompile(`Test\.php$|(^|/)tests?/.+\.php$`)},
	{"elixir_dart_swift", regexp.MustCompile(`_test\.exs?$|_test\.dart$|Tests\.swift$`)},
}

type testCounts struct {
	Total       int            `json:"total"`
	ByPattern   map[string]int `json:"by_pattern"`
	Unavailable string         `json:"unavailable,omitempty"`
}

// MarshalJSON records "unavailable" instead of a total of 0: no file matched a
// recognised test layout, which does not mean there are no tests.
func (t testCounts) MarshalJSON() ([]byte, error) {
	type plain struct {
		Total       *int           `json:"total,omitempty"`
		ByPattern   map[string]int `json:"by_pattern,omitempty"`
		Unavailable string         `json:"unavailable,omitempty"`
	}
	p := plain{ByPattern: t.ByPattern, Unavailable: t.Unavailable}
	if t.Total == 0 {
		p.Unavailable = "no file matches a recognised test layout (*.test.*, *_test.*, test_*.py, tests/ folders, __tests__/, src/test/, spec/); the project may keep tests elsewhere"
		p.ByPattern = nil
	} else {
		p.Total = &t.Total
	}
	return json.Marshal(p)
}

func countTestFiles(files []string) testCounts {
	tc := testCounts{ByPattern: map[string]int{}}
	for _, f := range files {
		if isVendoredPath(f) {
			continue
		}
		for _, p := range testPatterns {
			if p.Re.MatchString(f) {
				tc.Total++
				tc.ByPattern[p.Label]++
				break
			}
		}
	}
	return tc
}

var reTodo = regexp.MustCompile(`\b(TODO|FIXME|HACK|XXX)\b`)

type todoCounts struct {
	Total int            `json:"total"`
	ByTag map[string]int `json:"by_tag"`
}

// countTodos scans the code files (not docs or data) for marker comments.
func countTodos(root string, files []sccFile) todoCounts {
	tc := todoCounts{ByTag: map[string]int{"TODO": 0, "FIXME": 0, "HACK": 0, "XXX": 0}}
	for _, f := range countedFiles(files) {
		if docDataLangs[f.Lang] {
			continue
		}
		text, ok := readText(root, f.Path, 2<<20)
		if !ok {
			continue
		}
		for _, m := range reTodo.FindAllString(text, -1) {
			tc.ByTag[m]++
			tc.Total++
		}
	}
	return tc
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
