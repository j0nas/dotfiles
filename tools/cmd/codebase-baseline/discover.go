package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// loop is a candidate feedback-loop command. The orchestrating agent edits
// enabled and extra_env before run-loops runs it.
type loop struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Cmd       string            `json:"cmd"`
	Cwd       string            `json:"cwd"`
	Source    string            `json:"source"`
	TimeoutS  int               `json:"timeout_s"`
	Enabled   bool              `json:"enabled"`
	Needs     []string          `json:"needs"`
	ExtraEnv  map[string]string `json:"extra_env"`
	Ecosystem string            `json:"ecosystem"`
	Requires  []string          `json:"requires,omitempty"`
	InCI      bool              `json:"in_ci"`
	// ExitIsResult marks audits: a non-zero exit reports findings, it is not a broken loop.
	ExitIsResult bool `json:"exit_is_result,omitempty"`
	// RealHome opts the loop out of the sandboxed HOME (private-registry installs the user approved).
	RealHome bool   `json:"real_home,omitempty"`
	Note     string `json:"note,omitempty"`

	text     string // resolved script text (hooks and nested runs) for the needs analysis
	force    *bool  // overrides the default enabled guess (audits)
	matchKey string // substring a CI step contains when it runs the same thing
	script   string // package.json script name, for matching "pnpm run <name>"
	pm       string
	runner   bool // came from make/just/task
}

type skippedItem struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type ciCoverageOut struct {
	KindsRun    []string `json:"kinds_run_in_ci"`
	KindsNotRun []string `json:"kinds_not_run_in_ci"`
}

type signals struct {
	PackageManagers  map[string]string `json:"package_managers,omitempty"`
	RuntimeHints     map[string]string `json:"runtime_hints,omitempty"`
	ComposeServices  []string          `json:"compose_services,omitempty"`
	EnvExampleKeys   []string          `json:"env_example_keys,omitempty"`
	CIServices       []string          `json:"ci_services,omitempty"`
	CIEnvNames       []string          `json:"ci_env_names,omitempty"`
	WorkspaceMembers int               `json:"js_workspace_members,omitempty"`
	ExternalConfig   []externalHit     `json:"external_config,omitempty"`
}

type discoverOut struct {
	SchemaVersion int           `json:"schema_version"`
	Tool          toolInfo      `json:"tool"`
	Repo          string        `json:"repo"`
	GeneratedAt   string        `json:"generated_at"`
	Ecosystems    []string      `json:"ecosystems"`
	Signals       signals       `json:"signals"`
	Loops         []loop        `json:"loops"`
	CISteps       []ciStep      `json:"ci_steps"`
	CICoverage    ciCoverageOut `json:"ci_coverage"`
	Skipped       []skippedItem `json:"skipped"`
	Truncated     []string      `json:"truncated"`
	Notes         []string      `json:"notes"`
}

func (a *app) cmdDiscover(args []string) error {
	fl := flag.NewFlagSet("discover", flag.ContinueOnError)
	out := fl.String("out", "", "loops.json to write (required)")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline discover <repo> --out loops.json")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *out == "" {
		fl.Usage()
		return fmt.Errorf("discover needs <repo> and --out")
	}
	if !isDir(pos[0]) {
		return fmt.Errorf("%s is not a directory", pos[0])
	}
	if err := refuseInside(*out, pos[0]); err != nil {
		return err
	}
	res := discoverLoops(pos[0], a.now())
	if err := writeJSON(*out, res); err != nil {
		return err
	}
	counts := map[string]int{}
	enabled := 0
	for _, l := range res.Loops {
		counts[l.Kind]++
		if l.Enabled {
			enabled++
		}
	}
	fmt.Fprintf(a.stdout, "wrote %s: %d candidate loops (%d enabled), %d CI steps, ecosystems: %s\n", *out, len(res.Loops), enabled, len(res.CISteps), strings.Join(res.Ecosystems, ", "))
	for _, k := range sortedKeys(counts) {
		fmt.Fprintf(a.stdout, "  %-10s %d\n", k, counts[k])
	}
	return nil
}

type discoverer struct {
	repo      string
	files     []string
	fileSet   map[string]bool
	loops     []loop
	skipped   []skippedItem
	truncated []string
	ids       map[string]int
	sig       signals
	eco       map[string]bool
}

var timeoutByKind = map[string]int{"audit": 600, "install": 900, "typecheck": 600, "lint": 600, "test": 900, "build": 900, "e2e": 900, "format": 300, "other": 600}

var kindOrder = map[string]int{"install": 0, "lint": 1, "format": 2, "typecheck": 3, "build": 4, "test": 5, "e2e": 6, "audit": 7, "other": 8}

func discoverLoops(repo string, now time.Time) discoverOut {
	d := &discoverer{repo: repo, files: listRepoFiles(repo), fileSet: map[string]bool{}, ids: map[string]int{}, eco: map[string]bool{},
		sig: signals{PackageManagers: map[string]string{}, RuntimeHints: map[string]string{}}}
	for _, f := range d.files {
		d.fileSet[f] = true
	}
	d.jsProjects()
	d.pythonProjects()
	d.goProjects()
	d.rustProjects()
	d.gradleProjects()
	d.mavenProjects()
	d.dotnetProjects()
	d.rubyProjects()
	d.phpProjects()
	d.taskRunners()
	d.collectSignals()
	d.scanExternalConfig()
	steps := d.ciSteps()
	d.finish(steps)

	res := discoverOut{SchemaVersion: schemaVersion, Tool: currentTool(), Repo: absPath(repo), GeneratedAt: now.UTC().Format(time.RFC3339),
		Signals: d.sig, Loops: d.loops, CISteps: steps, Skipped: d.skipped, Truncated: d.truncated}
	res.Ecosystems = sortedKeys(d.eco)
	if res.CISteps == nil {
		res.CISteps = []ciStep{}
	}
	res.CICoverage.KindsRun, res.CICoverage.KindsNotRun = ciCoverage(steps)
	if res.CICoverage.KindsRun == nil {
		res.CICoverage.KindsRun = []string{}
	}
	if res.Truncated == nil {
		res.Truncated = []string{}
	}
	if res.Skipped == nil {
		res.Skipped = []skippedItem{}
	}
	if res.Loops == nil {
		res.Loops = []loop{}
	}
	res.Notes = []string{
		"enabled is a default guess; edit it, and put dummy values for anything a loop needs in extra_env",
		"ci_steps are reference only and are never run",
		"needs are guesses from commands, CI services and env names; an empty list does not prove the loop is self-contained",
	}
	return res
}

func (d *discoverer) hasFile(rel string) bool { return d.fileSet[rel] }

func (d *discoverer) read(rel string) string {
	t, _ := readText(d.repo, rel, 4<<20)
	return t
}

func join(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// dirsWith lists the directories holding a file with one of the given base
// names, up to maxDepth deep, keeping only the top-most of nested ones.
func (d *discoverer) dirsWith(match func(base string) bool, maxDepth int, keepNested bool) []string {
	set := map[string]bool{}
	for _, f := range d.files {
		if !match(path.Base(f)) || strings.Contains(f, "testdata/") || strings.Contains(f, "/fixtures/") {
			continue
		}
		dir := path.Dir(f)
		depth := 0
		if dir != "." {
			depth = strings.Count(dir, "/") + 1
		}
		if depth <= maxDepth {
			set[dir] = true
		}
	}
	dirs := sortedKeys(set)
	if keepNested {
		return dirs
	}
	var top []string
	for _, dir := range dirs {
		nested := false
		for _, t := range top {
			if t == "." || strings.HasPrefix(dir, t+"/") {
				nested = true
				break
			}
		}
		if !nested {
			top = append(top, dir)
		}
	}
	return top
}

func (d *discoverer) capDirs(eco string, dirs []string, max int) []string {
	if len(dirs) > max {
		d.truncated = append(d.truncated, fmt.Sprintf("%s: %d projects found, only the first %d were considered", eco, len(dirs), max))
		return dirs[:max]
	}
	return dirs
}

var (
	reNeedDB      = regexp.MustCompile(`(?i)DATABASE_URL|POSTGRES|PGHOST|PG_URL|MYSQL|MONGO|DB_HOST|prisma (migrate|db)|\bknex\b|typeorm|alembic|flyway|liquibase|\bpsql\b|testcontainers`)
	reNeedDocker  = regexp.MustCompile(`(?i)\bdocker\b|podman|testcontainers`)
	reNeedBrowser = regexp.MustCompile(`(?i)playwright|cypress|puppeteer|selenium|chromium|webdriver`)
	reNeedNet     = regexp.MustCompile(`(?i)REDIS|RABBIT|KAFKA|ELASTIC|MINIO|S3_ENDPOINT|localhost:\d|127\.0\.0\.1|wait-on|\bsmtp\b`)
	reNeedSecret  = regexp.MustCompile(`(?i)API_KEY|SECRET|TOKEN|PASSWORD|dotenv|\.env\b`)
)

// guessNeeds reads a command (and any env names around it) for services it
// likely depends on.
func guessNeeds(text string) []string {
	needs := []string{}
	if reNeedDB.MatchString(text) {
		needs = append(needs, "database")
	}
	if reNeedDocker.MatchString(text) {
		needs = append(needs, "docker")
	}
	if reNeedBrowser.MatchString(text) {
		needs = append(needs, "browser")
	}
	if reNeedNet.MatchString(text) {
		needs = append(needs, "network-service")
	}
	if reNeedSecret.MatchString(text) {
		needs = append(needs, "secrets")
	}
	return needs
}

func hasServiceNeed(needs []string) bool {
	for _, n := range needs {
		if n == "database" || n == "docker" || n == "browser" || n == "network-service" {
			return true
		}
	}
	return false
}

func addNeed(needs []string, n string) []string {
	if !contains(needs, n) {
		needs = append(needs, n)
	}
	return needs
}

// add fills in the defaults and registers a candidate.
func (d *discoverer) add(l loop) {
	if l.Cwd == "" {
		l.Cwd = "."
	}
	if l.Needs == nil {
		l.Needs = []string{}
	}
	l.Needs = uniqueKeepOrder(append(l.Needs, guessNeeds(l.Cmd+" "+l.text)...))
	deploy := deployToolsIn(l.Cmd + " ; " + l.text)
	for _, dt := range deploy {
		l.Needs = addNeed(l.Needs, "deploy-tool:"+dt)
	}
	l.ExtraEnv = map[string]string{}
	if l.TimeoutS == 0 {
		l.TimeoutS = timeoutByKind[l.Kind]
	}
	l.Enabled = (l.Kind != "e2e" && l.Kind != "other" && l.Kind != "audit") && !hasServiceNeed(l.Needs)
	if l.force != nil {
		l.Enabled = *l.force
	}
	if len(deploy) > 0 {
		l.Enabled = false
		l.Note = strings.TrimSpace(l.Note + " Disabled: it runs or calls " + strings.Join(deploy, ", ") + ", which deploys or publishes.")
	}
	if l.Kind == "e2e" {
		l.Needs = addNeed(l.Needs, "browser")
	}
	base := l.ID
	d.ids[base]++
	if d.ids[base] > 1 {
		l.ID = fmt.Sprintf("%s-%d", base, d.ids[base])
	}
	d.eco[l.Ecosystem] = true
	d.loops = append(d.loops, l)
}

func uniqueKeepOrder(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// addAudit registers a dependency-audit loop. It is enabled when its tool is on
// PATH (or always, for the JavaScript package managers, which are there by
// definition); otherwise it is off and needs "tool:<name>".
func (d *discoverer) addAudit(l loop, bin string, always bool) {
	l.Kind = "audit"
	l.ExitIsResult = true
	on := always
	if !always {
		if _, err := exec.LookPath(bin); err == nil {
			on = true
		} else {
			l.Needs = append(l.Needs, "tool:"+bin)
			l.Note = strings.TrimSpace(l.Note + " " + bin + " is not on PATH.")
		}
	}
	l.force = &on
	if l.Note == "" {
		l.Note = "a non-zero exit means advisories were found; that is the result, not a failure"
	}
	d.add(l)
}

func (d *discoverer) skip(source, name, reason string) {
	d.skipped = append(d.skipped, skippedItem{Source: source, Name: name, Reason: reason})
}

func idFor(eco, cwd, name string) string {
	parts := []string{eco}
	if cwd != "." && cwd != "" {
		parts = append(parts, slugify(cwd))
	}
	parts = append(parts, slugify(name))
	return strings.Join(parts, "-")
}

// classifyName decides what a script, make target or task name is for. It
// returns "" with a reason when the name should not become a candidate.
func classifyName(name, cmd string) (kind, why string) {
	l := strings.ToLower(name)
	lc := strings.ToLower(cmd)
	switch {
	case regexp.MustCompile(`^(pre|post)?(install|prepare|publish|prepublish(only)?|pack|version|uninstall)$`).MatchString(l):
		return "", "lifecycle hook"
	case strings.Contains(l, "watch") || (strings.Contains(lc, "--watch") && !strings.Contains(lc, "--watch=false") && !strings.Contains(lc, "--watchall=false")) ||
		regexp.MustCompile(`^(dev|start|serve)(:|$)`).MatchString(l):
		return "", "long-running (watch or dev server)"
	}
	isCheck := strings.Contains(l, "check") || strings.Contains(l, "verify") || strings.Contains(lc, "--check") ||
		strings.Contains(lc, "--list-different") || strings.Contains(lc, "--dry-run") || strings.Contains(lc, "--verify")
	switch {
	case regexp.MustCompile(`fix|write|(^|[:_-])(format|fmt|prettier)([:_-]|$)`).MatchString(l) && !isCheck:
		return "", "mutates files (formats or fixes in place)"
	case strings.Contains(lc, "--fix") || strings.Contains(lc, "--write"):
		return "", "mutates files (formats or fixes in place)"
	}
	switch {
	case regexp.MustCompile(`e2e|playwright|cypress|puppeteer|selenium|end-to-end`).MatchString(l):
		return "e2e", ""
	case regexp.MustCompile(`(^|[:_-])(format|fmt|prettier)([:_-]|$)`).MatchString(l) && isCheck:
		return "format", ""
	case regexp.MustCompile(`^(typecheck|type-check|check-types|check:types|types:check|type:check|tsc|tsc:.*|typecheck[:_-].*|types|check_types)$`).MatchString(l) && !strings.Contains(lc, "emitdeclarationonly"),
		strings.Contains(lc, "tsc") && strings.Contains(lc, "--noemit"), strings.Contains(lc, "vue-tsc"), strings.Contains(lc, "svelte-check"), strings.Contains(lc, "astro check"):
		return "typecheck", ""
	case strings.Contains(l, "lint") || regexp.MustCompile(`^(vet|clippy)$`).MatchString(l) || strings.Contains(lc, "eslint") || strings.Contains(lc, "next lint") || strings.Contains(lc, "oxlint") || strings.Contains(lc, "biome check"):
		return "lint", ""
	case regexp.MustCompile(`integration|acceptance|smoke`).MatchString(l):
		return "test", ""
	case regexp.MustCompile(`^(test|tests|unit|spec|specs|jest|vitest|mocha|ava|pytest|ci:test|test[:_-].*|.*[:_-]test|unit[:_-].*)$`).MatchString(l),
		regexp.MustCompile(`\b(jest|vitest|mocha|pytest)\b|node (--[\w-]+ )*--test|node:test`).MatchString(lc):
		return "test", ""
	case regexp.MustCompile(`^(build|compile|bundle|build[:_-](prod|production|app|all|ci))$`).MatchString(l):
		return "build", ""
	case regexp.MustCompile(`^build[:_-]`).MatchString(l):
		return "build", ""
	case regexp.MustCompile(`^(check|ci|verify|validate|all|checks|qa|lint-all)$`).MatchString(l):
		return "other", ""
	}
	return "", "name and command say nothing about testing, linting, typechecking or building"
}

func (d *discoverer) finish(steps []ciStep) {
	// Task runners add nothing when a native loop of the same kind exists.
	native := map[string]string{}
	for _, l := range d.loops {
		if !l.runner && l.Enabled {
			native[l.Kind+"@"+l.Cwd] = l.ID
		}
	}
	for i := range d.loops {
		l := &d.loops[i]
		if l.runner && l.Enabled {
			if id, ok := native[l.Kind+"@"+l.Cwd]; ok {
				l.Enabled = false
				l.Note = strings.TrimSpace(l.Note + " Disabled by default: " + id + " already covers this kind; the target probably wraps it.")
			}
		}
	}
	// Needs from the CI jobs that run the same command.
	for i := range d.loops {
		l := &d.loops[i]
		for _, s := range steps {
			if !ciMatches(*l, s) {
				continue
			}
			l.InCI = true
			for _, sv := range s.Services {
				l.Needs = addNeed(l.Needs, serviceNeed(sv))
			}
			for _, e := range s.EnvNames {
				for _, n := range guessNeeds(e) {
					l.Needs = addNeed(l.Needs, n)
				}
			}
		}
		if hasServiceNeed(l.Needs) {
			l.Enabled = false
		}
	}
	d.applyExternalConfig()
	sort.SliceStable(d.loops, func(i, j int) bool { return kindOrder[d.loops[i].Kind] < kindOrder[d.loops[j].Kind] })
}

func serviceNeed(name string) string {
	n := strings.ToLower(name)
	switch {
	case regexp.MustCompile(`postgres|mysql|mariadb|mongo|sqlite|mssql|cockroach|neon`).MatchString(n):
		return "database"
	}
	return "network-service"
}

// ciMatches reports whether a CI step runs the same thing as a candidate.
func ciMatches(l loop, s ciStep) bool {
	if l.Kind == "install" {
		return s.Kind == "install" && l.pm != "" && strings.Contains(s.Cmd, l.pm)
	}
	if l.script != "" && l.pm != "" {
		re := regexp.MustCompile(`(?:^|[\s;&|])` + regexp.QuoteMeta(l.pm) + `\s+(?:run\s+|exec\s+)?(?:-{1,2}[\w-]+(?:[= ]\S+)?\s+)*` + regexp.QuoteMeta(l.script) + `(?:\s|$)`)
		if re.MatchString(s.Cmd) {
			return true
		}
		if (l.script == "test") && regexp.MustCompile(`\b`+regexp.QuoteMeta(l.pm)+`\s+(t|test)\b`).MatchString(s.Cmd) {
			return true
		}
		return false
	}
	if l.matchKey != "" {
		return strings.Contains(s.Cmd, l.matchKey)
	}
	return false
}

func (d *discoverer) ciSteps() []ciStep {
	var steps []ciStep
	for _, f := range d.files {
		low := strings.ToLower(f)
		switch {
		case strings.HasPrefix(f, ".github/workflows/") && (strings.HasSuffix(low, ".yml") || strings.HasSuffix(low, ".yaml")):
			steps = append(steps, parseGitHubWorkflow(f, d.read(f))...)
		case path.Base(f) == ".gitlab-ci.yml":
			steps = append(steps, parseGitLabCI(f, d.read(f))...)
		}
	}
	return steps
}

func (d *discoverer) collectSignals() {
	for _, f := range d.files {
		base := path.Base(f)
		switch {
		case regexp.MustCompile(`^(docker-)?compose([.-][\w.-]+)?\.ya?ml$`).MatchString(base):
			lines := strings.Split(d.read(f), "\n")
			in := false
			for _, ln := range lines {
				if strings.TrimRight(ln, " ") == "services:" {
					in = true
					continue
				}
				if in {
					if ind := indentOf(ln); ind == 0 && strings.TrimSpace(ln) != "" {
						in = false
					} else if m := reYamlKey.FindStringSubmatch(ln); m != nil && indentOf(ln) == 2 && m[2] == "" && strings.TrimSpace(m[4]) == "" {
						d.sig.ComposeServices = append(d.sig.ComposeServices, m[3])
					}
				}
			}
		case reEnvExample.MatchString(f):
			for _, ln := range strings.Split(d.read(f), "\n") {
				ln = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "export "))
				if i := strings.IndexByte(ln, '='); i > 0 && regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(ln[:i]) {
					d.sig.EnvExampleKeys = append(d.sig.EnvExampleKeys, ln[:i]) // names only, never values
				}
			}
		case base == ".nvmrc" || base == ".node-version":
			d.sig.RuntimeHints["node"] = strings.TrimSpace(d.read(f)) + " (" + f + ")"
		case base == ".python-version":
			d.sig.RuntimeHints["python"] = strings.TrimSpace(d.read(f)) + " (" + f + ")"
		case base == ".tool-versions" || base == "mise.toml" || base == ".mise.toml":
			d.sig.RuntimeHints["tool_versions"] = f
		case base == "rust-toolchain.toml" || base == "rust-toolchain":
			d.sig.RuntimeHints["rust"] = f
		case base == ".ruby-version":
			d.sig.RuntimeHints["ruby"] = strings.TrimSpace(d.read(f)) + " (" + f + ")"
		}
	}
	d.sig.ComposeServices = uniqueSorted(d.sig.ComposeServices)
	d.sig.EnvExampleKeys = uniqueSorted(d.sig.EnvExampleKeys)
	for _, s := range d.ciSteps() {
		d.sig.CIServices = append(d.sig.CIServices, s.Services...)
		d.sig.CIEnvNames = append(d.sig.CIEnvNames, s.EnvNames...)
	}
	d.sig.CIServices = uniqueSorted(d.sig.CIServices)
	d.sig.CIEnvNames = uniqueSorted(d.sig.CIEnvNames)
	if len(d.sig.PackageManagers) == 0 {
		d.sig.PackageManagers = nil
	}
	if len(d.sig.RuntimeHints) == 0 {
		d.sig.RuntimeHints = nil
	}
}

// ---------------------------------------------------------------- JavaScript

type pkgInfo struct {
	Name            string
	Scripts         map[string]string
	Deps            map[string]bool
	PackageManager  string
	Workspaces      []string
	EnginesNode     string
	HasWorkspaceKey bool
}

func (d *discoverer) parsePkg(rel string) *pkgInfo {
	var raw struct {
		Name            string          `json:"name"`
		Scripts         map[string]any  `json:"scripts"`
		Dependencies    map[string]any  `json:"dependencies"`
		DevDependencies map[string]any  `json:"devDependencies"`
		PackageManager  string          `json:"packageManager"`
		Workspaces      json.RawMessage `json:"workspaces"`
		Engines         map[string]any  `json:"engines"`
	}
	if json.Unmarshal([]byte(d.read(rel)), &raw) != nil {
		return nil
	}
	p := &pkgInfo{Name: raw.Name, Scripts: map[string]string{}, Deps: map[string]bool{}, PackageManager: raw.PackageManager}
	for k, v := range raw.Scripts {
		if s, ok := v.(string); ok {
			p.Scripts[k] = s
		}
	}
	for k := range raw.Dependencies {
		p.Deps[k] = true
	}
	for k := range raw.DevDependencies {
		p.Deps[k] = true
	}
	if s, ok := raw.Engines["node"].(string); ok {
		p.EnginesNode = s
	}
	if len(raw.Workspaces) > 0 {
		p.HasWorkspaceKey = true
		var arr []string
		if json.Unmarshal(raw.Workspaces, &arr) == nil {
			p.Workspaces = arr
		} else {
			var obj struct {
				Packages []string `json:"packages"`
			}
			if json.Unmarshal(raw.Workspaces, &obj) == nil {
				p.Workspaces = obj.Packages
			}
		}
	}
	return p
}

func globToRegexp(g string) *regexp.Regexp {
	g = strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(g, "./"), "/"), "/")
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch {
		case strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i++
		case g[i] == '*':
			b.WriteString("[^/]+")
		default:
			b.WriteString(regexp.QuoteMeta(string(g[i])))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func (d *discoverer) pnpmWorkspaceGlobs(dir string) []string {
	text := d.read(join(dir, "pnpm-workspace.yaml"))
	var globs []string
	in := false
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(ln, "packages:") {
			in = true
			continue
		}
		if in {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if !strings.HasPrefix(t, "-") {
				in = false
				continue
			}
			g := unquoteYAML(strings.TrimSpace(strings.TrimPrefix(t, "-")))
			if !strings.HasPrefix(g, "!") {
				globs = append(globs, g)
			}
		}
	}
	return globs
}

type pmInfo struct {
	Name         string
	Install      string
	Run          string // "<pm> run"
	Exec         string // prefix for running a binary
	LockfileUsed string
}

func (d *discoverer) detectPM(dir string, pkg *pkgInfo) pmInfo {
	has := func(n string) bool { return d.hasFile(join(dir, n)) || d.hasFile(n) }
	name, lock := "", ""
	switch {
	case has("pnpm-lock.yaml"):
		name, lock = "pnpm", "pnpm-lock.yaml"
	case has("yarn.lock"):
		name, lock = "yarn", "yarn.lock"
	case has("bun.lockb") || has("bun.lock"):
		name, lock = "bun", "bun.lock"
	case has("package-lock.json") || has("npm-shrinkwrap.json"):
		name, lock = "npm", "package-lock.json"
	}
	if name == "" && pkg != nil && pkg.PackageManager != "" {
		name = strings.SplitN(pkg.PackageManager, "@", 2)[0]
	}
	if name == "" {
		name = "npm"
	}
	pm := pmInfo{Name: name, LockfileUsed: lock}
	switch name {
	case "pnpm":
		pm.Install = "pnpm install"
		if lock != "" {
			pm.Install = "pnpm install --frozen-lockfile"
		}
		pm.Run, pm.Exec = "pnpm run", "pnpm exec"
	case "yarn":
		berry := d.hasFile(".yarnrc.yml") || (pkg != nil && regexp.MustCompile(`^yarn@[2-9]|^yarn@\d\d`).MatchString(pkg.PackageManager))
		switch {
		case berry:
			pm.Install = "yarn install --immutable"
		case lock != "":
			pm.Install = "yarn install --frozen-lockfile"
		default:
			pm.Install = "yarn install"
		}
		pm.Run, pm.Exec = "yarn run", "yarn"
	case "bun":
		pm.Install = "bun install"
		if lock != "" {
			pm.Install = "bun install --frozen-lockfile"
		}
		pm.Run, pm.Exec = "bun run", "bunx"
	default:
		pm.Install = "npm install"
		if lock != "" {
			pm.Install = "npm ci"
		}
		pm.Run, pm.Exec = "npm run", "npm exec --"
	}
	return pm
}

func (d *discoverer) jsProjects() {
	var pkgDirs []string
	for _, f := range d.files {
		if path.Base(f) == "package.json" && !strings.Contains(f, "/fixtures/") && !strings.Contains(f, "testdata/") {
			pkgDirs = append(pkgDirs, path.Dir(f))
		}
	}
	sort.Strings(pkgDirs)
	if len(pkgDirs) == 0 {
		return
	}
	pkgs := map[string]*pkgInfo{}
	for _, dir := range pkgDirs {
		if p := d.parsePkg(join(dir, "package.json")); p != nil {
			pkgs[dir] = p
		}
	}
	var roots []string
	for _, dir := range pkgDirs {
		if pkgs[dir] == nil {
			continue
		}
		nested := false
		for _, r := range roots {
			if r == "." || strings.HasPrefix(dir, r+"/") {
				nested = true
			}
		}
		if !nested {
			roots = append(roots, dir)
		}
	}
	roots = d.capDirs("javascript", roots, maxProjects)
	done := map[string]bool{}
	queue := append([]string(nil), roots...)
	nested := map[string]bool{}
	for len(queue) > 0 {
		root := queue[0]
		queue = queue[1:]
		if done[root] {
			continue
		}
		done[root] = true
		if nested[root] && !d.hasCandidateScripts(pkgs[root]) {
			d.skip(join(root, "package.json"), pkgs[root].Name, "nested package.json that is not a workspace member and has no test, lint, typecheck or build script")
			continue
		}
		for _, dir := range d.jsRoot(root, pkgs, pkgDirs) {
			nested[dir] = true
			queue = append(queue, dir)
		}
	}
}

func (d *discoverer) hasCandidateScripts(p *pkgInfo) bool {
	for n, cmd := range p.Scripts {
		if k, _ := classifyName(n, cmd); k != "" {
			return true
		}
	}
	return false
}

// jsRoot adds the install loop and the script loops of one package root and its
// workspace members. It returns the packages below it that are not members:
// separate projects with their own install.
func (d *discoverer) jsRoot(root string, pkgs map[string]*pkgInfo, pkgDirs []string) (notMembers []string) {
	rootPkg := pkgs[root]
	pm := d.detectPM(root, rootPkg)
	d.sig.PackageManagers[root] = pm.Name
	if rootPkg.EnginesNode != "" {
		d.sig.RuntimeHints["node_engines"] = rootPkg.EnginesNode
	}
	installID := idFor("js", root, "install")
	d.add(loop{ID: installID, Kind: "install", Cmd: pm.Install, Cwd: root, Source: join(root, "package.json"), Ecosystem: "javascript", pm: pm.Name,
		Note: "installs the whole workspace; lifecycle scripts such as postinstall run"})
	auditCmd := pm.Name + " audit --json"
	always := true
	switch pm.Name {
	case "yarn":
		if strings.HasPrefix(pm.Install, "yarn install --immutable") {
			auditCmd = "yarn npm audit --json"
		}
	case "bun":
		always = false
	}
	d.addAudit(loop{ID: idFor("js", root, "audit"), Cmd: auditCmd, Cwd: root, Source: join(root, "package.json"), Ecosystem: "javascript", pm: pm.Name, matchKey: pm.Name + " audit"}, pm.Name, always)
	globs := append(append([]string(nil), rootPkg.Workspaces...), d.pnpmWorkspaceGlobs(root)...)
	var members []string
	for _, dir := range pkgDirs {
		if dir == root || pkgs[dir] == nil || !(root == "." || strings.HasPrefix(dir, root+"/")) {
			continue
		}
		rel := strings.TrimPrefix(dir, root+"/")
		if root == "." {
			rel = dir
		}
		ok := false
		for _, g := range globs {
			if globToRegexp(g).MatchString(rel) {
				ok = true
			}
		}
		if ok {
			members = append(members, dir)
		} else {
			notMembers = append(notMembers, dir)
		}
	}
	d.sig.WorkspaceMembers += len(members)
	members = d.capDirs("javascript workspace", members, maxProjects)
	for _, dir := range append([]string{root}, members...) {
		d.jsScripts(dir, pkgs[dir], pm, installID)
	}
	return notMembers
}

func (d *discoverer) jsScripts(dir string, p *pkgInfo, pm pmInfo, installID string) {
	src := join(dir, "package.json")
	var names []string
	for n := range p.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	haveTypecheck := false
	for _, n := range names {
		cmd := p.Scripts[n]
		// pre<name>/post<name> hooks run on their own.
		if (strings.HasPrefix(n, "pre") || strings.HasPrefix(n, "post")) && p.Scripts[strings.TrimPrefix(strings.TrimPrefix(n, "pre"), "post")] != "" {
			d.skip(src+" scripts."+n, n, "runs automatically around another script")
			continue
		}
		kind, why := classifyName(n, cmd)
		if kind == "" {
			if why != "name and command say nothing about testing, linting, typechecking or building" {
				d.skip(src+" scripts."+n, n, why)
			}
			continue
		}
		if kind == "typecheck" {
			haveTypecheck = true
		}
		l := loop{ID: idFor("js", dir, n), Kind: kind, Cmd: pm.Run + " " + n, Cwd: dir, Source: src + ":" + fmt.Sprint(d.scriptLine(src, n)),
			Ecosystem: "javascript", Requires: []string{idFor("js", pm.rootOf(dir, d), "install")}, script: n, pm: pm.Name,
			text: resolveScript(p.Scripts, n)}
		if kind == "other" {
			l.Note = "aggregate script; probably runs other loops"
		}
		if regexp.MustCompile(`integration|acceptance|smoke`).MatchString(strings.ToLower(n)) {
			l.Needs = addNeed(l.Needs, "network-service")
			l.Note = "integration-style tests usually need running services"
		}
		d.add(l)
	}
	if !haveTypecheck && (p.Deps["typescript"]) && d.hasFile(join(dir, "tsconfig.json")) {
		d.add(loop{ID: idFor("js", dir, "tsc-noemit"), Kind: "typecheck", Cmd: pm.Exec + " tsc --noEmit", Cwd: dir, Source: join(dir, "tsconfig.json"),
			Ecosystem: "javascript", Requires: []string{idFor("js", pm.rootOf(dir, d), "install")}, pm: pm.Name, matchKey: "tsc --noEmit",
			Note: "no typecheck script; typescript is a dependency"})
	}
}

// rootOf returns the root directory whose install covers dir.
func (pm pmInfo) rootOf(dir string, d *discoverer) string {
	for dir != "." && dir != "" {
		if _, ok := d.sig.PackageManagers[dir]; ok {
			return dir
		}
		dir = path.Dir(dir)
	}
	return "."
}

func (d *discoverer) scriptLine(rel, name string) int {
	text := d.read(rel)
	i := strings.Index(text, `"`+name+`"`)
	if i < 0 {
		return 1
	}
	if j := strings.Index(text, `"scripts"`); j >= 0 && j < i {
		return lineOf(text, i)
	}
	if j := strings.Index(text, `"scripts"`); j >= 0 {
		if k := strings.Index(text[j:], `"`+name+`"`); k >= 0 {
			return lineOf(text, j+k)
		}
	}
	return lineOf(text, i)
}

// ------------------------------------------------------------------- Python

func (d *discoverer) pythonProjects() {
	dirs := d.dirsWith(func(b string) bool {
		return b == "pyproject.toml" || b == "setup.py" || b == "setup.cfg" || b == "Pipfile" || b == "tox.ini" || b == "noxfile.py" ||
			(strings.HasPrefix(b, "requirements") && strings.HasSuffix(b, ".txt"))
	}, 3, false)
	dirs = d.capDirs("python", dirs, maxProjects)
	for _, dir := range dirs {
		pyproject := d.read(join(dir, "pyproject.toml"))
		setupcfg := d.read(join(dir, "setup.cfg"))
		tox := d.read(join(dir, "tox.ini"))
		var reqFiles []string
		for _, f := range d.files {
			if path.Dir(f) == dir && strings.HasPrefix(path.Base(f), "requirements") && strings.HasSuffix(f, ".txt") {
				reqFiles = append(reqFiles, path.Base(f))
			}
		}
		blob := strings.ToLower(pyproject + "\n" + setupcfg + "\n" + tox)
		for _, r := range reqFiles {
			blob += "\n" + strings.ToLower(d.read(join(dir, r)))
		}
		has := func(n string) bool { return d.hasFile(join(dir, n)) }
		hasPyproject := has("pyproject.toml")
		var install, runner, installNote string
		pm := ""
		switch {
		case has("uv.lock"):
			install, runner, pm = "uv sync", "uv run", "uv"
		case has("poetry.lock") || strings.Contains(pyproject, "[tool.poetry]"):
			install, runner, pm = "poetry install", "poetry run", "poetry"
		case has("Pipfile"):
			install, runner, pm = "pipenv install --dev", "pipenv run", "pipenv"
		default:
			args := ""
			if hasPyproject || has("setup.py") || has("setup.cfg") {
				args = "-e ."
				if strings.Contains(pyproject, "[project.optional-dependencies]") || strings.Contains(pyproject, "[dependency-groups]") {
					installNote = "optional dependencies or dependency groups exist; the dev extras may need adding to this install"
				}
			}
			for _, r := range reqFiles {
				args += " -r " + r
			}
			args = strings.TrimSpace(args)
			if args == "" {
				continue
			}
			install = "python3 -m venv .venv && .venv/bin/python -m pip install -q " + args
			runner, pm = ".venv/bin/python -m", "pip"
		}
		d.sig.PackageManagers[join(dir, "(python)")] = pm
		installID := idFor("py", dir, "install")
		d.add(loop{ID: installID, Kind: "install", Cmd: install, Cwd: dir, Source: firstExisting(d, dir, "pyproject.toml", "Pipfile", "setup.py", "setup.cfg", "requirements.txt"), Ecosystem: "python", pm: pm, Note: installNote})
		auditArgs := "-f json"
		if len(reqFiles) > 0 {
			auditArgs = "-r " + reqFiles[0] + " -f json"
		}
		d.addAudit(loop{ID: idFor("py", dir, "pip-audit"), Cmd: "pip-audit " + auditArgs, Cwd: dir, Source: firstExisting(d, dir, "pyproject.toml", "requirements.txt", "setup.py"), Ecosystem: "python", matchKey: "pip-audit"}, "pip-audit", false)
		tool := func(name, args string) string {
			if pm == "pip" {
				return runner + " " + name + " " + args
			}
			return strings.TrimSpace(runner + " " + name + " " + args)
		}
		src := firstExisting(d, dir, "pyproject.toml", "setup.cfg", "tox.ini", "requirements.txt")
		reqs := []string{installID}
		if strings.Contains(blob, "pytest") || has("pytest.ini") || has("conftest.py") {
			d.add(loop{ID: idFor("py", dir, "pytest"), Kind: "test", Cmd: tool("pytest", "-q"), Cwd: dir, Source: src, Ecosystem: "python", Requires: reqs, matchKey: "pytest"})
		}
		if strings.Contains(pyproject, "[tool.ruff") || has("ruff.toml") || has(".ruff.toml") || strings.Contains(blob, "ruff") {
			d.add(loop{ID: idFor("py", dir, "ruff"), Kind: "lint", Cmd: tool("ruff", "check ."), Cwd: dir, Source: src, Ecosystem: "python", Requires: reqs, matchKey: "ruff check"})
		}
		if has(".flake8") || strings.Contains(setupcfg, "[flake8]") || strings.Contains(tox, "[flake8]") {
			d.add(loop{ID: idFor("py", dir, "flake8"), Kind: "lint", Cmd: tool("flake8", ""), Cwd: dir, Source: src, Ecosystem: "python", Requires: reqs, matchKey: "flake8"})
		}
		if strings.Contains(pyproject, "[tool.mypy]") || has("mypy.ini") || has(".mypy.ini") || strings.Contains(setupcfg, "[mypy]") || strings.Contains(blob, "mypy") {
			d.add(loop{ID: idFor("py", dir, "mypy"), Kind: "typecheck", Cmd: tool("mypy", "."), Cwd: dir, Source: src, Ecosystem: "python", Requires: reqs, matchKey: "mypy"})
		} else if has("pyrightconfig.json") || strings.Contains(pyproject, "[tool.pyright]") {
			d.add(loop{ID: idFor("py", dir, "pyright"), Kind: "typecheck", Cmd: tool("pyright", ""), Cwd: dir, Source: src, Ecosystem: "python", Requires: reqs, matchKey: "pyright"})
		}
		if strings.Contains(pyproject, "[tool.black]") {
			d.add(loop{ID: idFor("py", dir, "black-check"), Kind: "format", Cmd: tool("black", "--check ."), Cwd: dir, Source: src, Ecosystem: "python", Requires: reqs, matchKey: "black --check"})
		}
		if has("tox.ini") || strings.Contains(pyproject, "[tool.tox]") {
			d.add(loop{ID: idFor("py", dir, "tox"), Kind: "test", Cmd: tool("tox", ""), Cwd: dir, Source: src, Ecosystem: "python", Requires: reqs, matchKey: "tox",
				Note: "builds its own environments; usually duplicates the pytest loop"})
			d.disableLast("heavy: tox builds its own environments")
		}
		if has("noxfile.py") {
			d.add(loop{ID: idFor("py", dir, "nox"), Kind: "test", Cmd: tool("nox", ""), Cwd: dir, Source: join(dir, "noxfile.py"), Ecosystem: "python", Requires: reqs, matchKey: "nox"})
			d.disableLast("heavy: nox builds its own environments")
		}
	}
}

func (d *discoverer) disableLast(note string) {
	l := &d.loops[len(d.loops)-1]
	l.Enabled = false
	if note != "" && !strings.Contains(l.Note, note) {
		l.Note = strings.TrimSpace(l.Note + " " + note)
	}
}

func firstExisting(d *discoverer, dir string, names ...string) string {
	for _, n := range names {
		if d.hasFile(join(dir, n)) {
			return join(dir, n)
		}
	}
	return dir
}

// ------------------------------------------------------------------------ Go

func (d *discoverer) goProjects() {
	dirs := d.dirsWith(func(b string) bool { return b == "go.mod" }, 3, true)
	dirs = d.capDirs("go", dirs, maxProjects)
	var mods []string
	for _, dir := range dirs {
		mods = append(mods, dir)
	}
	for _, dir := range mods {
		src := join(dir, "go.mod")
		needs := []string{}
		if strings.Contains(d.read(src), "testcontainers") {
			needs = []string{"docker"}
		}
		installID := idFor("go", dir, "mod-download")
		d.add(loop{ID: installID, Kind: "install", Cmd: "go mod download", Cwd: dir, Source: src, Ecosystem: "go", pm: "go"})
		req := []string{installID}
		d.add(loop{ID: idFor("go", dir, "build"), Kind: "build", Cmd: "go build ./...", Cwd: dir, Source: src, Ecosystem: "go", Requires: req, matchKey: "go build"})
		d.add(loop{ID: idFor("go", dir, "vet"), Kind: "lint", Cmd: "go vet ./...", Cwd: dir, Source: src, Ecosystem: "go", Requires: req, matchKey: "go vet"})
		d.add(loop{ID: idFor("go", dir, "test"), Kind: "test", Cmd: "go test -count=1 -v ./...", Cwd: dir, Source: src, Ecosystem: "go", Requires: req, matchKey: "go test", Needs: needs,
			Note: "-v so the individual tests can be counted"})
		d.addAudit(loop{ID: idFor("go", dir, "govulncheck"), Cmd: "govulncheck ./...", Cwd: dir, Source: src, Ecosystem: "go", matchKey: "govulncheck"}, "govulncheck", false)
		for _, cfg := range []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"} {
			if d.hasFile(join(dir, cfg)) {
				if _, err := exec.LookPath("golangci-lint"); err == nil {
					d.add(loop{ID: idFor("go", dir, "golangci-lint"), Kind: "lint", Cmd: "golangci-lint run ./...", Cwd: dir, Source: join(dir, cfg), Ecosystem: "go", Requires: req, matchKey: "golangci-lint"})
				} else {
					d.skip(join(dir, cfg), "golangci-lint", "configured, but golangci-lint is not installed on this machine")
				}
				break
			}
		}
	}
}

// ---------------------------------------------------------------------- Rust

func (d *discoverer) rustProjects() {
	dirs := d.dirsWith(func(b string) bool { return b == "Cargo.toml" }, 3, false)
	dirs = d.capDirs("rust", dirs, maxProjects)
	for _, dir := range dirs {
		src := join(dir, "Cargo.toml")
		installID := idFor("rust", dir, "fetch")
		d.add(loop{ID: installID, Kind: "install", Cmd: "cargo fetch", Cwd: dir, Source: src, Ecosystem: "rust", pm: "cargo"})
		req := []string{installID}
		d.add(loop{ID: idFor("rust", dir, "build"), Kind: "build", Cmd: "cargo build", Cwd: dir, Source: src, Ecosystem: "rust", Requires: req, matchKey: "cargo build"})
		d.add(loop{ID: idFor("rust", dir, "clippy"), Kind: "lint", Cmd: "cargo clippy", Cwd: dir, Source: src, Ecosystem: "rust", Requires: req, matchKey: "cargo clippy"})
		d.add(loop{ID: idFor("rust", dir, "test"), Kind: "test", Cmd: "cargo test --no-fail-fast", Cwd: dir, Source: src, Ecosystem: "rust", Requires: req, matchKey: "cargo test"})
		d.addAudit(loop{ID: idFor("rust", dir, "audit"), Cmd: "cargo audit", Cwd: dir, Source: src, Ecosystem: "rust", matchKey: "cargo audit"}, "cargo-audit", false)
		d.add(loop{ID: idFor("rust", dir, "fmt-check"), Kind: "format", Cmd: "cargo fmt --check", Cwd: dir, Source: src, Ecosystem: "rust", Requires: req, matchKey: "cargo fmt"})
	}
}

// ----------------------------------------------------------------------- JVM

func (d *discoverer) gradleProjects() {
	dirs := d.dirsWith(func(b string) bool {
		return b == "settings.gradle" || b == "settings.gradle.kts" || b == "build.gradle" || b == "build.gradle.kts"
	}, 2, false)
	dirs = d.capDirs("gradle", dirs, maxProjects)
	for _, dir := range dirs {
		g := "gradle"
		if d.hasFile(join(dir, "gradlew")) {
			g = "./gradlew"
		}
		src := firstExisting(d, dir, "settings.gradle.kts", "settings.gradle", "build.gradle.kts", "build.gradle")
		blob := d.read(join(dir, "build.gradle")) + d.read(join(dir, "build.gradle.kts"))
		d.add(loop{ID: idFor("gradle", dir, "build"), Kind: "build", Cmd: g + " build -x test --console=plain", Cwd: dir, Source: src, Ecosystem: "jvm", matchKey: "gradle"})
		d.add(loop{ID: idFor("gradle", dir, "test"), Kind: "test", Cmd: g + " test --continue --console=plain", Cwd: dir, Source: src, Ecosystem: "jvm", matchKey: "gradle test"})
		for _, lt := range []struct{ plugin, task string }{{"spotless", "spotlessCheck"}, {"checkstyle", "checkstyleMain"}, {"detekt", "detekt"}, {"ktlint", "ktlintCheck"}} {
			if strings.Contains(blob, lt.plugin) {
				d.add(loop{ID: idFor("gradle", dir, lt.task), Kind: "lint", Cmd: g + " " + lt.task + " --console=plain", Cwd: dir, Source: src, Ecosystem: "jvm", matchKey: lt.task})
			}
		}
	}
}

func (d *discoverer) mavenProjects() {
	dirs := d.dirsWith(func(b string) bool { return b == "pom.xml" }, 2, false)
	dirs = d.capDirs("maven", dirs, maxProjects)
	for _, dir := range dirs {
		m := "mvn"
		if d.hasFile(join(dir, "mvnw")) {
			m = "./mvnw"
		}
		src := join(dir, "pom.xml")
		pom := d.read(src)
		d.add(loop{ID: idFor("maven", dir, "package"), Kind: "build", Cmd: m + " -B -DskipTests package", Cwd: dir, Source: src, Ecosystem: "jvm", matchKey: "mvn"})
		d.add(loop{ID: idFor("maven", dir, "test"), Kind: "test", Cmd: m + " -B test", Cwd: dir, Source: src, Ecosystem: "jvm", matchKey: " test"})
		if strings.Contains(pom, "maven-checkstyle-plugin") {
			d.add(loop{ID: idFor("maven", dir, "checkstyle"), Kind: "lint", Cmd: m + " -B checkstyle:check", Cwd: dir, Source: src, Ecosystem: "jvm", matchKey: "checkstyle"})
		}
		if strings.Contains(pom, "spotless-maven-plugin") {
			d.add(loop{ID: idFor("maven", dir, "spotless"), Kind: "lint", Cmd: m + " -B spotless:check", Cwd: dir, Source: src, Ecosystem: "jvm", matchKey: "spotless"})
		}
	}
}

// --------------------------------------------------------------------- .NET

func (d *discoverer) dotnetProjects() {
	dirs := d.dirsWith(func(b string) bool {
		return strings.HasSuffix(b, ".sln") || strings.HasSuffix(b, ".slnx")
	}, 2, false)
	target := map[string]string{}
	for _, dir := range dirs {
		for _, f := range d.files {
			if path.Dir(f) == dir && (strings.HasSuffix(f, ".sln") || strings.HasSuffix(f, ".slnx")) {
				target[dir] = path.Base(f)
				break
			}
		}
	}
	if len(dirs) == 0 {
		dirs = d.dirsWith(func(b string) bool {
			return strings.HasSuffix(b, ".csproj") || strings.HasSuffix(b, ".fsproj")
		}, 3, false)
		for _, dir := range dirs {
			for _, f := range d.files {
				if path.Dir(f) == dir && (strings.HasSuffix(f, ".csproj") || strings.HasSuffix(f, ".fsproj")) {
					target[dir] = path.Base(f)
					break
				}
			}
		}
	}
	dirs = d.capDirs("dotnet", dirs, maxProjects)
	for _, dir := range dirs {
		t := target[dir]
		src := join(dir, t)
		installID := idFor("dotnet", dir, "restore")
		d.add(loop{ID: installID, Kind: "install", Cmd: "dotnet restore " + t, Cwd: dir, Source: src, Ecosystem: "dotnet", pm: "dotnet"})
		req := []string{installID}
		d.add(loop{ID: idFor("dotnet", dir, "build"), Kind: "build", Cmd: "dotnet build --no-restore " + t, Cwd: dir, Source: src, Ecosystem: "dotnet", Requires: req, matchKey: "dotnet build"})
		d.add(loop{ID: idFor("dotnet", dir, "test"), Kind: "test", Cmd: "dotnet test --no-restore " + t, Cwd: dir, Source: src, Ecosystem: "dotnet", Requires: req, matchKey: "dotnet test"})
		d.add(loop{ID: idFor("dotnet", dir, "format"), Kind: "format", Cmd: "dotnet format --verify-no-changes " + t, Cwd: dir, Source: src, Ecosystem: "dotnet", Requires: req, matchKey: "dotnet format"})
	}
}

// ---------------------------------------------------------------------- Ruby

func (d *discoverer) rubyProjects() {
	dirs := d.dirsWith(func(b string) bool { return b == "Gemfile" }, 2, false)
	dirs = d.capDirs("ruby", dirs, maxProjects)
	for _, dir := range dirs {
		src := join(dir, "Gemfile")
		gems := d.read(src) + d.read(join(dir, "Gemfile.lock"))
		installID := idFor("ruby", dir, "bundle-install")
		d.add(loop{ID: installID, Kind: "install", Cmd: "bundle install", Cwd: dir, Source: src, Ecosystem: "ruby", pm: "bundle"})
		req := []string{installID}
		var needs []string
		if db := strings.ToLower(d.read(join(dir, "config/database.yml"))); strings.Contains(db, "postgres") || strings.Contains(db, "mysql") {
			needs = []string{"database"}
		}
		hasSpecDir := false
		for _, f := range d.files {
			if strings.HasPrefix(f, join(dir, "spec/")) {
				hasSpecDir = true
				break
			}
		}
		switch {
		case d.hasFile(join(dir, ".rspec")) || strings.Contains(gems, "rspec") || hasSpecDir:
			d.add(loop{ID: idFor("ruby", dir, "rspec"), Kind: "test", Cmd: "bundle exec rspec", Cwd: dir, Source: src, Ecosystem: "ruby", Requires: req, matchKey: "rspec", Needs: needs})
		case d.hasFile(join(dir, "Rakefile")):
			d.add(loop{ID: idFor("ruby", dir, "rake-test"), Kind: "test", Cmd: "bundle exec rake test", Cwd: dir, Source: join(dir, "Rakefile"), Ecosystem: "ruby", Requires: req, matchKey: "rake test", Needs: needs})
		}
		d.addAudit(loop{ID: idFor("ruby", dir, "bundle-audit"), Cmd: "bundle audit check", Cwd: dir, Source: src, Ecosystem: "ruby", matchKey: "bundle audit"}, "bundle-audit", false)
		if d.hasFile(join(dir, ".rubocop.yml")) || strings.Contains(gems, "rubocop") {
			d.add(loop{ID: idFor("ruby", dir, "rubocop"), Kind: "lint", Cmd: "bundle exec rubocop", Cwd: dir, Source: src, Ecosystem: "ruby", Requires: req, matchKey: "rubocop"})
		}
	}
}

// ----------------------------------------------------------------------- PHP

func (d *discoverer) phpProjects() {
	dirs := d.dirsWith(func(b string) bool { return b == "composer.json" }, 2, false)
	dirs = d.capDirs("php", dirs, maxProjects)
	for _, dir := range dirs {
		src := join(dir, "composer.json")
		text := d.read(src)
		installID := idFor("php", dir, "composer-install")
		d.add(loop{ID: installID, Kind: "install", Cmd: "composer install --no-interaction --prefer-dist", Cwd: dir, Source: src, Ecosystem: "php", pm: "composer"})
		req := []string{installID}
		if d.hasFile(join(dir, "phpunit.xml")) || d.hasFile(join(dir, "phpunit.xml.dist")) || strings.Contains(text, "phpunit/phpunit") {
			d.add(loop{ID: idFor("php", dir, "phpunit"), Kind: "test", Cmd: "vendor/bin/phpunit", Cwd: dir, Source: src, Ecosystem: "php", Requires: req, matchKey: "phpunit"})
		}
		if d.hasFile(join(dir, "phpstan.neon")) || d.hasFile(join(dir, "phpstan.neon.dist")) || strings.Contains(text, "phpstan/phpstan") {
			d.add(loop{ID: idFor("php", dir, "phpstan"), Kind: "typecheck", Cmd: "vendor/bin/phpstan analyse --no-progress", Cwd: dir, Source: src, Ecosystem: "php", Requires: req, matchKey: "phpstan"})
		}
		if d.hasFile(join(dir, "psalm.xml")) || strings.Contains(text, "vimeo/psalm") {
			d.add(loop{ID: idFor("php", dir, "psalm"), Kind: "typecheck", Cmd: "vendor/bin/psalm --no-progress", Cwd: dir, Source: src, Ecosystem: "php", Requires: req, matchKey: "psalm"})
		}
		if strings.Contains(text, "friendsofphp/php-cs-fixer") {
			d.add(loop{ID: idFor("php", dir, "php-cs-fixer"), Kind: "format", Cmd: "vendor/bin/php-cs-fixer fix --dry-run --diff", Cwd: dir, Source: src, Ecosystem: "php", Requires: req, matchKey: "php-cs-fixer"})
		}
		if strings.Contains(text, "squizlabs/php_codesniffer") {
			d.add(loop{ID: idFor("php", dir, "phpcs"), Kind: "lint", Cmd: "vendor/bin/phpcs", Cwd: dir, Source: src, Ecosystem: "php", Requires: req, matchKey: "phpcs"})
		}
	}
}

// -------------------------------------------------------------- task runners

var (
	reMakeTarget = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*)\s*:(?:[^=]|$)`)
	reJustRecipe = regexp.MustCompile(`^@?([A-Za-z_][A-Za-z0-9_-]*)\s*:(?:[^=]|$)`)
	reTaskKey    = regexp.MustCompile(`^  ([A-Za-z0-9_:.-]+):\s*$`)
)

func (d *discoverer) taskRunners() {
	for _, f := range d.files {
		if strings.Count(f, "/") > 1 {
			continue
		}
		dir := path.Dir(f)
		base := path.Base(f)
		lines := strings.Split(d.read(f), "\n")
		switch {
		case base == "Makefile" || base == "makefile" || base == "GNUmakefile":
			d.runnerTargets(f, dir, "make", "make ", lines, func(l string) string {
				if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, " ") {
					return ""
				}
				if m := reMakeTarget.FindStringSubmatch(l); m != nil && !strings.HasPrefix(m[1], ".") {
					return m[1]
				}
				return ""
			})
		case base == "justfile" || base == "Justfile" || base == ".justfile":
			d.runnerTargets(f, dir, "just", "just ", lines, func(l string) string {
				if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
					return ""
				}
				if m := reJustRecipe.FindStringSubmatch(l); m != nil {
					return m[1]
				}
				return ""
			})
		case base == "Taskfile.yml" || base == "Taskfile.yaml":
			in := false
			d.runnerTargets(f, dir, "task", "task ", lines, func(l string) string {
				if strings.HasPrefix(l, "tasks:") {
					in = true
					return ""
				}
				if in && indentOf(l) == 0 && strings.TrimSpace(l) != "" {
					in = false
				}
				if m := reTaskKey.FindStringSubmatch(l); in && m != nil {
					return m[1]
				}
				return ""
			})
		}
	}
}

func (d *discoverer) runnerTargets(file, dir, tool, prefix string, lines []string, nameOf func(string) string) {
	for i, ln := range lines {
		name := nameOf(ln)
		if name == "" {
			continue
		}
		var body []string
		for j := i + 1; j < len(lines) && j < i+12; j++ {
			if lines[j] != "" && !strings.HasPrefix(lines[j], " ") && !strings.HasPrefix(lines[j], "\t") {
				break
			}
			body = append(body, lines[j])
		}
		text := strings.Join(body, "\n")
		kind, why := classifyName(name, "")
		if kind == "" {
			if why != "name and command say nothing about testing, linting, typechecking or building" {
				d.skip(fmt.Sprintf("%s:%d", file, i+1), name, why)
			}
			continue
		}
		l := loop{ID: idFor(tool, dir, name), Kind: kind, Cmd: prefix + name, Cwd: dir, Source: fmt.Sprintf("%s:%d", file, i+1),
			Ecosystem: tool, text: text, runner: true, matchKey: prefix + name,
			Note: "task runner target; it may need tools or services its recipe starts"}
		d.add(l)
	}
}
