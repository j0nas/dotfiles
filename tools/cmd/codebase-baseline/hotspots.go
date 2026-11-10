package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

type tmNode struct {
	Name       string
	Path       string
	Loc        int64
	Churn      int
	Complexity int64
	Lang       string
	Children   []*tmNode
	leaf       bool
	parent     *tmNode
	other      *tmNode // the folded "other" leaf of a directory
	ChurnLabel string  // set on the root only
	folded     int
}

func (n *tmNode) MarshalJSON() ([]byte, error) {
	if n.leaf {
		return json.Marshal(struct {
			Name       string `json:"name"`
			Path       string `json:"path"`
			Loc        int64  `json:"loc"`
			Churn      int    `json:"churn"`
			Complexity int64  `json:"complexity"`
			Lang       string `json:"lang"`
		}{n.Name, n.Path, n.Loc, n.Churn, n.Complexity, n.Lang})
	}
	return json.Marshal(struct {
		Name       string    `json:"name"`
		ChurnLabel string    `json:"churnLabel,omitempty"`
		Children   []*tmNode `json:"children"`
	}{n.Name, n.ChurnLabel, n.Children})
}

type hotspot struct {
	Path       string `json:"path"`
	Loc        int64  `json:"loc"`
	Churn      int    `json:"churn"`
	Complexity int64  `json:"complexity"`
	Lang       string `json:"lang"`
	Score      int64  `json:"score"`
}

type hotspotsOut struct {
	SchemaVersion int       `json:"schema_version"`
	Tool          toolInfo  `json:"tool"`
	Repo          string    `json:"repo"`
	FilesHead     string    `json:"files_head_sha,omitempty"`
	RepoHead      string    `json:"repo_head_sha"`
	GeneratedAt   string    `json:"generated_at"`
	Days          int       `json:"days"`
	Note          string    `json:"note"`
	ChurnLabel    string    `json:"churnLabel"`
	MaxLeaves     int       `json:"max_leaves"`
	Summary       hsSummary `json:"summary"`
	Hotspots      struct {
		ByLocXChurn        []hotspot `json:"loc_x_churn"`
		ByComplexityXChurn []hotspot `json:"complexity_x_churn"`
	} `json:"hotspots"`
	Tree *tmNode `json:"tree"`
}

type hsSummary struct {
	Files          int   `json:"files"`
	TotalLoc       int64 `json:"total_loc"`
	FilesWithChurn int   `json:"files_with_churn"`
	CommitsScanned int   `json:"commits_scanned"`
	DroppedGen     int   `json:"dropped_generated_or_minified"`
	DroppedVendor  int   `json:"dropped_vendored"`
	DroppedData    int   `json:"dropped_docs_and_data"`
	FoldedFiles    int   `json:"files_folded_into_other"`
}

func (a *app) cmdHotspots(args []string) error {
	fl := flag.NewFlagSet("hotspots", flag.ContinueOnError)
	files := fl.String("files", "", "files.json from measure --files-out (required)")
	repo := fl.String("repo", "", "the git repo whose history gives the churn (required)")
	days := fl.Int("days", 180, "churn window in days")
	out := fl.String("out", "", "treemap.json to write (required)")
	maxLeaves := fl.Int("max-leaves", 600, "fold the smallest files into one \"other\" leaf per directory until the tree has at most this many leaves")
	includeData := fl.Bool("include-data", false, "keep docs and data files (Markdown, JSON, YAML, ...)")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline hotspots --files files.json --repo <repo> --days 180 --out treemap.json")
		fl.PrintDefaults()
	}
	if _, err := parseFlags(fl, args, a.stderr); err != nil {
		return err
	}
	if *files == "" || *repo == "" || *out == "" {
		fl.Usage()
		return fmt.Errorf("hotspots needs --files, --repo and --out")
	}
	if err := refuseInside(*out, *repo); err != nil {
		return err
	}
	var fo filesOut
	if err := readJSON(*files, &fo); err != nil {
		return err
	}
	setProjectRoots(sccPaths(fo.Files))
	if !hasGit(*repo) {
		return fmt.Errorf("%s is not a git checkout", *repo)
	}
	head, err := git(*repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	since := a.now().AddDate(0, 0, -*days)
	logOut, err := git(*repo, scoped(*repo, "log", "--no-merges", "--no-renames", "--name-only", "--relative", "--since="+since.Format(time.RFC3339), "--format=%x1e")...)
	if err != nil {
		return err
	}
	churn := map[string]int{}
	commits := 0
	for _, rec := range strings.Split(logOut, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		commits++
		seen := map[string]bool{}
		for _, f := range strings.Split(rec, "\n") {
			if f = strings.TrimSpace(f); f != "" && !seen[f] {
				seen[f] = true
				churn[f]++
			}
		}
	}
	res := hotspotsOut{SchemaVersion: schemaVersion, Tool: currentTool(), Repo: absPath(*repo), FilesHead: fo.Head, RepoHead: strings.TrimSpace(head),
		GeneratedAt: a.now().UTC().Format(time.RFC3339), Days: *days, MaxLeaves: *maxLeaves,
		ChurnLabel: fmt.Sprintf("commits touching the file in the last %d days", *days),
		Note:       "loc is scc's code lines; churn is the number of non-merge commits touching the file in the window; generated, minified, vendored, docs and data files are dropped"}
	res.Summary.CommitsScanned = commits
	root := &tmNode{Name: pathBase(res.Repo)}
	var spots []hotspot
	for _, f := range fo.Files {
		switch {
		case f.Generated || f.Minified:
			res.Summary.DroppedGen++
			continue
		case isVendoredPath(f.Path):
			res.Summary.DroppedVendor++
			continue
		case docDataLangs[f.Lang] && !*includeData:
			res.Summary.DroppedData++
			continue
		}
		c := churn[f.Path]
		insertLeaf(root, f.Path, &tmNode{Name: path.Base(f.Path), Path: f.Path, Loc: f.Code, Churn: c, Complexity: f.Complexity, Lang: f.Lang, leaf: true})
		res.Summary.Files++
		res.Summary.TotalLoc += f.Code
		if c > 0 {
			res.Summary.FilesWithChurn++
			spots = append(spots, hotspot{Path: f.Path, Loc: f.Code, Churn: c, Complexity: f.Complexity, Lang: f.Lang})
		}
	}
	res.Summary.FoldedFiles = foldSmallest(root, *maxLeaves)
	collapse(root, true)
	sortTree(root)
	root.ChurnLabel = res.ChurnLabel
	res.Tree = root
	res.Hotspots.ByLocXChurn = topBy(spots, func(h hotspot) int64 { return h.Loc * int64(h.Churn) }, 20)
	res.Hotspots.ByComplexityXChurn = topBy(spots, func(h hotspot) int64 { return h.Complexity * int64(h.Churn) }, 20)
	if err := writeJSON(*out, res); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s: %d files, %d with churn in %d days (%d commits)\n", *out, res.Summary.Files, res.Summary.FilesWithChurn, *days, commits)
	for _, h := range res.Hotspots.ByLocXChurn[:min(5, len(res.Hotspots.ByLocXChurn))] {
		fmt.Fprintf(a.stdout, "  loc x churn  %-60s loc=%d churn=%d\n", h.Path, h.Loc, h.Churn)
	}
	return nil
}

func pathBase(p string) string {
	return path.Base(strings.TrimRight(p, "/"))
}

func insertLeaf(root *tmNode, p string, leaf *tmNode) {
	parts := strings.Split(p, "/")
	cur := root
	for _, seg := range parts[:len(parts)-1] {
		var next *tmNode
		for _, c := range cur.Children {
			if !c.leaf && c.Name == seg {
				next = c
				break
			}
		}
		if next == nil {
			next = &tmNode{Name: seg}
			cur.Children = append(cur.Children, next)
		}
		cur = next
	}
	leaf.parent = cur
	cur.Children = append(cur.Children, leaf)
}

// foldSmallest merges the smallest files into one "other" leaf per directory
// until the tree has at most max leaves (or nothing more can be folded). It
// returns how many files were folded.
func foldSmallest(root *tmNode, max int) int {
	if max <= 0 {
		return 0
	}
	var leaves []*tmNode
	var walk func(n *tmNode)
	walk = func(n *tmNode) {
		for _, c := range n.Children {
			if c.leaf {
				leaves = append(leaves, c)
			} else {
				walk(c)
			}
		}
	}
	walk(root)
	total := len(leaves)
	if total <= max {
		return 0
	}
	sort.SliceStable(leaves, func(i, j int) bool {
		if leaves[i].Loc != leaves[j].Loc {
			return leaves[i].Loc < leaves[j].Loc
		}
		return leaves[i].Path < leaves[j].Path
	})
	folded := 0
	for _, l := range leaves {
		if total <= max {
			break
		}
		dir := l.parent
		if dir.other == nil {
			// The first file folded in a directory only changes its name: the leaf count drops from the second on.
			dir.other = &tmNode{Name: "other", Path: dirPath(l.Path) + "/", Lang: "mixed", leaf: true, parent: dir}
			dir.Children = append(dir.Children, dir.other)
			total++
		}
		o := dir.other
		o.Loc += l.Loc
		o.Churn += l.Churn
		o.Complexity += l.Complexity
		o.folded++
		o.Name = fmt.Sprintf("other (%d files)", o.folded)
		if o.folded == 1 {
			o.Name = "other (1 file)"
		}
		for i, c := range dir.Children {
			if c == l {
				dir.Children = append(dir.Children[:i], dir.Children[i+1:]...)
				break
			}
		}
		total--
		folded++
	}
	return folded
}

func dirPath(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

// collapse folds directories with a single child into their child, so a chain
// like src/main/java/com becomes one box.
func collapse(n *tmNode, isRoot bool) {
	for _, c := range n.Children {
		if !c.leaf {
			collapse(c, false)
		}
	}
	for i, c := range n.Children {
		for !c.leaf && len(c.Children) == 1 {
			only := c.Children[0]
			if only.leaf {
				only.Name = c.Name + "/" + only.Name
				c = only
			} else {
				only.Name = c.Name + "/" + only.Name
				c = only
			}
		}
		n.Children[i] = c
	}
}

func sortTree(n *tmNode) {
	sort.SliceStable(n.Children, func(i, j int) bool { return n.Children[i].Name < n.Children[j].Name })
	for _, c := range n.Children {
		if !c.leaf {
			sortTree(c)
		}
	}
}

func topBy(spots []hotspot, score func(hotspot) int64, n int) []hotspot {
	out := make([]hotspot, 0, len(spots))
	for _, h := range spots {
		h.Score = score(h)
		if h.Score > 0 {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
