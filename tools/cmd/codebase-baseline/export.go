package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func (a *app) cmdExport(args []string) error {
	fl := flag.NewFlagSet("export", flag.ContinueOnError)
	strip := fl.Bool("strip-docs", false, "delete prose docs and agent-instruction files, list them in <dest>.removed.txt")
	var keep stringList
	fl.Var(&keep, "keep", "glob of a doc file to keep under --strip-docs (repeatable): content or code that only looks like docs")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline export <repo> <dest> [--strip-docs]")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		fl.Usage()
		return fmt.Errorf("export needs <repo> and <dest>")
	}
	repo, dest := pos[0], pos[1]
	if !hasGit(repo) {
		return fmt.Errorf("%s is not inside a git checkout", repo)
	}
	if pathInside(dest, repo) {
		return fmt.Errorf("dest %s is inside the repo", dest)
	}
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return fmt.Errorf("dest %s exists and is not empty", dest)
	}
	exp, err := exportTree(repo, "HEAD", dest)
	if err != nil {
		return err
	}
	sha, _ := git(repo, "rev-parse", "HEAD")
	where := repo
	if prefix := strings.TrimSuffix(gitPrefix(repo), "/"); prefix != "" {
		top, _ := git(repo, "rev-parse", "--show-toplevel")
		where = fmt.Sprintf("%s (only the subtree %s/ of the git work tree %s; the rest of that checkout is not exported)", repo, prefix, strings.TrimSpace(top))
	}
	fmt.Fprintf(a.stdout, "exported %d files from %s at %s to %s\n", exp.Files, where, strings.TrimSpace(sha), dest)
	var removed []removedFile
	for _, s := range exp.Symlinks {
		removed = append(removed, removedFile{s, "symlink"})
	}
	removed = append(removed, exp.Secrets...)
	for _, s := range exp.Skipped {
		fmt.Fprintf(a.stdout, "  left out: %s\n", s)
	}
	if len(exp.Symlinks) > 0 {
		fmt.Fprintf(a.stdout, "  left out %d symlinks\n", len(exp.Symlinks))
	}
	if len(exp.Secrets) > 0 {
		fmt.Fprintf(a.stdout, "  left out %d secret-like files (env files, keys, credentials)\n", len(exp.Secrets))
	}
	if *strip {
		var keepRes []*regexp.Regexp
		for _, k := range keep {
			keepRes = append(keepRes, globToRE(k))
		}
		sr := stripDocs(dest, keepRes)
		for _, f := range sr.files {
			removed = append(removed, removedFile{f, sr.reasons[f]})
		}
		fmt.Fprintf(a.stdout, "stripped %d files (%d bytes of prose and agent instructions)", len(sr.files), sr.bytes)
		if sr.kept > 0 {
			fmt.Fprintf(a.stdout, ", kept %d matching --keep", sr.kept)
		}
		fmt.Fprintln(a.stdout)
		for _, k := range sortedKeys(sr.byReason) {
			fmt.Fprintf(a.stdout, "  %-28s %d\n", k, sr.byReason[k])
		}
	}
	if len(removed) > 0 {
		sort.Slice(removed, func(i, j int) bool { return removed[i].Path < removed[j].Path })
		var b strings.Builder
		for _, r := range removed {
			b.WriteString(r.Path + "\t" + r.Reason + "\n")
		}
		list := filepath.Clean(dest) + ".removed.txt"
		if err := os.WriteFile(list, []byte(b.String()), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "removed files listed in %s (path, reason)\n", list)
	}
	return nil
}

// exportResult says what an export wrote and what it left out on purpose.
type exportResult struct {
	Files    int
	Symlinks []string
	Secrets  []removedFile
	EnvFiles []string
	Skipped  []string // submodules and other non-file entries
}

type removedFile struct{ Path, Reason string }

var envSafeSuffixes = []string{".example", ".sample", ".template", ".dist"}

// secretLike says why a tracked path must not be exported: env files with
// values, keys and credentials. Example env files stay.
func secretLike(rel string) string {
	base := path.Base(rel)
	lb := strings.ToLower(base)
	switch {
	case lb == ".env" || strings.HasPrefix(lb, ".env."):
		for _, s := range envSafeSuffixes {
			if strings.HasSuffix(lb, s) {
				return ""
			}
		}
		return "env file"
	case strings.HasSuffix(lb, ".pem"), strings.HasSuffix(lb, ".key"), strings.HasSuffix(lb, ".p12"), strings.HasSuffix(lb, ".pfx"), strings.HasSuffix(lb, ".keystore"):
		return "key or certificate file"
	case strings.HasPrefix(lb, "id_rsa"), strings.HasPrefix(lb, "id_ed25519"), strings.HasPrefix(lb, "id_ecdsa"), strings.HasPrefix(lb, "id_dsa"):
		return "ssh key"
	case strings.HasPrefix(lb, "credentials") && strings.HasSuffix(lb, ".json"):
		return "credentials file"
	}
	return ""
}

// exportTree writes the files of rev into dest by reading the git object
// database directly: no index (the repo's own stays untouched), no attributes
// (export-ignore cannot drop tests or CI), no filters or hooks (a hostile
// config runs nothing). Symlinks, submodules and secret-like files are left out
// and reported.
func exportTree(repo, rev, dest string) (exportResult, error) {
	var res exportResult
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return res, err
	}
	top := repo
	if p := strings.TrimSuffix(gitPrefix(repo), "/"); p != "" {
		rev += ":" + p // a folder of a bigger checkout: export just that subtree
		if t, err := git(repo, "rev-parse", "--show-toplevel"); err == nil {
			top = strings.TrimSpace(t)
		}
	}
	listing, err := git(top, "ls-tree", "-r", "-z", rev)
	if err != nil {
		return res, err
	}
	type entry struct {
		mode, sha, path string
	}
	var entries []entry
	for _, rec := range strings.Split(listing, "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			continue
		}
		p = path.Clean(p)
		if p == "." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
			continue
		}
		switch {
		case f[1] == "commit":
			res.Skipped = append(res.Skipped, p+" (submodule)")
		case f[0] == "120000":
			res.Symlinks = append(res.Symlinks, p)
		case secretLike(p) != "":
			reason := secretLike(p)
			res.Secrets = append(res.Secrets, removedFile{p, reason})
			if reason == "env file" {
				res.EnvFiles = append(res.EnvFiles, p)
			}
		case f[1] == "blob":
			entries = append(entries, entry{f[0], f[2], p})
		}
	}
	if len(entries) == 0 {
		return res, nil
	}
	cmd := exec.Command("git", append(gitSafeArgs(top), "cat-file", "--batch")...)
	cmd.Env = gitEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return res, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return res, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return res, err
	}
	go func() {
		w := bufio.NewWriter(stdin)
		for _, e := range entries {
			fmt.Fprintln(w, e.sha)
		}
		w.Flush()
		stdin.Close()
	}()
	br := bufio.NewReaderSize(stdout, 1<<20)
	root := filepath.Clean(dest)
	var werr error
	for _, e := range entries {
		header, err := br.ReadString('\n')
		if err != nil {
			werr = fmt.Errorf("git cat-file: %v %s", err, firstLine(stderr.String()))
			break
		}
		hf := strings.Fields(header)
		if len(hf) != 3 {
			werr = fmt.Errorf("git cat-file: unexpected header %q", strings.TrimSpace(header))
			break
		}
		size, _ := strconv.ParseInt(hf[2], 10, 64)
		target := filepath.Join(root, filepath.FromSlash(e.path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			werr = err
			break
		}
		mode := os.FileMode(0o644)
		if e.mode == "100755" {
			mode = 0o755
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			werr = err
			break
		}
		_, cerr := io.CopyN(f, br, size)
		if err := f.Close(); cerr == nil {
			cerr = err
		}
		if cerr != nil {
			werr = cerr
			break
		}
		_, _ = br.ReadByte() // the newline after each object
		res.Files++
	}
	io.Copy(io.Discard, stdout)
	_ = cmd.Wait()
	sort.Strings(res.Symlinks)
	sort.Strings(res.EnvFiles)
	sort.Slice(res.Secrets, func(i, j int) bool { return res.Secrets[i].Path < res.Secrets[j].Path })
	return res, werr
}

var (
	docExts     = map[string]bool{".md": true, ".mdx": true, ".markdown": true, ".rst": true, ".adoc": true, ".asciidoc": true}
	docDirNames = map[string]bool{"docs": true, "doc": true, "documentation": true, "adr": true, "adrs": true, "decisions": true, "wiki": true, "rfcs": true}
	// agentFiles match by base name at any depth.
	agentFiles = map[string]bool{"AGENTS.md": true, "CLAUDE.md": true, "GEMINI.md": true, ".cursorrules": true, ".windsurfrules": true, ".clinerules": true}
	// agentDirs are directories removed wherever they sit.
	agentDirs = map[string]bool{".claude": true, ".cursor": true, ".continue": true, ".junie": true, ".kiro": true, ".roo": true, ".codex": true, ".agents": true}
	// proseNames are extensionless (or .txt) top-level notes such as README.
	proseNames = []string{"README", "CHANGELOG", "CONTRIBUTING", "ARCHITECTURE", "HISTORY", "CODE_OF_CONDUCT", "SECURITY", "NOTICE"}
)

type stripResult struct {
	files    []string
	reasons  map[string]string
	bytes    int64
	byReason map[string]int
	kept     int
}

// stripReason says why a path is removed, or "" when it stays. rel uses slashes.
// Directory names match without regard to case.
func stripReason(rel string, isDir bool) string {
	base := path.Base(rel)
	lbase := strings.ToLower(base)
	if isDir {
		switch {
		case docDirNames[lbase]:
			return "doc directory"
		case agentDirs[lbase]:
			return "agent directory"
		case strings.HasPrefix(lbase, ".aider"):
			return "agent file"
		case strings.EqualFold(rel, ".github/instructions") || strings.EqualFold(rel, ".github/prompts"):
			return "agent directory"
		}
		return ""
	}
	switch {
	case agentFiles[base] || agentFiles[lbase] || agentFilesLower[lbase]:
		return "agent file"
	case strings.EqualFold(rel, ".github/copilot-instructions.md"):
		return "agent file"
	case strings.HasPrefix(lbase, ".aider"):
		return "agent file"
	}
	ext := strings.ToLower(path.Ext(base))
	if docExts[ext] {
		return "prose doc"
	}
	stem := strings.ToUpper(strings.TrimSuffix(base, path.Ext(base)))
	if ext == "" || ext == ".txt" {
		for _, p := range proseNames {
			if stem == p || strings.HasPrefix(stem, p+"_") || strings.HasPrefix(stem, p+"-") {
				return "prose doc"
			}
		}
	}
	return ""
}

var agentFilesLower = map[string]bool{"agents.md": true, "claude.md": true, "gemini.md": true}

// stripDocs deletes prose docs and agent-instruction files under dest, except
// files matching a keep glob.
func stripDocs(dest string, keep []*regexp.Regexp) stripResult {
	res := stripResult{byReason: map[string]int{}, reasons: map[string]string{}}
	root := filepath.Clean(dest)
	remove := func(p, rel, reason string) {
		if matchesAny(keep, rel) {
			res.kept++
			return
		}
		if info, err := os.Lstat(p); err == nil {
			res.bytes += info.Size()
		}
		res.files = append(res.files, rel)
		res.reasons[rel] = reason
		res.byReason[reason]++
		_ = os.Remove(p)
	}
	var walk func(dir, rel string, inRemovedDir string)
	walk = func(dir, rel string, dirReason string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			r := path.Join(rel, e.Name())
			if e.IsDir() {
				reason := dirReason
				if reason == "" {
					reason = stripReason(r, true)
				}
				walk(p, r, reason)
				if rest, err := os.ReadDir(p); err == nil && len(rest) == 0 {
					_ = os.Remove(p)
				}
				continue
			}
			reason := dirReason
			if reason == "" {
				reason = stripReason(r, false)
			}
			if reason != "" {
				remove(p, r, reason)
			}
		}
	}
	walk(root, "", "")
	sort.Strings(res.files)
	return res
}
