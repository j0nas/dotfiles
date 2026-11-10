package main

import (
	"regexp"
	"sort"
	"strings"
)

// ciStep is one command a CI config runs, with where it came from. It is
// reference material: discover never runs it.
type ciStep struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Cmd        string   `json:"cmd"`
	Kind       string   `json:"kind"`
	Job        string   `json:"job,omitempty"`
	WorkingDir string   `json:"working_directory,omitempty"`
	Services   []string `json:"services,omitempty"`
	EnvNames   []string `json:"env_names,omitempty"`
}

var (
	reYamlKey   = regexp.MustCompile(`^(\s*)(-\s+)?([A-Za-z0-9_.'"/-]+):(?:\s+(.*))?$`)
	reGlabInstl = regexp.MustCompile(`(?i)\b(npm (ci|install|i)\b|pnpm (install|i)\b|yarn( install)?( --|\s*$)|bun install|pip3? install|poetry install|uv sync|pipenv install|go mod download|bundle install|composer install|cargo fetch|dotnet restore)`)
	reKindE2E   = regexp.MustCompile(`(?i)playwright|cypress|puppeteer|e2e|selenium`)
	reKindFmt   = regexp.MustCompile(`(?i)prettier.*(--check|-c\b)|format:check|format-check|fmt.*--check|--check.*fmt|black --check|gofmt -l|isort --check|dotnet format|ruff format|biome format`)
	reKindType  = regexp.MustCompile(`(?i)\btsc\b|typecheck|type-check|check-types|\bmypy\b|pyright|vue-tsc|svelte-check|astro check`)
	reKindLint  = regexp.MustCompile(`(?i)lint|ruff|flake8|pylint|rubocop|clippy|golangci|biome (check|ci)|phpstan|psalm|oxlint|go vet|checkstyle|spotless`)
	reKindTest  = regexp.MustCompile(`(?i)\b(test|tests|jest|vitest|pytest|mocha|rspec|phpunit|nextest)\b|go test|cargo test|dotnet test|\bnpm t\b|test:`)
	reKindBuild = regexp.MustCompile(`(?i)\bbuild\b|compile|go build|cargo build|assemble|\bpackage\b|webpack|vite build|next build|tsc -b`)
)

// classifyCmdKind guesses what kind of loop a shell command is.
func classifyCmdKind(cmd string) string {
	switch {
	case reGlabInstl.MatchString(cmd):
		return "install"
	case reKindE2E.MatchString(cmd):
		return "e2e"
	case reKindFmt.MatchString(cmd):
		return "format"
	case reKindType.MatchString(cmd):
		return "typecheck"
	case reKindLint.MatchString(cmd):
		return "lint"
	case reKindTest.MatchString(cmd):
		return "test"
	case reKindBuild.MatchString(cmd):
		return "build"
	}
	return "other"
}

func indentOf(s string) int {
	n := 0
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		n++
	}
	return n
}

func isBlankOrComment(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || strings.HasPrefix(t, "#")
}

func unquoteYAML(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

type scriptLine struct {
	Line int
	Text string
}

// blockLines reads the lines of a block scalar that starts after line i; key is
// the column of the key that owns it.
func blockLines(lines []string, i, keyCol int) (out []scriptLine, end int) {
	j := i + 1
	for ; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if indentOf(lines[j]) <= keyCol {
			break
		}
		out = append(out, scriptLine{Line: j + 1, Text: strings.TrimSpace(lines[j])})
	}
	return out, j
}

// joinContinuations merges lines ending in a backslash and drops comments.
func joinContinuations(in []scriptLine) []scriptLine {
	var out []scriptLine
	for i := 0; i < len(in); i++ {
		cur := in[i]
		for strings.HasSuffix(cur.Text, "\\") && i+1 < len(in) {
			i++
			cur.Text = strings.TrimSpace(strings.TrimSuffix(cur.Text, "\\")) + " " + in[i].Text
		}
		if cur.Text == "" || strings.HasPrefix(cur.Text, "#") {
			continue
		}
		out = append(out, cur)
	}
	return out
}

// childKeys collects the keys one level below the line at index i.
func childKeys(lines []string, i int) []string {
	base := indentOf(lines[i])
	child := -1
	var keys []string
	for j := i + 1; j < len(lines); j++ {
		if isBlankOrComment(lines[j]) {
			continue
		}
		ind := indentOf(lines[j])
		if ind <= base {
			break
		}
		if child == -1 {
			child = ind
		}
		if ind == child {
			if m := reYamlKey.FindStringSubmatch(lines[j]); m != nil {
				keys = append(keys, strings.Trim(m[3], `"'`))
			} else if t := strings.TrimSpace(lines[j]); strings.HasPrefix(t, "- ") {
				keys = append(keys, strings.Trim(strings.TrimPrefix(t, "- "), `"'`))
			}
		}
	}
	return keys
}

type ghJob struct {
	services []string
	env      []string
}

// parseGitHubWorkflow extracts the run: steps of a workflow file with a
// tolerant line reader (no YAML library).
func parseGitHubWorkflow(file, text string) []ciStep {
	lines := strings.Split(text, "\n")
	var steps []ciStep
	jobs := map[string]*ghJob{}
	var stepJobs []string
	inJobs := false
	jobIndent := -1
	cur := ""
	for i, raw := range lines {
		if isBlankOrComment(raw) {
			continue
		}
		m := reYamlKey.FindStringSubmatch(raw)
		ind := indentOf(raw)
		if ind == 0 {
			inJobs = m != nil && m[3] == "jobs"
			jobIndent = -1
			continue
		}
		if !inJobs || m == nil {
			continue
		}
		if jobIndent == -1 {
			jobIndent = ind
		}
		key := strings.Trim(m[3], `"'`)
		keyCol := len(m[1]) + len(m[2])
		if ind == jobIndent && m[2] == "" {
			cur = key
			if jobs[cur] == nil {
				jobs[cur] = &ghJob{}
			}
			continue
		}
		if cur == "" {
			continue
		}
		val := strings.TrimSpace(m[4])
		switch {
		case key == "services" && keyCol == jobIndent+2 && val == "":
			jobs[cur].services = append(jobs[cur].services, childKeys(lines, i)...)
		case key == "env" && keyCol == jobIndent+2 && val == "":
			jobs[cur].env = append(jobs[cur].env, childKeys(lines, i)...)
		case key == "run" && val != "":
			var cmds []scriptLine
			if strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">") {
				body, _ := blockLines(lines, i, keyCol)
				if strings.HasPrefix(val, ">") {
					var parts []string
					for _, b := range body {
						parts = append(parts, b.Text)
					}
					if len(body) > 0 {
						cmds = []scriptLine{{Line: body[0].Line, Text: strings.Join(parts, " ")}}
					}
				} else {
					cmds = joinContinuations(body)
				}
			} else {
				cmds = []scriptLine{{Line: i + 1, Text: unquoteYAML(val)}}
			}
			wd, env := stepSiblings(lines, i, keyCol, m[2] != "")
			for _, c := range cmds {
				steps = append(steps, ciStep{File: file, Line: c.Line, Cmd: c.Text, Kind: classifyCmdKind(c.Text), Job: cur, WorkingDir: wd, EnvNames: env})
				stepJobs = append(stepJobs, cur)
			}
		}
	}
	for i := range steps {
		if j := jobs[stepJobs[i]]; j != nil {
			steps[i].Services = uniqueSorted(j.services)
			steps[i].EnvNames = uniqueSorted(append(append([]string(nil), steps[i].EnvNames...), j.env...))
		}
	}
	return steps
}

// stepSiblings finds working-directory and the env names of the step that owns
// the run: key at line i.
func stepSiblings(lines []string, i, keyCol int, startsStep bool) (wd string, env []string) {
	start := i
	if !startsStep {
		for start > 0 {
			start--
			if isBlankOrComment(lines[start]) {
				continue
			}
			if indentOf(lines[start]) < keyCol {
				// the "- " line sits at keyCol-2
				break
			}
		}
	}
	end := i + 1
	for end < len(lines) {
		if isBlankOrComment(lines[end]) {
			end++
			continue
		}
		ind := indentOf(lines[end])
		if ind < keyCol || (ind == keyCol-2 && strings.HasPrefix(strings.TrimSpace(lines[end]), "-")) {
			break
		}
		end++
	}
	for j := start; j < end && j < len(lines); j++ {
		m := reYamlKey.FindStringSubmatch(lines[j])
		if m == nil || len(m[1])+len(m[2]) != keyCol {
			continue
		}
		switch m[3] {
		case "working-directory":
			wd = unquoteYAML(m[4])
		case "env":
			if strings.TrimSpace(m[4]) == "" {
				env = append(env, childKeys(lines, j)...)
			}
		}
	}
	return wd, env
}

var gitlabReserved = map[string]bool{
	"stages": true, "variables": true, "default": true, "include": true, "workflow": true, "image": true,
	"services": true, "cache": true, "before_script": true, "after_script": true, "pages": false,
}

// parseGitLabCI extracts script, before_script and after_script commands.
func parseGitLabCI(file, text string) []ciStep {
	lines := strings.Split(text, "\n")
	var steps []ciStep
	job := ""
	jobServices := map[string][]string{}
	var stepJobs []string
	for i, raw := range lines {
		if isBlankOrComment(raw) {
			continue
		}
		m := reYamlKey.FindStringSubmatch(raw)
		ind := indentOf(raw)
		if ind == 0 && m != nil && m[2] == "" {
			job = m[3]
			if gitlabReserved[job] {
				job = ""
			}
			if job == "before_script" || job == "after_script" {
				job = "(global)"
			}
		}
		if m == nil {
			continue
		}
		key := m[3]
		keyCol := len(m[1]) + len(m[2])
		if key == "services" && ind > 0 && strings.TrimSpace(m[4]) == "" {
			for j := i + 1; j < len(lines); j++ {
				if isBlankOrComment(lines[j]) {
					continue
				}
				t := strings.TrimSpace(lines[j])
				if indentOf(lines[j]) < keyCol || (indentOf(lines[j]) == keyCol && !strings.HasPrefix(t, "-")) {
					break
				}
				if strings.HasPrefix(t, "- name:") {
					jobServices[job] = append(jobServices[job], strings.TrimSpace(strings.TrimPrefix(t, "- name:")))
				} else if strings.HasPrefix(t, "- ") {
					jobServices[job] = append(jobServices[job], strings.TrimSpace(strings.TrimPrefix(t, "- ")))
				}
			}
			continue
		}
		if key != "script" && key != "before_script" && key != "after_script" {
			continue
		}
		val := strings.TrimSpace(m[4])
		var cmds []scriptLine
		if val != "" && !strings.HasPrefix(val, "[") {
			cmds = append(cmds, scriptLine{Line: i + 1, Text: unquoteYAML(val)})
		} else {
			for j := i + 1; j < len(lines); j++ {
				if isBlankOrComment(lines[j]) {
					continue
				}
				t := strings.TrimSpace(lines[j])
				ind := indentOf(lines[j])
				if ind < keyCol || (ind == keyCol && !strings.HasPrefix(t, "-")) {
					break
				}
				if !strings.HasPrefix(t, "-") {
					continue
				}
				item := strings.TrimSpace(strings.TrimPrefix(t, "-"))
				if item == "|" || item == ">" || item == "|-" || item == ">-" {
					body, _ := blockLines(lines, j, ind)
					cmds = append(cmds, joinContinuations(body)...)
					continue
				}
				if item != "" {
					cmds = append(cmds, scriptLine{Line: j + 1, Text: unquoteYAML(item)})
				}
			}
		}
		for _, c := range cmds {
			steps = append(steps, ciStep{File: file, Line: c.Line, Cmd: c.Text, Kind: classifyCmdKind(c.Text), Job: job})
			stepJobs = append(stepJobs, job)
		}
	}
	for i := range steps {
		steps[i].Services = uniqueSorted(jobServices[stepJobs[i]])
	}
	return steps
}

// ciCoverage says which loop kinds the CI steps run.
func ciCoverage(steps []ciStep) (run, notRun []string) {
	seen := map[string]bool{}
	for _, s := range steps {
		seen[s.Kind] = true
	}
	for _, k := range []string{"install", "typecheck", "lint", "format", "test", "build", "e2e"} {
		if seen[k] {
			run = append(run, k)
		} else {
			notRun = append(notRun, k)
		}
	}
	sort.Strings(run)
	sort.Strings(notRun)
	return
}
