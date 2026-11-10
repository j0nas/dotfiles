package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func defaultAllowSet() map[string]bool {
	m := map[string]bool{}
	for _, t := range defaultAllow {
		m[t] = true
	}
	return m
}

func failuresOf(r reportResult, check string) []reportFailure {
	var out []reportFailure
	for _, f := range r.Failures {
		if f.Check == check {
			out = append(out, f)
		}
	}
	return out
}

const termCheck = "identifier-like term left undefined"

func TestFindTokens(t *testing.T) {
	var got []string
	for _, tok := range findTokens("R3.3b ships after PE-13 and ADR-0008; v2-beta uses NextAuth, the JWT and APIs and iPhone but not Normal words.") {
		got = append(got, tok.Kind+":"+tok.Norm)
	}
	want := "label:R3.3b ticket:PE-13 ticket:ADR-0008 version label:v2-beta CamelCase:NextAuth acronym:JWT acronym:API CamelCase:iPhone"
	if strings.Join(got, " ") != want {
		t.Errorf("tokens = %v\nwant     %s", got, want)
	}
}

func TestCheckReportDefinitions(t *testing.T) {
	src := `<html><head><title>T HTTP</title></head><body>
<p>The <abbr title="Release three">R3</abbr> migration, then R3 again.</p>
<p>The <dfn>EDA</dfn> style and later EDA.</p>
<p>Sync with Zookeeper<sup class="g"><a href="#g-zk">?</a></sup> and ZK.</p>
<p>Uses the API over HTTPS with a JWT.</p>
<p>See <code>FOOBAR</code> and <pre>BAZQUX_1</pre>.</p>
<p>Bare QUUX here, QUUX twice.</p>
<p>The NextAuth hook and R9.</p>
<section id="glossary"><h2>Glossary</h2><dl>
<dt id="g-zk">ZK</dt><dd>Zookeeper, a coordination service</dd>
</dl></section>
<script>var SCRIPTY = 1;</script>
</body></html>`
	res := checkReport(src, defaultAllowSet(), nil, false)
	got := map[string]int{}
	for _, f := range failuresOf(res, termCheck) {
		got[f.Token]++
	}
	for _, bad := range []string{"QUUX", "NextAuth", "R9", "ZK"} {
		if got[bad] != 1 {
			t.Errorf("%s should be reported exactly once, got %d (%v)", bad, got[bad], got)
		}
	}
	for _, ok := range []string{"R3", "EDA", "API", "HTTPS", "JWT", "FOOBAR", "BAZQUX_1", "SCRIPTY", "HTTP"} {
		if got[ok] != 0 {
			t.Errorf("%s should pass, reported %d times", ok, got[ok])
		}
	}
	strictRes := checkReport(src, defaultAllowSet(), nil, true)
	n := 0
	for _, f := range failuresOf(strictRes, termCheck) {
		if f.Token == "QUUX" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("strict mode should report both QUUX occurrences, got %d", n)
	}
}

func TestCheckReportSessionLeaks(t *testing.T) {
	src := `<body><p>As discussed, the plan changed. See <a href="#p">the plan</a>. You asked for this session's notes.</p><p id="p">x</p></body>`
	res := checkReport(src, defaultAllowSet(), nil, false)
	leaks := failuresOf(res, "session-leak phrases")
	var toks []string
	for _, f := range leaks {
		toks = append(toks, strings.ToLower(f.Token))
	}
	got := strings.Join(toks, "|")
	for _, want := range []string{"as discussed", "the plan", "you asked", "this session"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing leak %q in %q", want, got)
		}
	}
	if n := strings.Count(got, "the plan"); n != 1 {
		t.Errorf("only the unlinked 'the plan' should be flagged, got %d in %q", n, got)
	}
}

func TestCheckReportLinksAndGlossary(t *testing.T) {
	src := `<body>
<nav><a href="#intro">Intro</a> <a href="#nowhere">Gone</a></nav>
<h2 id="intro">Intro</h2>
<p>Term<sup class="g"><a href="#g-a">1</a></sup> and Other<sup class="g"><a href="#g-missing">2</a></sup> and Bare<sup class="g">x</sup></p>
<section class="glossary"><dl>
<dt id="g-a">A</dt><dd>first</dd>
<dt id="g-unused">U</dt><dd>never linked</dd>
</dl></section></body>`
	res := checkReport(src, defaultAllowSet(), nil, false)
	msgs := ""
	for _, f := range failuresOf(res, "broken links and glossary") {
		msgs += f.Message + "\n"
	}
	for _, want := range []string{`href="#nowhere" has no element`, `"#g-missing" points at a missing glossary entry`, `marker without an in-page link`, `glossary entry "g-unused" is never referenced`} {
		if !strings.Contains(msgs, want) {
			t.Errorf("missing %q in:\n%s", want, msgs)
		}
	}
	if strings.Contains(msgs, `"g-a"`) || strings.Contains(msgs, "#intro") {
		t.Errorf("valid links flagged:\n%s", msgs)
	}
	noGloss := checkReport(`<p>Term<sup class="g"><a href="#x">1</a></sup></p>`, defaultAllowSet(), nil, false)
	hasMsg := false
	for _, f := range failuresOf(noGloss, "broken links and glossary") {
		hasMsg = hasMsg || strings.Contains(f.Message, "no glossary section")
	}
	if !hasMsg {
		t.Errorf("sup markers without a glossary should fail: %+v", noGloss.Failures)
	}
	if clean := checkReport(`<body><h1>Hello</h1><p>Plain words only.</p></body>`, defaultAllowSet(), nil, false); !clean.Passed {
		t.Errorf("clean report failed: %+v", clean.Failures)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestHarvestRepoLabels(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/plan.md", "# Plan\n\nR3.3b drops the old columns. Phase 2 follows. See ADR-0008. Ask Zorblax about Zorblax twice, and Zorblax again. Uses S3 and AES-256 and P2021.\n")
	write("README.md", "Stage 4 of the rollout.\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "Add: PE-13 first cut of v2-beta")
	gitRun(t, dir, "branch", "claude/r3-7-brief-a1292e")
	h := harvestRepo(dir, defaultAllowSet())
	for _, want := range []string{"R3.3b", "Phase 2", "ADR-0008", "Stage 4", "PE-13", "v2-beta", "R3", "R3.7"} {
		if !contains(h.Labels, want) {
			t.Errorf("label %q not harvested: %v", want, h.Labels)
		}
	}
	for _, bad := range []string{"S3", "AES-256", "P2021"} {
		if contains(h.Labels, bad) {
			t.Errorf("%q is noise, harvested anyway", bad)
		}
	}
	if !contains(h.Codenames, "Zorblax") {
		t.Errorf("codename Zorblax not harvested: %v", h.Codenames)
	}
	// The harvested labels then fail a report that uses them bare.
	res := checkReport(`<p>R3.3b is next, after Phase 2 and PE-13. Zorblax decides.</p><p>Later, <abbr title="x">Phase 2</abbr>.</p>`, defaultAllowSet(), &h, false)
	var found []string
	for _, f := range failuresOf(res, "repo label or codename left undefined") {
		found = append(found, f.Token)
	}
	got := strings.Join(found, ",")
	for _, want := range []string{"R3.3b", "Phase 2", "PE-13", "Zorblax"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s should be flagged: %s", want, got)
		}
	}
	if strings.Contains(got, "R3,") || strings.HasSuffix(got, ",R3") {
		t.Errorf("R3 inside R3.3b is the same label, not a second finding: %s", got)
	}
}

func TestWordIndexes(t *testing.T) {
	if got := wordIndexes("R3 and R3.3b and R3. and xR3 and R3-1", "R3"); len(got) != 2 || got[0] != 0 || got[1] != 17 {
		t.Errorf("indexes = %v", got)
	}
}
