package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		fp := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func commitAll(t *testing.T, repo, msg string) {
	t.Helper()
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", msg)
}

func TestGitWrapperIgnoresHostileConfig(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	writeFiles(t, repo, map[string]string{"a.txt": "a"})
	commitAll(t, repo, "init")
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "hook.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	gitRun(t, repo, "config", "core.fsmonitor", script)
	gitRun(t, repo, "config", "core.hooksPath", filepath.Dir(script))
	if _, err := git(repo, "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "log", "-1", "--format=%H"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a hostile core.fsmonitor ran through the git wrapper")
	}
	args := strings.Join(gitSafeArgs("x"), " ")
	for _, want := range []string{"core.fsmonitor=false", "core.hooksPath=/dev/null", "protocol.file.allow=user", "--no-optional-locks"} {
		if !strings.Contains(args, want) {
			t.Errorf("gitSafeArgs lacks %s", want)
		}
	}
}

func TestExportHardening(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	writeFiles(t, repo, map[string]string{
		"src/main.go": "package main\n", "tests/a_test.go": "package main\n", ".gitattributes": "tests/ export-ignore\n.github/ export-ignore\n",
		".github/workflows/ci.yml": "on: push\n", ".env": "SECRET=1\n", ".env.local": "X=1\n", ".env.example": "A=\n", ".env.sample": "A=\n",
		"keys/server.pem": "-----BEGIN\n", "keys/id_rsa": "k", "keys/id_rsa.pub": "p", "app.keystore": "k", "creds/credentials.json": "{}", "cert.p12": "x", "cert.pfx": "x", "server.key": "x",
		"Docs/guide.md": "x\n", "ADR/0001.md": "y\n", "site/content/post.md": "a post that is content\n", "README.md": "hi\n", "prompts/system.md": "prompt\n",
	})
	if err := os.Symlink("src/main.go", filepath.Join(repo, "link.go")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repo, "init")
	idxBefore, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
	sumBefore := sha256.Sum256(idxBefore)

	var so, se strings.Builder
	a := &app{stdout: &so, stderr: &se, now: timeNow}
	dest := filepath.Join(t.TempDir(), "out")
	if code := a.run([]string{"export", repo, dest, "--strip-docs", "--keep", "site/content/**", "--keep", "prompts/*.md"}); code != 0 {
		t.Fatalf("export: %s", se.String())
	}
	idxAfter, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if sha256.Sum256(idxAfter) != sumBefore {
		t.Error("the repo's own index was touched")
	}
	got := strings.Join(walkFiles(dest), ",")
	for _, want := range []string{"tests/a_test.go", ".github/workflows/ci.yml", "src/main.go", ".env.example", ".env.sample", "site/content/post.md", "prompts/system.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s should be exported (export-ignore must not drop it, kept docs stay): %s", want, got)
		}
	}
	for _, gone := range []string{".env,", ".env.local", "server.pem", "id_rsa", "app.keystore", "credentials.json", "cert.p12", "cert.pfx", "server.key", "link.go", "Docs/guide.md", "ADR/0001.md", "README.md"} {
		if strings.Contains(got+",", gone) {
			t.Errorf("%s must not be exported: %s", gone, got)
		}
	}
	list, _ := os.ReadFile(dest + ".removed.txt")
	for _, want := range []string{"link.go\tsymlink", ".env\tenv file", "keys/id_rsa\tssh key", "keys/server.pem\tkey or certificate file", "creds/credentials.json\tcredentials file", "Docs/guide.md\tdoc directory", "ADR/0001.md\tdoc directory", "README.md\tprose doc"} {
		if !strings.Contains(string(list), want) {
			t.Errorf("removed list lacks %q:\n%s", want, list)
		}
	}
	// Without --strip-docs the secret-like files and symlinks still go, and the list says so.
	dest2 := filepath.Join(t.TempDir(), "out2")
	if code := a.run([]string{"export", repo, dest2}); code != 0 {
		t.Fatalf("export 2: %s", se.String())
	}
	if _, err := os.Stat(filepath.Join(dest2, ".env")); err == nil {
		t.Error(".env exported without --strip-docs")
	}
	if _, err := os.Stat(filepath.Join(dest2, "README.md")); err != nil {
		t.Error("README.md should stay without --strip-docs")
	}
	if l, _ := os.ReadFile(dest2 + ".removed.txt"); !strings.Contains(string(l), "link.go\tsymlink") {
		t.Errorf("removed list = %s", l)
	}
}

func TestVendorDirRules(t *testing.T) {
	files := []string{"package.json", "src/app.ts", "build/out.js", "dist/bundle.js", "vendor/lib.go", "node_modules/x/i.js",
		"web/.venv/lib/x.py", "tools/build/gen.go", "pkg/service/vendor/x.go", "packages/a/package.json", "packages/a/dist/i.js", "packages/a/src/dist/real.ts", "coverage/l.json"}
	got := dropVendored(append([]string(nil), files...))
	want := map[string]bool{"package.json": true, "src/app.ts": true, "tools/build/gen.go": true, "pkg/service/vendor/x.go": true, "packages/a/package.json": true, "packages/a/src/dist/real.ts": true}
	if len(got) != len(want) {
		t.Errorf("kept = %v", got)
	}
	for _, f := range got {
		if !want[f] {
			t.Errorf("%s should have been dropped", f)
		}
	}
	// node_modules, .venv and bower_components go anywhere.
	if !isVendoredPath("deep/er/node_modules/a/b.js") || !isVendoredPath("a/.venv/b.py") || !isVendoredPath("a/bower_components/b.js") {
		t.Error("strong vendor dirs must match at any depth")
	}
}

func TestDiscoverDeployToolsAndHooks(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"package.json": `{"name":"x","scripts":{
 "build":"npm run prep && next build",
 "prep":"vercel pull --yes",
 "lint":"eslint .",
 "pretest":"sentry-cli releases new 1",
 "test":"vitest run",
 "typecheck":"tsc --noEmit",
 "release":"npm publish",
 "test:db":"prisma migrate deploy && vitest"
}}`,
		"pnpm-lock.yaml": "x",
	})
	res := discoverLoops(dir, timeNow())
	by := map[string]loop{}
	for _, l := range res.Loops {
		by[l.ID] = l
	}
	if l := by["js-build"]; l.Enabled || !contains(l.Needs, "deploy-tool:vercel") {
		t.Errorf("build calls vercel through a nested run: %+v", l)
	}
	if l := by["js-test"]; l.Enabled || !contains(l.Needs, "deploy-tool:sentry-cli") {
		t.Errorf("test has a pretest hook with sentry-cli: %+v", l)
	}
	if l := by["js-test-db"]; l.Enabled || !contains(l.Needs, "deploy-tool:prisma-migrate-deploy") {
		t.Errorf("test:db = %+v", l)
	}
	if l := by["js-lint"]; !l.Enabled {
		t.Errorf("lint is clean: %+v", l)
	}
	if got := deployToolsIn("aws s3 sync . s3://x; az login; docker push a/b; gh release create; terraform apply; flyctl deploy; wrangler publish; heroku logs"); len(got) != 8 {
		t.Errorf("deploy tools = %v", got)
	}
	if len(deployToolsIn("pnpm run build && echo awsome")) != 0 {
		t.Error("false positive on plain words")
	}
}

func TestDiscoverExternalConfig(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"svc/pom.xml": "<project/>", "svc/src/main/resources/application.yml": "spring:\n  datasource:\n    url: jdbc:postgresql://prod-db.corp-internal.io:5432/app\n",
		"local/pom.xml": "<project/>", "local/src/main/resources/application.yml": "spring:\n  datasource:\n    url: jdbc:postgresql://localhost:5432/app\n# url: https://docs.example.com\n",
		"api/package.json": `{"scripts":{"test":"vitest"}}`, "api/.env.example": "API=https://api.stripe.com/v1\nDB=postgres://user:pw@db.neon.tech/x\n",
		"svc/pom.xml.bak": "", "web/package.json": `{"scripts":{"test":"vitest"}}`, "web/.env.example": "URL=http://localhost:3000\nHOST=postgres://u:p@postgres:5432/x\nDOC=https://www.w3.org/2000/svg\n",
	})
	res := discoverLoops(dir, timeNow())
	hits := map[string]string{}
	for _, h := range res.Signals.ExternalConfig {
		hits[h.File] = h.Scheme + "://" + h.Host
		if strings.Contains(h.Host, "pw") || strings.Contains(h.Host, "@") {
			t.Errorf("credentials leaked into the host: %+v", h)
		}
	}
	if hits["svc/src/main/resources/application.yml"] != "jdbc://prod-db.corp-internal.io" || hits["api/.env.example"] == "" {
		t.Errorf("hits = %v", hits)
	}
	if _, ok := hits["local/src/main/resources/application.yml"]; ok {
		t.Errorf("localhost config flagged: %v", hits)
	}
	if _, ok := hits["web/.env.example"]; ok {
		t.Errorf("service names, localhost and schema URLs are local: %v", hits)
	}
	for _, l := range res.Loops {
		switch {
		case l.Kind == "test" && strings.HasPrefix(l.Cwd, "svc") || l.Kind == "test" && l.Cwd == "api":
			if l.Enabled || !strings.Contains(strings.Join(l.Needs, ","), "external-config:") {
				t.Errorf("%s should be off, needs %v", l.ID, l.Needs)
			}
		case l.Kind == "test" && (l.Cwd == "local" || l.Cwd == "web"):
			if !l.Enabled {
				t.Errorf("%s should stay on: %+v", l.ID, l)
			}
		}
	}
}

func TestDiscoverTruncationAndCaps(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 45; i++ {
		files[fmt.Sprintf("svc%02d/go.mod", i)] = "module x\n\ngo 1.22\n"
	}
	writeFiles(t, dir, files)
	res := discoverLoops(dir, timeNow())
	if len(res.Truncated) != 1 || !strings.Contains(res.Truncated[0], "go: 45 projects") || !strings.Contains(res.Truncated[0], "first 40") {
		t.Errorf("truncated = %v", res.Truncated)
	}
	dirs := map[string]bool{}
	for _, l := range res.Loops {
		dirs[l.Cwd] = true
	}
	if len(dirs) != 40 {
		t.Errorf("projects considered = %d", len(dirs))
	}
}

func TestUnavailableInsteadOfZero(t *testing.T) {
	d := scanDeps(t.TempDir(), []string{"main.c", "README"})
	b, _ := json.Marshal(d)
	if !strings.Contains(string(b), `"unavailable"`) || strings.Contains(string(b), "direct_total") {
		t.Errorf("deps json = %s", b)
	}
	tc := countTestFiles([]string{"main.c"})
	b, _ = json.Marshal(tc)
	if !strings.Contains(string(b), "unavailable") || strings.Contains(string(b), `"total"`) {
		t.Errorf("tests json = %s", b)
	}
	tc = countTestFiles([]string{"a_test.go"})
	b, _ = json.Marshal(tc)
	if !strings.Contains(string(b), `"total":1`) {
		t.Errorf("tests json = %s", b)
	}
}

func TestMeasureRevShallowAndLoopsNotRun(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	writeFiles(t, repo, map[string]string{"package.json": `{"dependencies":{"a":"1"}}`, "a.ts": "// TODO one\n"})
	commitAll(t, repo, "first")
	first, _ := git(repo, "rev-parse", "HEAD")
	writeFiles(t, repo, map[string]string{"b.ts": "// TODO two\n// TODO three\n", "package.json": `{"dependencies":{"a":"1","b":"2"}}`})
	commitAll(t, repo, "second")
	results := filepath.Join(t.TempDir(), "results.json")
	_ = os.WriteFile(results, []byte(`{"truncated":["go: 45 projects found, only the first 40 were considered"],"loops":[
 {"id":"a","status":"passed"},{"id":"b","status":"disabled","reason":"disabled in loops.json (needs: database)"},{"id":"c","status":"tool-missing","reason":"x"},{"id":"d","status":"findings"}]}`), 0o644)
	run := func(args ...string) snapshot {
		t.Helper()
		out := filepath.Join(t.TempDir(), "s.json")
		var so, se strings.Builder
		a := &app{stdout: &so, stderr: &se, now: timeNow}
		if code := a.run(append([]string{"measure", repo, "--out", out, "--months", "2"}, args...)); code != 0 {
			t.Fatalf("measure %v: %s", args, se.String())
		}
		var s snapshot
		b, _ := os.ReadFile(out)
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	head := run("--loop-results", results, "--as-of", "2099-01-01")
	old := run("--rev", strings.TrimSpace(first))
	if head.Deps.Total() != 2 || old.Deps.Total() != 1 || head.Hygiene.Todos.Total != 3 || old.Hygiene.Todos.Total != 1 {
		t.Errorf("rev measure: head deps=%d todos=%d, old deps=%d todos=%d", head.Deps.Total(), head.Hygiene.Todos.Total, old.Deps.Total(), old.Hygiene.Todos.Total)
	}
	if old.Meta.Head != strings.TrimSpace(first) || old.Git.CommitsTotal != 1 || head.Git.CommitsTotal != 2 || old.Meta.Tree != "rev-export" {
		t.Errorf("old meta = %+v commits=%d", old.Meta, old.Git.CommitsTotal)
	}
	if head.Branch != "main" || head.Shallow {
		t.Errorf("branch = %q shallow = %v", head.Branch, head.Shallow)
	}
	if head.LoopsNotRun == nil || head.LoopsNotRun.Count != 2 || head.LoopsNotRun.Reasons[0].ID != "b" || !strings.Contains(head.LoopsNotRun.Reasons[0].Reason, "database") {
		t.Errorf("loops not run = %+v", head.LoopsNotRun)
	}
	if len(head.Truncated) != 1 {
		t.Errorf("truncated = %v", head.Truncated)
	}
	var so, se strings.Builder
	a := &app{stdout: &so, stderr: &se, now: timeNow}
	if code := a.run([]string{"measure", repo, "--out", filepath.Join(t.TempDir(), "x.json"), "--rev", "main", "--worktree"}); code == 0 {
		t.Error("--rev with --worktree must fail")
	}
	if code := a.run([]string{"measure", repo, "--out", filepath.Join(t.TempDir(), "x.json"), "--rev", "no-such-ref"}); code == 0 {
		t.Error("unknown --rev must fail")
	}
}

func TestAgentContributorsExcludeBots(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	for i, auth := range []string{"Ann <ann@x.io>", "dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>", "Ann <ann@x.io>"} {
		writeFiles(t, repo, map[string]string{fmt.Sprintf("f%d.txt", i): "x"})
		gitRun(t, repo, "add", "-A")
		gitRun(t, repo, "commit", "-q", "--author="+auth, "-m", fmt.Sprintf("c%d", i), "-m", "Co-authored-by: Claude <noreply@anthropic.com>")
	}
	g := collectGit(repo, "HEAD", 2, timeNow().AddDate(100, 0, 0), nil)
	if g.ContributorsTotal != 1 || g.BotCommitsTotal != 1 || g.AgentCommitsTotal != 3 {
		t.Errorf("contributors=%d bots=%d agent=%d", g.ContributorsTotal, g.BotCommitsTotal, g.AgentCommitsTotal)
	}
	if g.AuthorsFor80pct12m != 0 && g.HumanCommits12m != 0 && g.TopAuthorShare12m != 1 {
		t.Errorf("top share = %v", g.TopAuthorShare12m)
	}
}

func TestScrubAndRedact(t *testing.T) {
	dir := t.TempDir()
	awsSecret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	writeFiles(t, dir, map[string]string{
		"notes/a.txt":                 "key AKIAIOSFODNN7EXAMPLE here\nsecret " + awsSecret + "\n",
		"notes/b.env":                 "STRIPE=sk_live_abcdefghijklmnop1234\npassword = hunter2hunter2hunter2\n",
		"notes/c.md":                  "url postgres://app:s3cretpw@db.internal.corp/x and ghp_" + strings.Repeat("a1B2", 9) + "\n",
		"notes/d.json":                `{"jwt":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"}` + "\n-----BEGIN RSA PRIVATE KEY-----\n",
		"clean/ok.txt":                "const token = process.env.API_TOKEN_VALUE_HERE\nsee /Users/jonas/Desktop/projects/consulting/peers/app/lib/db/actions/events/queries.ts\n",
		"node_modules/x/leak.txt":     "AKIAIOSFODNN7EXAMPLE",
		".home-run/.npmrc":            "AKIAIOSFODNN7EXAMPLE",
		"clean/redacted.txt":          "token=[REDACTED] password: [REDACTED:DB]\n",
		"report.html":                 "<p>AIza" + strings.Repeat("A1b2", 8) + "xyz_ and xoxb-1234567890-abcdefghij</p>\n",
		"clean/postgres_template.txt": "postgres://user:${DB_PASSWORD}@host/db\n",
	})
	var so, se strings.Builder
	a := &app{stdout: &so, stderr: &se, now: timeNow}
	code := a.run([]string{"scrub", dir})
	out := so.String()
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	for _, want := range []string{"notes/a.txt:1: AWS access key id", "notes/a.txt:2: AWS secret access key", "notes/b.env:1: Stripe live key", "notes/b.env:2: key, secret, token or password assignment",
		"notes/c.md:1: database URL with password", "notes/c.md:1: GitHub token", "notes/d.json:1: JWT", "notes/d.json:2: private key block", "report.html:1: Google API key", "report.html:1: Slack token"} {
		if !strings.Contains(out, want) {
			t.Errorf("scrub output lacks %q:\n%s", want, out)
		}
	}
	for _, raw := range []string{"AKIAIOSFODNN7EXAMPLE", awsSecret, "sk_live_abcdefghijklmnop1234", "hunter2hunter2hunter2", "s3cretpw", "dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"} {
		if strings.Contains(out, raw) {
			t.Errorf("scrub printed a raw value %q:\n%s", raw, out)
		}
	}
	for _, clean := range []string{"clean/ok.txt", "clean/redacted.txt", "node_modules", ".home-run", "postgres_template"} {
		if strings.Contains(out, clean) {
			t.Errorf("%s should not be flagged:\n%s", clean, out)
		}
	}
	// A clean workspace passes; --report adds an outside file.
	cleanDir := t.TempDir()
	writeFiles(t, cleanDir, map[string]string{"a.txt": "nothing here\n"})
	rep := filepath.Join(t.TempDir(), "report.html")
	_ = os.WriteFile(rep, []byte("<p>token: sk_live_abcdefghijklmnop1234</p>"), 0o644)
	so.Reset()
	if code := a.run([]string{"scrub", cleanDir}); code != 0 {
		t.Errorf("clean workspace exit %d: %s", code, so.String())
	}
	if code := a.run([]string{"scrub", cleanDir, "--report", rep}); code != 1 {
		t.Errorf("report with a secret should fail, exit %d", code)
	}
	// run-loops tails redact the same shapes.
	for _, s := range []string{"aws_secret " + awsSecret + " end", "k sk_live_abcdefghijklmnop1234 end", "AIza" + strings.Repeat("A1b2", 8) + "xyz_"} {
		if got := redactSecrets(s, nil); !strings.Contains(got, "REDACTED") || strings.Contains(got, awsSecret) || strings.Contains(got, "sk_live_abc") {
			t.Errorf("redactSecrets(%q) = %q", s, got)
		}
	}
	if got := redactSecrets("/var/folders/4x/2k2r8yr91_ddndtz7c_05jvc0000gn/T/TestRunLoops/001/.home-001", nil); strings.Contains(got, "REDACTED") {
		t.Errorf("a path was redacted: %s", got)
	}
}

func TestCheckReportJSONBlockContent(t *testing.T) {
	bad := `<script type="application/json" id="a">{"t":"<script>alert(1)</script>"}</script>`
	r := checkReport(bad, defaultAllowSet(), nil, false)
	if len(failuresOf(r, "embedded JSON data")) == 0 {
		t.Errorf("a </script inside a block should fail: %+v", r.Failures)
	}
	for _, c := range []struct{ name, body, want string }{
		{"script tag", `{"t":"x <script src=//e>"}`, "<script"},
		{"javascript url", `{"rows":[{"how":"<a href=\"javascript:alert(1)\">x</a>"}]}`, "javascript:"},
		{"handler", `{"v":"<img src=x onerror=alert(1)>"}`, "event handler"},
		{"escaped end tag", `{"v":"a <\/script> b"}`, ""},
		{"clean", `{"v":"1 < 2 and <code>x</code>","n":3}`, ""},
	} {
		src := `<script type="application/json" id="data">` + c.body + `</script>`
		r := checkReport(src, defaultAllowSet(), nil, false)
		var msgs []string
		for _, f := range failuresOf(r, "embedded JSON data") {
			msgs = append(msgs, f.Message)
		}
		all := strings.Join(msgs, "\n")
		if c.want == "" && len(msgs) != 0 || c.want != "" && !strings.Contains(all, c.want) {
			t.Errorf("%s: messages = %q, want %q", c.name, all, c.want)
		}
	}
	// A literal </script cuts the block: the parse error says why.
	cut := `<script type="application/json" id="cut">{"v":"a </script> b"}</script>`
	r = checkReport(cut, defaultAllowSet(), nil, false)
	if m := failuresOf(r, "embedded JSON data"); len(m) == 0 || !strings.Contains(m[0].Message, "literal </script") {
		t.Errorf("cut block = %+v", r.Failures)
	}
	for _, term := range []string{"Frankfurt", "Oslo", "NOK", "SEK", "UAE", "Vercel"} {
		if defaultAllowSet()[term] {
			t.Errorf("%s is author-specific and must not be in the default allowlist", term)
		}
	}
}

func TestDiffDirectionsWarningsAndAuthors(t *testing.T) {
	mk := func(todo float64, scanner string, custom float64) map[string]any {
		return map[string]any{"schema_version": 1.0, "tool": map[string]any{"version": "0.1.0"}, "meta": map[string]any{"head_sha": "a", "tree": "head-export"},
			"size":    map[string]any{"tool": "scc 4.1.0"},
			"hygiene": map[string]any{"todo_markers": map[string]any{"total": todo}, "test_files": map[string]any{"total": todo}, "secret_scan": map[string]any{"tool": scanner}},
			"custom":  []any{map[string]any{"name": "unowned handlers", "value": custom, "definition": map[string]any{"better": "down"}}}}
	}
	r := buildDiff(mk(10, "gitleaks", 9), mk(4, "", 5))
	by := map[string]metricDelta{}
	for _, d := range append(append([]metricDelta{}, r.Headline...), r.Metrics...) {
		by[d.Path] = d
	}
	if by["hygiene.todo_markers.total"].Direction != "" || by["hygiene.test_files.total"].Direction != "" {
		t.Errorf("gameable counts must have no direction: %+v %+v", by["hygiene.todo_markers.total"], by["hygiene.test_files.total"])
	}
	if d := by["custom[unowned handlers].value"]; d.Direction != "better" || d.Delta != -4 {
		t.Errorf("declared direction lost: %+v", d)
	}
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "secret scanner differs") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v", r.Warnings)
	}
	if isLockfilePath("a/b/pnpm-lock.yaml") != true || isLockfilePath("src/lock.go") || !isLockfilePath("Cargo.lock") || !isLockfilePath("x/yarn.lock") {
		t.Error("lockfile detection")
	}

	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	commit := func(author, file, content string) {
		writeFiles(t, repo, map[string]string{file: content})
		gitRun(t, repo, "add", "-A")
		gitRun(t, repo, "commit", "-q", "--author="+author+" <"+strings.ToLower(strings.Fields(author)[0])+"@x.io>", "-m", "c "+file)
	}
	commit("Base Person", "a.txt", "1\n")
	base, _ := git(repo, "rev-parse", "HEAD")
	commit("Jon Smith", "b.txt", "1\n")
	commit("Jonas Jensen", "pnpm-lock.yaml", strings.Repeat("x\n", 500))
	commit("Jonas Jensen", "c.txt", "1\n2\n")
	head, _ := git(repo, "rev-parse", "HEAD")
	if s := authorSectionFor("jon", repo, strings.TrimSpace(base), strings.TrimSpace(head)); s.Commits != 0 {
		t.Errorf("an anchored 'jon' matches neither Jon Smith nor Jonas Jensen: %+v", s)
	}
	if s := authorSectionFor("jon smith", repo, strings.TrimSpace(base), strings.TrimSpace(head)); s.Commits != 1 || s.Authors[0].Path != "Jon Smith" {
		t.Errorf("full name should match: %+v", s)
	}
	s := authorSectionFor("jonas.*", repo, strings.TrimSpace(base), strings.TrimSpace(head))
	if s.Commits != 2 || s.Added != 2 || s.FilesTouched != 1 {
		t.Errorf("lockfile churn must not count: %+v", s)
	}
}
