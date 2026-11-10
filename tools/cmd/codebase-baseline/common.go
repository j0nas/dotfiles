package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// parseFlags parses args with flags allowed before, between and after the
// positional arguments, and returns the positionals.
func parseFlags(fsx *flag.FlagSet, args []string, stderr io.Writer) ([]string, error) {
	fsx.SetOutput(stderr)
	var pos []string
	for {
		if err := fsx.Parse(args); err != nil {
			return nil, err
		}
		args = fsx.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// absPath resolves symlinks of the longest existing prefix of p.
func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rest := ""
	cur := abs
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func pathInside(child, parent string) bool {
	c, p := absPath(child), absPath(parent)
	rel, err := filepath.Rel(p, c)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// refuseInside stops an output path from landing inside the analysed repo.
func refuseInside(out, repo string) error {
	if out != "" && pathInside(out, repo) {
		return fmt.Errorf("refusing to write %s inside the analysed repo %s: pick an output path outside it", out, repo)
	}
	return nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// execResult is the outcome of a short helper command.
type execResult struct {
	Stdout, Stderr string
	Code           int
}

func runCmd(dir string, timeout time.Duration, env []string, name string, args ...string) (execResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	res := execResult{Stdout: so.String(), Stderr: se.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.Code = ee.ExitCode()
		return res, nil
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return res, err
}

// gitSafeArgs are put before every git command: a hostile .git/config must not
// execute anything (no fsmonitor, no hooks, no file-protocol submodule tricks),
// and nothing may take a lock in the analysed repo.
func gitSafeArgs(repo string) []string {
	return []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "protocol.file.allow=user", "--no-optional-locks",
		"-C", repo, "-c", "core.quotepath=off", "-c", "core.pager=cat"}
}

func gitEnv() []string {
	return append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
}

// git runs a read-only git command in repo and returns its stdout.
func git(repo string, args ...string) (string, error) {
	full := append(gitSafeArgs(repo), args...)
	res, err := runCmd("", 10*time.Minute, gitEnv(), "git", full...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return res.Stdout, nil
}

var gitInside = map[string]bool{}

// hasGit reports whether repo is a git checkout or a folder inside one.
func hasGit(repo string) bool {
	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
		return true
	}
	key := absPath(repo)
	if v, ok := gitInside[key]; ok {
		return v
	}
	out, err := git(repo, "rev-parse", "--is-inside-work-tree")
	gitInside[key] = err == nil && strings.TrimSpace(out) == "true"
	return gitInside[key]
}

// gitPrefix is the path of repo inside its git checkout ("" at the top, else
// "sub/dir/"). Analysing a monorepo folder limits every history query to it.
func gitPrefix(repo string) string {
	out, err := git(repo, "rev-parse", "--show-prefix")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// vendorDirs are tool and dependency directories that are never source, at any
// depth: installed packages, virtual environments and tool caches.
var vendorDirs = map[string]bool{
	"node_modules": true, "bower_components": true, ".venv": true, "venv": true, "site-packages": true,
	"__pycache__": true, ".next": true, ".nuxt": true, ".svelte-kit": true, ".gradle": true, "Pods": true,
	".git": true, ".tox": true, ".mypy_cache": true, ".terraform": true,
}

// rootOnlyDirs are vendored or generated only where a project keeps them: at the
// repo root or the root of a project (a directory holding a manifest). A
// "build" folder deeper in a source tree is usually code.
var rootOnlyDirs = map[string]bool{"vendor": true, "coverage": true, "dist": true, "build": true, "third_party": true, "third-party": true}

// projectRoots holds the directories that hold a recognised manifest, set from
// each file listing; the repo root always counts.
var projectRoots = map[string]bool{".": true}

func isManifestName(base string) bool {
	switch base {
	case "package.json", "go.mod", "Cargo.toml", "pyproject.toml", "setup.py", "setup.cfg", "pom.xml", "build.gradle", "build.gradle.kts",
		"settings.gradle", "settings.gradle.kts", "composer.json", "Gemfile", "Pipfile":
		return true
	}
	return strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt") ||
		strings.HasSuffix(base, ".csproj") || strings.HasSuffix(base, ".fsproj") || strings.HasSuffix(base, ".sln")
}

func setProjectRoots(paths []string) {
	projectRoots = map[string]bool{".": true}
	for _, p := range paths {
		if isManifestName(path.Base(p)) {
			projectRoots[path.Dir(p)] = true
		}
	}
}

func isVendoredPath(rel string) bool {
	segs := strings.Split(rel, "/")
	for i, seg := range segs[:len(segs)-1] {
		if vendorDirs[seg] {
			return true
		}
		if rootOnlyDirs[seg] {
			parent := "."
			if i > 0 {
				parent = strings.Join(segs[:i], "/")
			}
			if projectRoots[parent] {
				return true
			}
		}
	}
	return false
}

// dropVendored sets the project roots from the listing and removes vendored paths.
func dropVendored(files []string) []string {
	setProjectRoots(files)
	out := files[:0:0]
	for _, f := range files {
		if !isVendoredPath(f) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// walkFiles lists the regular files under root as slash-separated relative
// paths, skipping vendored directories.
func walkFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && vendorDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return dropVendored(out)
}

// listRepoFiles returns the tracked files of a git checkout, or the files on
// disk when root isn't the top of one. Vendored paths are dropped.
func listRepoFiles(root string) []string {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		if files, err := gitTrackedFiles(root); err == nil {
			return files
		}
	}
	return walkFiles(root)
}

// gitTrackedFiles lists the files git tracks under dir, relative to dir, minus
// vendored paths. It works for a folder inside a bigger checkout too.
func gitTrackedFiles(dir string) ([]string, error) {
	out, err := git(dir, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return dropVendored(files), nil
}

func readText(root, rel string, limit int64) (string, bool) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Size() > limit {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return "", false
	}
	return string(b), true
}

func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func median(xs []int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return float64(s[n/2-1]+s[n/2]) / 2
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5*sign(f))) / 100
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// monthsBack lists n months ending with the month of t, oldest first.
func monthsBack(t time.Time, n int) []string {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[n-1-i] = first.AddDate(0, -i, 0).Format("2006-01")
	}
	return out
}

var reNonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return strings.Trim(reNonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// scoped adds a pathspec limiting a git log to the analysed folder, but only
// when that folder is a subfolder of the checkout: a pathspec at the top would
// simplify history and drop merge commits.
func scoped(repo string, args ...string) []string {
	if gitPrefix(repo) != "" {
		return append(args, "--", ".")
	}
	return args
}
