package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type secretScan struct {
	Tool        string         `json:"tool,omitempty"`
	Unavailable string         `json:"unavailable,omitempty"`
	Findings    *int           `json:"findings,omitempty"`
	ByRule      map[string]int `json:"by_rule,omitempty"`
	Locations   []string       `json:"locations,omitempty"`
	Note        string         `json:"note,omitempty"`
}

type hygieneSection struct {
	CI                map[string][]string `json:"ci"`
	CIKindsRun        []string            `json:"ci_kinds_run"`
	Hooks             map[string][]string `json:"hooks"`
	Codeowners        []string            `json:"codeowners"`
	Containers        map[string][]string `json:"containers"`
	Devcontainer      []string            `json:"devcontainer"`
	EnvExamples       []string            `json:"env_examples"`
	TrackedEnvFiles   []string            `json:"tracked_env_files"`
	TrackedSecretLike []string            `json:"tracked_secret_like_files,omitempty"`
	Symlinks          []string            `json:"symlinks,omitempty"`
	IaC               map[string][]string `json:"iac"`
	Tests             testCounts          `json:"test_files"`
	Todos             todoCounts          `json:"todo_markers"`
	SecretScan        secretScan          `json:"secret_scan"`
	Notes             []string            `json:"notes,omitempty"`
}

var (
	reEnvExample = regexp.MustCompile(`(?i)(^|/)(\.?env\.(example|sample|template|dist|defaults?)|env\.example|[^/]*\.env\.(example|sample|template)|\.env\.[a-z]+\.(example|sample|template))$`)
	reEnvReal    = regexp.MustCompile(`(^|/)\.env(\.[A-Za-z0-9_-]+)?$`)
	reIaCDir     = regexp.MustCompile(`(^|/)(terraform|terragrunt|ansible|pulumi|cdk|cloudformation|helm|charts|k8s|kubernetes|infra|infrastructure)/`)
)

func appendTo(m map[string][]string, key, p string) {
	m[key] = append(m[key], p)
}

// scanHygiene looks for the files that show how a project is run and checked.
// files are root-relative paths; sccFiles feed the TODO count.
func scanHygiene(root string, files []string, scc []sccFile) hygieneSection {
	h := hygieneSection{
		CI: map[string][]string{}, Hooks: map[string][]string{}, Containers: map[string][]string{}, IaC: map[string][]string{},
		Codeowners: []string{}, Devcontainer: []string{}, EnvExamples: []string{}, TrackedEnvFiles: []string{},
	}
	for _, f := range files {
		if isVendoredPath(f) {
			continue
		}
		base := path.Base(f)
		low := strings.ToLower(f)
		switch {
		case strings.HasPrefix(f, ".github/workflows/") && (strings.HasSuffix(low, ".yml") || strings.HasSuffix(low, ".yaml")):
			appendTo(h.CI, "github_actions", f)
		case strings.HasPrefix(f, ".github/actions/") && (strings.HasSuffix(low, "action.yml") || strings.HasSuffix(low, "action.yaml")):
			appendTo(h.CI, "github_actions", f)
		case base == ".gitlab-ci.yml" || strings.HasPrefix(f, ".gitlab/ci/"):
			appendTo(h.CI, "gitlab", f)
		case strings.HasPrefix(f, ".circleci/") && strings.HasSuffix(low, ".yml"):
			appendTo(h.CI, "circleci", f)
		case base == "Jenkinsfile" || strings.HasPrefix(base, "Jenkinsfile."):
			appendTo(h.CI, "jenkins", f)
		case strings.HasPrefix(f, ".buildkite/") || base == "buildkite.yml":
			appendTo(h.CI, "buildkite", f)
		case base == "azure-pipelines.yml" || strings.HasPrefix(f, ".azure-pipelines/"):
			appendTo(h.CI, "azure_pipelines", f)
		case base == "bitbucket-pipelines.yml":
			appendTo(h.CI, "bitbucket", f)
		case base == ".travis.yml" || base == "appveyor.yml" || base == ".drone.yml" || base == "cloudbuild.yaml" || base == "cloudbuild.yml":
			appendTo(h.CI, "other", f)
		}
		switch {
		case base == ".pre-commit-config.yaml":
			appendTo(h.Hooks, "pre_commit", f)
		case strings.HasPrefix(f, ".husky/") && !strings.HasPrefix(f, ".husky/_/"):
			appendTo(h.Hooks, "husky", f)
		case base == "lefthook.yml" || base == ".lefthook.yml" || base == "lefthook.yaml" || base == "lefthook.json":
			appendTo(h.Hooks, "lefthook", f)
		case strings.HasPrefix(base, ".lintstagedrc") || strings.HasPrefix(base, "lint-staged.config"):
			appendTo(h.Hooks, "lint_staged", f)
		case strings.HasPrefix(f, ".githooks/") || strings.HasPrefix(f, ".git-hooks/"):
			appendTo(h.Hooks, "git_hooks_dir", f)
		case base == ".simple-git-hooks.json":
			appendTo(h.Hooks, "simple_git_hooks", f)
		}
		if base == "CODEOWNERS" {
			h.Codeowners = append(h.Codeowners, f)
		}
		switch {
		case base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".Dockerfile"):
			appendTo(h.Containers, "dockerfile", f)
		case regexp.MustCompile(`^(docker-)?compose([.-][\w.-]+)?\.ya?ml$`).MatchString(base):
			appendTo(h.Containers, "compose", f)
		case base == ".dockerignore":
			appendTo(h.Containers, "dockerignore", f)
		case base == "Containerfile":
			appendTo(h.Containers, "dockerfile", f)
		}
		if strings.HasPrefix(f, ".devcontainer/") || base == ".devcontainer.json" {
			h.Devcontainer = append(h.Devcontainer, f)
		}
		if reEnvExample.MatchString(f) {
			h.EnvExamples = append(h.EnvExamples, f)
		} else if reEnvReal.MatchString(f) {
			h.TrackedEnvFiles = append(h.TrackedEnvFiles, f) // names only; never read
		}
		switch {
		case strings.HasSuffix(low, ".tf") || strings.HasSuffix(low, ".tf.json"):
			appendTo(h.IaC, "terraform", f)
		case strings.HasSuffix(low, ".bicep"):
			appendTo(h.IaC, "bicep", f)
		case base == "Pulumi.yaml" || base == "cdk.json" || base == "serverless.yml" || base == "serverless.ts" || base == "skaffold.yaml" || base == "Chart.yaml" || base == "kustomization.yaml" || base == "samconfig.toml":
			appendTo(h.IaC, "config_files", f)
		case reIaCDir.MatchString(f) && (strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml")):
			appendTo(h.IaC, "yaml_in_infra_dirs", f)
		}
	}
	// A long list of Terraform files says little; keep the count's worth of paths short.
	for k, v := range h.IaC {
		if len(v) > 40 {
			h.IaC[k] = append(v[:40:40], fmt.Sprintf("... and %d more", len(v)-40))
		}
	}
	var ciSteps []ciStep
	for _, paths := range h.CI {
		for _, f := range paths {
			text, ok := readText(root, f, 2<<20)
			if !ok {
				continue
			}
			if strings.HasPrefix(f, ".github/") {
				ciSteps = append(ciSteps, parseGitHubWorkflow(f, text)...)
			} else if path.Base(f) == ".gitlab-ci.yml" {
				ciSteps = append(ciSteps, parseGitLabCI(f, text)...)
			}
		}
	}
	h.CIKindsRun, _ = ciCoverage(ciSteps)
	if h.CIKindsRun == nil {
		h.CIKindsRun = []string{}
	}
	h.Tests = countTestFiles(files)
	h.Todos = countTodos(root, scc)
	if len(h.TrackedEnvFiles) > 0 {
		h.Notes = append(h.Notes, "tracked_env_files lists env files committed to git (names only, contents never read)")
	}
	return h
}

// runSecretScan runs gitleaks or trufflehog when installed. It records rule
// names and file:line only; the matched values never leave the scanner.
func runSecretScan(repo string) secretScan {
	if p, err := exec.LookPath("gitleaks"); err == nil {
		tmp, err := os.MkdirTemp("", "cb-gitleaks-")
		if err != nil {
			return secretScan{Tool: "gitleaks", Unavailable: err.Error()}
		}
		defer os.RemoveAll(tmp)
		report := filepath.Join(tmp, "report.json")
		res, err := runCmd("", 20*time.Minute, nil, p, "detect", "--source", repo, "--redact", "--no-banner",
			"--report-format", "json", "--report-path", report, "--exit-code", "0")
		if err != nil || res.Code != 0 {
			msg := firstLine(res.Stderr)
			if err != nil {
				msg = err.Error()
			}
			return secretScan{Tool: "gitleaks", Unavailable: "gitleaks failed: " + msg}
		}
		var items []struct {
			RuleID    string `json:"RuleID"`
			File      string `json:"File"`
			StartLine int    `json:"StartLine"`
		}
		b, err := os.ReadFile(report)
		if err == nil {
			_ = json.Unmarshal(b, &items)
		}
		s := secretScan{Tool: "gitleaks", ByRule: map[string]int{}, Note: "findings include git history; values are never recorded"}
		n := 0
		s.Findings = &n
		for _, it := range items {
			n++
			s.ByRule[it.RuleID]++
			if len(s.Locations) < 50 {
				s.Locations = append(s.Locations, fmt.Sprintf("%s:%d (%s)", it.File, it.StartLine, it.RuleID))
			}
		}
		return s
	}
	if p, err := exec.LookPath("trufflehog"); err == nil {
		res, err := runCmd("", 20*time.Minute, nil, p, "git", "file://"+repo, "--json", "--no-update", "--no-verification")
		if err != nil {
			return secretScan{Tool: "trufflehog", Unavailable: err.Error()}
		}
		s := secretScan{Tool: "trufflehog", ByRule: map[string]int{}, Note: "values are never recorded"}
		n := 0
		s.Findings = &n
		sc := bufio.NewScanner(strings.NewReader(res.Stdout))
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var it struct {
				DetectorName   string `json:"DetectorName"`
				SourceMetadata struct {
					Data struct {
						Git struct {
							File string `json:"file"`
							Line int    `json:"line"`
						} `json:"Git"`
					} `json:"Data"`
				} `json:"SourceMetadata"`
			}
			if json.Unmarshal(sc.Bytes(), &it) != nil || it.DetectorName == "" {
				continue
			}
			n++
			s.ByRule[it.DetectorName]++
			if len(s.Locations) < 50 {
				g := it.SourceMetadata.Data.Git
				s.Locations = append(s.Locations, fmt.Sprintf("%s:%d (%s)", g.File, g.Line, it.DetectorName))
			}
		}
		return s
	}
	return secretScan{Unavailable: "neither gitleaks nor trufflehog is installed"}
}

func sortStrings(s []string) []string {
	sort.Strings(s)
	return s
}
