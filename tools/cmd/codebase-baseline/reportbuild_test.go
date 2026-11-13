package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The tests build against the real template, so a change to its slots that
// the tool no longer understands fails here.
const templateRel = "../../../skills/codebase-baseline/assets/report-template.html"

func loadTemplate(t *testing.T) *reportTemplate {
	t.Helper()
	b, err := os.ReadFile(templateRel)
	if err != nil {
		t.Fatal(err)
	}
	tm, err := parseReportTemplate(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// fixture copies testdata/report-src, a complete small source folder, to a temp dir.
func fixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "report-src")
	copyTree(t, "testdata/report-src", dir)
	return dir
}

func writeIn(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readIn(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func replaceIn(t *testing.T, dir, rel, old, new string) {
	t.Helper()
	s := readIn(t, dir, rel)
	if !strings.Contains(s, old) {
		t.Fatalf("%s has no %q", rel, old)
	}
	writeIn(t, dir, rel, strings.Replace(s, old, new, 1))
}

func build(t *testing.T, tm *reportTemplate, src string, draft bool) buildResult {
	t.Helper()
	return buildReport(tm, buildOpts{src: src, draft: draft})
}

func problemsText(r buildResult) string { return strings.Join(r.problems, "\n") }

func TestBuildReportFixture(t *testing.T) {
	tm := loadTemplate(t)
	res := build(t, tm, fixture(t), false)
	if len(res.problems) > 0 {
		t.Fatalf("fixture should build:\n%s", problemsText(res))
	}
	page := string(res.page)
	for _, want := range []string{
		"<title>Tiny Baseline</title>",
		`<li><a href="#plan">Plan</a></li>`,
		`<li><a href="#quality">Quality &amp; readiness</a></li>`,
		`<section id="what" data-asof="2026-11-13">`,
		`<section id="summary">`,
		`<h2>Quality &amp; factory readiness</h2>`,
		`<li><time datetime="2026-11-13">2026-11-13</time><span>First baseline of <code>abc1234</code>.`,
		"[].concat(J('commits-data') || [])",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(page, "PLACEHOLDER") || strings.Contains(page, "DUMMY") {
		t.Error("comments should be dropped from the page")
	}
	// What check-report checks structurally.
	cr := checkReport(page, defaultAllowSet(), nil, false)
	if !cr.Passed {
		t.Errorf("check-report fails the built page: %+v", cr.Failures)
	}
}

func TestBuildReportDeterministic(t *testing.T) {
	tm := loadTemplate(t)
	a := build(t, tm, fixture(t), false)
	b := build(t, tm, fixture(t), false)
	if len(a.problems) > 0 || len(b.problems) > 0 {
		t.Fatalf("%v %v", a.problems, b.problems)
	}
	if !bytes.Equal(a.page, b.page) {
		t.Error("the same sources built two different pages")
	}
}

var (
	reJSONBlock = regexp.MustCompile(`(?s)<script type="application/json" id="([^"]+)">(.*?)</script>`)
	reWS        = regexp.MustCompile(`\s+`)
)

// normalised drops what the build changes on purpose: comments, the title,
// data-asof attributes, whitespace, and the JSON blocks (compared separately).
func normalised(page string) (string, map[string]any) {
	blocks := map[string]any{}
	for _, m := range reJSONBlock.FindAllStringSubmatch(page, -1) {
		var v any
		if err := json.Unmarshal([]byte(m[2]), &v); err != nil {
			blocks[m[1]] = "INVALID: " + err.Error()
			continue
		}
		blocks[m[1]] = v
	}
	s := reJSONBlock.ReplaceAllString(page, "")
	s = stripHTMLComments(s)
	s = reTmplTitle.ReplaceAllString(s, "")
	s = regexp.MustCompile(` data-asof="[^"]*"`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`>\s+<`).ReplaceAllString(s, "><")
	return strings.TrimSpace(reWS.ReplaceAllString(s, " ")), blocks
}

// --init writes a folder the build turns back into the template, as a demo page.
func TestBuildReportInitRoundTrip(t *testing.T) {
	tm := loadTemplate(t)
	a := &app{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, now: timeNow}
	dir := filepath.Join(t.TempDir(), "report-src")
	if err := a.cmdBuildReport([]string{"--init", "--src", dir, "--template", templateRel}); err != nil {
		t.Fatal(err)
	}
	// every slot has a file
	for _, s := range tm.sections {
		if _, err := os.Stat(filepath.Join(dir, "sections", s.ID+".html")); err != nil {
			t.Errorf("no file for section %s", s.ID)
		}
	}
	for _, blk := range tm.blocks {
		if _, err := os.Stat(filepath.Join(dir, "data", blk.ID+".json")); err != nil {
			t.Errorf("no file for data block %s", blk.ID)
		}
	}
	for _, f := range []string{"sections.json", "changelog.txt", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("no %s", f)
		}
	}
	readme := readIn(t, dir, "README.md")
	if !strings.Contains(readme, "data/summary-data.json: ") || !strings.Contains(readme, "exactly 5 facts") {
		t.Errorf("README should carry the template's shape notes:\n%s", readme)
	}
	// the starter holds the template's example text, so a plain build refuses it
	res := build(t, tm, dir, false)
	if len(res.problems) != 1 || !strings.Contains(res.problems[0], "template's example content") || !strings.Contains(res.problems[0], "sections/what.html") || !strings.Contains(res.problems[0], "data/summary-data.json") || !strings.Contains(res.problems[0], "changelog.txt") {
		t.Fatalf("starter folder should be refused as example content: %v", res.problems)
	}
	// --draft builds it, and it is the template page
	res = build(t, tm, dir, true)
	if len(res.problems) > 0 {
		t.Fatalf("draft build: %v", res.problems)
	}
	if len(res.notes) != 1 {
		t.Errorf("draft should list the example content once: %v", res.notes)
	}
	got, gotBlocks := normalised(string(res.page))
	want, wantBlocks := normalised(tm.src)
	if got != want {
		for i := 0; i < len(got) && i < len(want); i++ {
			if got[i] != want[i] {
				lo := max(0, i-80)
				t.Fatalf("page differs from the template near\n got: %s\nwant: %s", got[lo:min(len(got), i+120)], want[lo:min(len(want), i+120)])
			}
		}
		t.Fatalf("page differs in length: %d vs %d", len(got), len(want))
	}
	gb, _ := json.Marshal(gotBlocks)
	wb, _ := json.Marshal(wantBlocks)
	if !bytes.Equal(gb, wb) {
		t.Errorf("data blocks differ:\n%s\n%s", gb, wb)
	}
	// init refuses a folder that has files
	if err := a.cmdBuildReport([]string{"--init", "--src", dir, "--template", templateRel}); err == nil || !strings.Contains(err.Error(), "already has files") {
		t.Errorf("init over a non-empty folder: %v", err)
	}
}

// The command writes the page and exits clean, or lists the problems and writes nothing.
func TestBuildReportCommand(t *testing.T) {
	src := fixture(t)
	out := filepath.Join(t.TempDir(), "report", "index.html")
	var so, se bytes.Buffer
	a := &app{stdout: &so, stderr: &se, now: timeNow}
	if err := a.cmdBuildReport([]string{"--template", templateRel, "--src", src, "--out", out}); err != nil {
		t.Fatalf("%v\n%s", err, se.String())
	}
	first, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(so.String(), "built ") {
		t.Errorf("stdout: %s", so.String())
	}
	os.Remove(filepath.Join(src, "sections", "plan.html"))
	se.Reset()
	if err := a.cmdBuildReport([]string{"--template", templateRel, "--src", src, "--out", out}); err != errFailed {
		t.Fatalf("expected errFailed, got %v", err)
	}
	if !strings.Contains(se.String(), `section "plan" has no file sections/plan.html`) {
		t.Errorf("stderr: %s", se.String())
	}
	after, _ := os.ReadFile(out)
	if !bytes.Equal(first, after) {
		t.Error("a failed build changed the existing page")
	}
	if err := a.cmdBuildReport([]string{"--src", src}); err == nil {
		t.Error("missing --out should fail")
	}
}

func TestBuildReportAddedSections(t *testing.T) {
	tm := loadTemplate(t)
	src := fixture(t)
	replaceIn(t, src, "sections.json", `{"id":"scoreboard"`, `{"id":"changes","title":"What changed on 2026-11-14","nav":"What changed","asof":"2026-11-14"},
    {"id":"decisions","title":"Decisions for you","open":true},
    {"id":"scoreboard"`)
	writeIn(t, src, "sections/changes.html", `<p class="sub">Two fixes, both deployed. See <a href="#d-1">decision 1</a>.</p>`+"\n")
	writeIn(t, src, "sections/decisions.html", `<table><tbody><tr id="d-1"><td>1</td><td>Turn on HTTPS.</td></tr></tbody></table>`+"\n")
	res := build(t, tm, src, false)
	if len(res.problems) > 0 {
		t.Fatalf("%s", problemsText(res))
	}
	page := string(res.page)
	pos := func(s string) int { return strings.Index(page, s) }
	if !(pos(`<section id="summary">`) < pos(`<section id="changes" data-asof="2026-11-14">`) &&
		pos(`<section id="changes"`) < pos(`<section id="decisions">`) &&
		pos(`<section id="decisions">`) < pos(`<section id="scoreboard">`)) {
		t.Error("sections are not in the order sections.json gives")
	}
	if !(pos(`<li><a href="#summary">`) < pos(`<li><a href="#changes">What changed</a></li>`) && pos(`<li><a href="#changes">`) < pos(`<li><a href="#decisions">Decisions for you</a></li>`) && pos(`<li><a href="#decisions">`) < pos(`<li><a href="#scoreboard">`)) {
		t.Error("the index does not follow sections.json")
	}
	if !strings.Contains(page, `<details open><summary><h2>Decisions for you</h2>`) {
		t.Error("open flag lost")
	}
	// a new section the manifest lists without a file, and a file it does not list
	src2 := fixture(t)
	writeIn(t, src2, "sections/extra.html", "<p>x</p>\n")
	if p := problemsText(build(t, tm, src2, false)); !strings.Contains(p, "sections/extra.html: not listed in sections.json") {
		t.Errorf("unlisted file: %s", p)
	}
	src3 := fixture(t)
	replaceIn(t, src3, "sections.json", `{"id":"glossary"`, `{"id":"extra","title":"Extra"},{"id":"glossary"`)
	if p := problemsText(build(t, tm, src3, false)); !strings.Contains(p, `section "extra" has no file sections/extra.html`) {
		t.Errorf("listed without file: %s", p)
	}
}

func TestBuildReportEscapesJSONForScript(t *testing.T) {
	tm := loadTemplate(t)
	src := fixture(t)
	replaceIn(t, src, "data/summary-data.json", `"1k"`, `"1k"`)
	replaceIn(t, src, "data/summary-data.json", `"lines of Go"`, `"lines of Go; the page writes &lt;/b&gt; and </b><!-- x -->"`)
	res := build(t, tm, src, false)
	if len(res.problems) > 0 {
		t.Fatalf("%s", problemsText(res))
	}
	page := string(res.page)
	if strings.Contains(page, `"lines of Go; the page writes &lt;/b&gt; and </b>`) {
		t.Error("</ inside JSON must be written as <\\/")
	}
	if !strings.Contains(page, `and <\/b><!-- x -->`) {
		t.Errorf("escapes missing:\n%s", reJSONBlock.FindString(page)[:300])
	}
	// the page's check reads the same text back
	if cr := checkReport(page, defaultAllowSet(), nil, false); !cr.Passed {
		t.Errorf("%+v", cr.Failures)
	}
}

func TestBuildReportErrors(t *testing.T) {
	tm := loadTemplate(t)
	cases := []struct {
		name   string
		mutate func(t *testing.T, dir string)
		want   []string
	}{
		{"data file missing", func(t *testing.T, d string) { os.Remove(filepath.Join(d, "data", "quality-data.json")) },
			[]string{`data/quality-data.json: missing (the template has a data block "quality-data")`}},
		{"template section missing from the manifest", func(t *testing.T, d string) {
			replaceIn(t, d, "sections.json", `    {"id":"glossary","title":"Glossary"}`, `    {"id":"x1","title":"X"}`)
			writeIn(t, d, "sections/x1.html", "<p>x</p>")
			os.Remove(filepath.Join(d, "sections", "glossary.html"))
		}, []string{`the template's section "glossary" is missing`}},
		{"section file missing", func(t *testing.T, d string) { os.Remove(filepath.Join(d, "sections", "how.html")) },
			[]string{`section "how" has no file sections/how.html`}},
		{"manifest missing", func(t *testing.T, d string) { os.Remove(filepath.Join(d, "sections.json")) },
			[]string{`sections.json: missing`}},
		{"manifest typo", func(t *testing.T, d string) { replaceIn(t, d, "sections.json", `"nav":"Plan"`, `"navigation":"Plan"`) },
			[]string{`unknown field "navigation"`}},
		{"manifest repeats an id", func(t *testing.T, d string) {
			replaceIn(t, d, "sections.json", `{"id":"glossary"`, `{"id":"plan","title":"Again"},{"id":"glossary"`)
		}, []string{`section "plan": listed twice`}},
		{"bad asof", func(t *testing.T, d string) {
			replaceIn(t, d, "sections.json", `"asof":"2026-11-13"`, `"asof":"13 Nov"`)
		},
			[]string{`asof "13 Nov" is not an ISO date`}},
		{"stray file", func(t *testing.T, d string) { writeIn(t, d, "notes.txt", "x") },
			[]string{`notes.txt: matches no slot`}},
		{"stray directory", func(t *testing.T, d string) { writeIn(t, d, "shots/a.png", "x") },
			[]string{`shots/a.png: matches no slot`}},
		{"data file for no block", func(t *testing.T, d string) { writeIn(t, d, "data/extra-data.json", "{}") },
			[]string{`data/extra-data.json: matches no slot; the template's data blocks are meta-data, summary-data`}},
		{"section file with the wrong extension", func(t *testing.T, d string) { writeIn(t, d, "sections/plan.md", "x") },
			[]string{`sections/plan.md: matches no slot`}},
		{"json does not parse", func(t *testing.T, d string) {
			replaceIn(t, d, "data/meta-data.json", `"repo": "org/tiny",`, "\"repo\": \"org/tiny\"\n \"ref\": 1,")
		}, []string{`data/meta-data.json: line 4, column`}},
		{"json repeats a key", func(t *testing.T, d string) {
			replaceIn(t, d, "data/bars-data.json", `"owners": {`, `"loc": {"rows": []}, "owners": {`)
		}, []string{`data/bars-data.json: the key "loc" appears twice`}},
		{"script in a string", func(t *testing.T, d string) {
			replaceIn(t, d, "data/summary-data.json", `"<b>No CI.</b>`, `"<script>x()</script>`)
		}, []string{`contains <script`}},
		{"javascript link", func(t *testing.T, d string) {
			replaceIn(t, d, "data/summary-data.json", `"<b>One deployer.</b>`, `"<a href=\"javascript:x()\">One deployer.</a>`)
		}, []string{`contains javascript:`}},
		{"tag the page strips", func(t *testing.T, d string) {
			replaceIn(t, d, "data/summary-data.json", `"<b>No alerts.</b>`, `"<div>No alerts.</div>`)
		}, []string{`<div> is stripped by the page`}},
		{"link kind the page strips", func(t *testing.T, d string) {
			replaceIn(t, d, "data/summary-data.json", `"<b>No alerts.</b>`, `"<a href=\"/x\">No alerts.</a>`)
		}, []string{`keeps only # and https:// links`}},
		{"four facts", func(t *testing.T, d string) {
			s := readIn(t, d, "data/summary-data.json")
			var v map[string]any
			_ = json.Unmarshal([]byte(s), &v)
			v["facts"] = v["facts"].([]any)[:4]
			b, _ := json.Marshal(v)
			writeIn(t, d, "data/summary-data.json", string(b))
		}, []string{`summary-data.json: needs exactly 5 facts, has 4`}},
		{"unknown severity", func(t *testing.T, d string) {
			replaceIn(t, d, "data/summary-data.json", `"sev": "crit"`, `"sev": "critical"`)
		}, []string{`risks[0].sev must be crit, high, med, good or low`}},
		{"scoreboard row without how", func(t *testing.T, d string) {
			replaceIn(t, d, "data/scoreboard-data.json", `"how": "go build ./..."`, `"hows": "go build ./..."`)
		}, []string{`row "Production build" has no how`}},
		{"commits series length", func(t *testing.T, d string) {
			replaceIn(t, d, "data/commits-data.json", `40`, `40, 41`)
		}, []string{`chart "commits": series 1 has 4 values for 3 months`}},
		{"bars colour", func(t *testing.T, d string) { replaceIn(t, d, "data/bars-data.json", `"b3"`, `"purple"`) },
			[]string{`colour c "purple"`}},
		{"quality level", func(t *testing.T, d string) { replaceIn(t, d, "data/quality-data.json", `"level": 2`, `"level": 7`) },
			[]string{`autonomy.level must be a number from 0 to 5`}},
		{"unbalanced div", func(t *testing.T, d string) {
			replaceIn(t, d, "sections/delivery.html", `<div class="signals">`, `<div class="signals"><div>`)
		},
			[]string{`sections/delivery.html:1: <div> is never closed`}},
		{"stray close", func(t *testing.T, d string) { writeIn(t, d, "sections/production.html", "<p>x</p></div>\n") },
			[]string{`sections/production.html:1: </div> closes nothing`}},
		{"script element", func(t *testing.T, d string) {
			writeIn(t, d, "sections/production.html", "<p>x</p><script>alert(1)</script>\n")
		},
			[]string{`sections/production.html:1: <script> is not allowed in a section`}},
		{"event handler", func(t *testing.T, d string) {
			writeIn(t, d, "sections/production.html", `<p><img src="x.png" onerror="alert(1)"></p>`+"\n")
		}, []string{`event handler attribute onerror=`}},
		{"javascript href in a section", func(t *testing.T, d string) {
			writeIn(t, d, "sections/production.html", `<p><a href=" JavaScript:alert(1)">x</a></p>`+"\n")
		}, []string{`would run script`}},
		{"broken link in a section", func(t *testing.T, d string) {
			writeIn(t, d, "sections/production.html", `<p>See <a href="#nowhere">x</a>.</p>`+"\n")
		}, []string{`sections/production.html:1: link #nowhere points at no element`}},
		{"broken link in a data string", func(t *testing.T, d string) {
			replaceIn(t, d, "data/summary-data.json", `href=\"#plan\"`, `href=\"#plans\"`)
		}, []string{`data/summary-data.json at actions[0]: link #plans points at no element`}},
		{"broken link in the changelog", func(t *testing.T, d string) {
			replaceIn(t, d, "changelog.txt", `<code>abc1234</code>`, `<a href="#gone">abc1234</a>`)
		}, []string{`changelog.txt: link #gone points at no element`}},
		{"duplicate id across sections", func(t *testing.T, d string) {
			writeIn(t, d, "sections/production.html", `<h3 id="how-repo">x</h3>`+"\n")
		}, []string{`sections/production.html:1: id "how-repo" is already used by sections/how.html`}},
		{"id of the page", func(t *testing.T, d string) { writeIn(t, d, "sections/production.html", `<h3 id="tip">x</h3>`+"\n") },
			[]string{`id "tip" is already used by the page's own markup`}},
		{"mermaid with markup", func(t *testing.T, d string) {
			replaceIn(t, d, "sections/how.html", "U->>S: GET /", "U->>S: GET <b>/</b>")
		}, []string{`a mermaid diagram holds a <b> tag; write < as &lt;`}},
		{"missing mount", func(t *testing.T, d string) {
			replaceIn(t, d, "sections/quality.html", `<div class="axes" id="axes"></div>`, `<div class="axes"></div>`)
		}, []string{`data/quality-data.json: no section has an element with id="axes"`}},
		{"missing treemap mount", func(t *testing.T, d string) { replaceIn(t, d, "sections/how.html", ` id="tm-ramp"`, ``) },
			[]string{`no section has an element with id="tm-ramp"`}},
		{"bars key without element", func(t *testing.T, d string) {
			replaceIn(t, d, "sections/delivery.html", `<div class="bars" id="owners"></div>`, ``)
		}, []string{`data/bars-data.json: key "owners" has no element with id="owners"`}},
		{"bars element without key", func(t *testing.T, d string) {
			replaceIn(t, d, "sections/delivery.html", `<div class="bars" id="owners"></div>`, `<div class="bars" id="owners"></div><div class="bars" id="more"></div>`)
		}, []string{`bars chart #more has no key "more"`}},
		{"chart without element", func(t *testing.T, d string) {
			replaceIn(t, d, "sections/delivery.html", `<div id="commits"></div>`, ``)
		}, []string{`chart "commits" has no element with id="commits"`}},
		{"changelog line", func(t *testing.T, d string) { replaceIn(t, d, "changelog.txt", `2026-11-13 First`, `13 Nov First`) },
			[]string{`changelog.txt:2: a line is a date`}},
		{"changelog order", func(t *testing.T, d string) {
			replaceIn(t, d, "changelog.txt", "note:", "2026-11-14 Later entry below.\nnote:")
		}, []string{`2026-11-14 is listed below 2026-11-13; newest first`}},
		{"changelog without note", func(t *testing.T, d string) {
			writeIn(t, d, "changelog.txt", "2026-11-13 First baseline.\n")
		}, []string{`changelog.txt: has no note: line`}},
		{"changelog markup", func(t *testing.T, d string) {
			replaceIn(t, d, "changelog.txt", `<code>abc1234</code>`, `<code>abc1234`)
		},
			[]string{`changelog.txt: <code> is never closed`}},
		{"dummy comment", func(t *testing.T, d string) {
			replaceIn(t, d, "sections/production.html", `<p class="stub">`, "<!-- DUMMY: replace -->\n<p class=\"stub\">")
		}, []string{`still holding the template's example content: sections/production.html (a DUMMY comment)`}},
		{"dummy data", func(t *testing.T, d string) {
			writeIn(t, d, "data/meta-data.json", tm.block("meta-data").JSON)
		}, []string{`data/meta-data.json (equals the example data)`}},
		{"scoreboard note left at the example", func(t *testing.T, d string) {
			replaceIn(t, d, "data/scoreboard-data.json", `"note": "Snapshot 1, measured on 2026-11-13.",`, ``)
			replaceIn(t, d, "sections/scoreboard.html", "Snapshot 1.", "Snapshot 1 of N. Delta is against the first snapshot.")
		}, []string{`sections/scoreboard.html (the "Snapshot 1 of N" line`}},
		{"missing source folder", func(t *testing.T, d string) { os.RemoveAll(d) },
			[]string{`source folder`, `--init`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fixture(t)
			c.mutate(t, dir)
			res := build(t, tm, dir, false)
			if len(res.problems) == 0 || res.page != nil {
				t.Fatalf("expected problems and no page, got %v (page %d bytes)", res.problems, len(res.page))
			}
			got := problemsText(res)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestBuildReportScoreboardFromFile(t *testing.T) {
	tm := loadTemplate(t)
	src := fixture(t)
	sbFile := filepath.Join(filepath.Dir(src), "scoreboard-tiny.json")
	writeIn(t, filepath.Dir(src), "scoreboard-tiny.json", `{"schema_version":1,"tool":{"name":"codebase-baseline","version":"0.1.0"},"note":"2 snapshots.","snapshots":[{"file":"a.json"}],
 "rows":[
  {"metric":"Tests (go)","value":12,"first":10,"better":"up","trend":[10,12],"how":"go test ./...","source":"loops.loops[id=go-test].parsed.passed","asOf":"2026-11-13","unit":"tests","headline":true},
  {"metric":"Lint warnings","value":null,"first":null,"better":"down","trend":null,"how":"golangci-lint run","source":"x","asOf":"2026-11-13","headline":false}
 ]}`)
	// relative pointer, with its own note
	writeIn(t, src, "data/scoreboard-data.json", `{"from": "../scoreboard-tiny.json", "note": "Snapshot 2 of 2."}`)
	res := build(t, tm, src, false)
	if len(res.problems) > 0 {
		t.Fatalf("%s", problemsText(res))
	}
	block := ""
	for _, m := range reJSONBlock.FindAllStringSubmatch(string(res.page), -1) {
		if m[1] == "scoreboard-data" {
			block = m[2]
		}
	}
	want := `{"note":"Snapshot 2 of 2.","rows":[{"metric":"Tests (go)","value":12,"first":10,"better":"up","trend":[10,12],"how":"go test ./...","asOf":"2026-11-13","unit":"tests"},{"metric":"Lint warnings","value":null,"first":null,"better":"down","trend":[],"how":"golangci-lint run","asOf":"2026-11-13"}]}`
	if strings.TrimSpace(block) != want {
		t.Errorf("scoreboard block:\n%s\nwant\n%s", block, want)
	}
	// the file's own note when the pointer has none; an absolute path
	writeIn(t, src, "data/scoreboard-data.json", `{"from": "`+sbFile+`"}`)
	res = build(t, tm, src, false)
	if len(res.problems) > 0 || !strings.Contains(string(res.page), `"note":"2 snapshots."`) {
		t.Errorf("file note: %v", res.problems)
	}
	// a missing file, an unknown key, a file that is not a scoreboard
	writeIn(t, src, "data/scoreboard-data.json", `{"from": "../nope.json"}`)
	if p := problemsText(build(t, tm, src, false)); !strings.Contains(p, "data/scoreboard-data.json: from ../nope.json:") {
		t.Errorf("missing file: %s", p)
	}
	writeIn(t, src, "data/scoreboard-data.json", `{"from": "../scoreboard-tiny.json", "rows": []}`)
	if p := problemsText(build(t, tm, src, false)); !strings.Contains(p, `unknown field "rows"`) {
		t.Errorf("unknown key: %s", p)
	}
	writeIn(t, filepath.Dir(src), "other.json", `{"hello": 1}`)
	writeIn(t, src, "data/scoreboard-data.json", `{"from": "../other.json"}`)
	if p := problemsText(build(t, tm, src, false)); !strings.Contains(p, "no rows") {
		t.Errorf("not a scoreboard: %s", p)
	}
}

func TestBuildReportSeveralCharts(t *testing.T) {
	tm := loadTemplate(t)
	src := fixture(t)
	chart := readIn(t, src, "data/commits-data.json")
	second := strings.Replace(chart, `"id": "commits"`, `"id": "commits-agents"`, 1)
	writeIn(t, src, "data/commits-data.json", "["+chart+","+second+"]")
	if p := problemsText(build(t, tm, src, false)); !strings.Contains(p, `chart "commits-agents" has no element with id="commits-agents"`) {
		t.Errorf("second chart needs its element: %s", p)
	}
	replaceIn(t, src, "sections/delivery.html", `<div id="commits"></div>`, `<div id="commits"></div><div id="commits-agents"></div>`)
	if res := build(t, tm, src, false); len(res.problems) > 0 {
		t.Errorf("two charts: %s", problemsText(res))
	}
}

func TestStripHTMLComments(t *testing.T) {
	in := "a\n  <!-- own line -->\nb <!-- inline --> c\n<script>var s = '<!-- kept -->';</script>\n<style>/* <!-- kept --> */</style>\n<!-- multi\nline -->\nend"
	want := "a\nb  c\n<script>var s = '<!-- kept -->';</script>\n<style>/* <!-- kept --> */</style>\nend"
	if got := stripHTMLComments(in); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestCheckBalance(t *testing.T) {
	ok := []string{
		`<p>a<br>b <img src="x"> <input></p>`,
		`<ul><li>a<li>b</ul>`,
		`<table><thead><tr><th>a<th>b<tbody><tr><td>1<td>2<tr><td>3</table>`,
		`<dl><dt>a<dd>b<dt>c<dd>d</dl>`,
		`<p>one<div>two</div>`,
		`<svg viewBox="0 0 1 1"><rect x="1"/><text>a<tspan>b</tspan></text></svg>`,
		`<pre class="mermaid">A->>B: hi &lt;x&gt;</pre>`,
		`text with a < b and 1<2`,
		`<!-- <div> in a comment -->`,
		`</p>`,
	}
	for _, s := range ok {
		if p := checkBalance(s); len(p) != 0 {
			t.Errorf("%q flagged: %v", s, p)
		}
	}
	bad := map[string]string{
		`<div><p>a</div></div>`:   "</div> closes nothing",
		`<div><span>a</div>`:      "<span> is not closed before </div>",
		"<div>\n<b>a</div>":       "<b> is not closed",
		`<section><div>a`:         "<div> is never closed",
		`<a href="x">a`:           "<a> is never closed",
		`<svg><line x="1"></svg>`: "<line> is not closed before </svg>",
	}
	for s, want := range bad {
		var msgs []string
		for _, p := range checkBalance(s) {
			msgs = append(msgs, p.msg)
		}
		if !strings.Contains(strings.Join(msgs, "|"), want) {
			t.Errorf("%q: want %q, got %v", s, want, msgs)
		}
	}
}

func TestParseReportTemplateSlots(t *testing.T) {
	tm := loadTemplate(t)
	var ids []string
	for _, s := range tm.sections {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids, ","); got != "what,summary,scoreboard,how,quality,delivery,production,plan,sources,glossary" {
		t.Errorf("sections: %s", got)
	}
	var bl []string
	for _, b := range tm.blocks {
		bl = append(bl, b.ID)
	}
	if got := strings.Join(bl, ","); got != "meta-data,summary-data,scoreboard-data,treemap-data,commits-data,bars-data,quality-data" {
		t.Errorf("blocks: %s", got)
	}
	if s := tm.section("quality"); s == nil || s.Title != "Quality & factory readiness" || s.Nav != "Quality & readiness" || s.Open {
		t.Errorf("quality: %+v", s)
	}
	if s := tm.section("what"); s == nil || !s.Open || s.Nav != "" {
		t.Errorf("what: %+v", s)
	}
	if len(tm.log) != 1 || tm.note == "" {
		t.Errorf("footer: %v %q", tm.log, tm.note)
	}
	if _, err := parseReportTemplate("<html></html>"); err == nil {
		t.Error("a page without slots is not a template")
	}
}

func TestBuildReportTreemapFromFile(t *testing.T) {
	tm := loadTemplate(t)
	src := fixture(t)
	writeIn(t, filepath.Dir(src), "treemap-tiny.json", `{"schema_version":1,"churnLabel":"commits, last 180 days","summary":{"files":2},"hotspots":[{"path":"main.go"}],
 "tree":{"name":"tiny","children":[{"name":"main.go","path":"main.go","loc":800,"churn":40,"complexity":12,"lang":"go"}]}}`)
	writeIn(t, src, "data/treemap-data.json", `{"from": "../treemap-tiny.json"}`)
	res := build(t, tm, src, false)
	if len(res.problems) > 0 {
		t.Fatalf("%s", problemsText(res))
	}
	block := ""
	for _, m := range reJSONBlock.FindAllStringSubmatch(string(res.page), -1) {
		if m[1] == "treemap-data" {
			block = strings.TrimSpace(m[2])
		}
	}
	want := `{"name":"tiny","churnLabel":"commits, last 180 days","children":[{"name":"main.go","path":"main.go","loc":800,"churn":40,"complexity":12,"lang":"go"}]}`
	if block != want {
		t.Errorf("treemap block:\n%s\nwant\n%s", block, want)
	}
	writeIn(t, src, "data/treemap-data.json", `{"from": "../treemap-tiny.json", "churnLabel": "my label"}`)
	if res := build(t, tm, src, false); len(res.problems) > 0 || !strings.Contains(string(res.page), `"churnLabel":"my label"`) {
		t.Errorf("label override: %v", res.problems)
	}
	writeIn(t, src, "data/treemap-data.json", `{"from": "../other.json"}`)
	if p := problemsText(build(t, tm, src, false)); !strings.Contains(p, "from ../other.json:") {
		t.Errorf("missing file: %s", p)
	}
	writeIn(t, src, "data/quality-data.json", `{"from": "../x.json"}`)
	if p := problemsText(build(t, tm, src, false)); !strings.Contains(p, `"from" works only in scoreboard-data.json and treemap-data.json`) {
		t.Errorf("from elsewhere: %s", p)
	}
}
