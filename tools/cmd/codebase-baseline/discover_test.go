package main

import (
	"strings"
	"testing"
	"time"
)

func discoverFixture(t *testing.T, name string) discoverOut {
	t.Helper()
	return discoverLoops("testdata/"+name, time.Unix(0, 0))
}

func findLoop(t *testing.T, res discoverOut, id string) loop {
	t.Helper()
	for _, l := range res.Loops {
		if l.ID == id {
			return l
		}
	}
	var ids []string
	for _, l := range res.Loops {
		ids = append(ids, l.ID)
	}
	t.Fatalf("no loop %q; have %s", id, strings.Join(ids, ", "))
	return loop{}
}

func TestDiscoverJSPnpmWorkspace(t *testing.T) {
	res := discoverFixture(t, "js-pnpm")
	install := findLoop(t, res, "js-install")
	if install.Cmd != "pnpm install --frozen-lockfile" || install.Kind != "install" || !install.Enabled {
		t.Errorf("install = %+v", install)
	}
	cases := []struct {
		id, cmd, kind string
		enabled       bool
	}{
		{"js-lint", "pnpm run lint", "lint", true},
		{"js-format-check", "pnpm run format:check", "format", true},
		{"js-test", "pnpm run test", "test", true},
		{"js-build", "pnpm run build", "build", true},
		{"js-test-e2e", "pnpm run test:e2e", "e2e", false},
		{"js-test-integration", "pnpm run test:integration", "test", false},
		{"js-tsc-noemit", "pnpm exec tsc --noEmit", "typecheck", true},
		{"js-packages-api-test", "pnpm run test", "test", true},
		{"js-packages-api-typecheck", "pnpm run typecheck", "typecheck", true},
		{"js-packages-web-build", "pnpm run build", "build", true},
	}
	for _, c := range cases {
		l := findLoop(t, res, c.id)
		if l.Cmd != c.cmd || l.Kind != c.kind || l.Enabled != c.enabled {
			t.Errorf("%s = {%q %s enabled=%v}, want {%q %s enabled=%v}", c.id, l.Cmd, l.Kind, l.Enabled, c.cmd, c.kind, c.enabled)
		}
	}
	if l := findLoop(t, res, "js-packages-api-test"); l.Cwd != "packages/api" || l.Requires[0] != "js-install" {
		t.Errorf("workspace loop = %+v", l)
	}
	for _, l := range res.Loops {
		for _, bad := range []string{"js-dev", "js-lint-fix", "js-format", "js-test-watch", "js-prebuild", "js-postinstall", "js-db-migrate"} {
			if l.ID == bad {
				t.Errorf("%s must not be a candidate", bad)
			}
		}
	}
	reasons := map[string]string{}
	for _, s := range res.Skipped {
		reasons[s.Name] = s.Reason
	}
	for _, name := range []string{"dev", "lint:fix", "format", "test:watch", "prebuild", "postinstall"} {
		if reasons[name] == "" {
			t.Errorf("script %q should be listed as skipped with a reason", name)
		}
	}
	if l := findLoop(t, res, "js-test-e2e"); !contains(l.Needs, "browser") {
		t.Errorf("e2e needs = %v", l.Needs)
	}
}

func TestDiscoverPython(t *testing.T) {
	res := discoverFixture(t, "python-uv")
	if l := findLoop(t, res, "py-install"); l.Cmd != "uv sync" {
		t.Errorf("install = %q", l.Cmd)
	}
	for id, cmd := range map[string]string{"py-pytest": "uv run pytest -q", "py-ruff": "uv run ruff check .", "py-mypy": "uv run mypy ."} {
		if l := findLoop(t, res, id); l.Cmd != cmd || !l.Enabled {
			t.Errorf("%s = %q enabled=%v", id, l.Cmd, l.Enabled)
		}
	}
}

func TestDiscoverGo(t *testing.T) {
	res := discoverFixture(t, "go-mod")
	for id, cmd := range map[string]string{"go-mod-download": "go mod download", "go-build": "go build ./...", "go-vet": "go vet ./...", "go-test": "go test -count=1 -v ./..."} {
		if l := findLoop(t, res, id); l.Cmd != cmd || !l.Enabled {
			t.Errorf("%s = %q enabled=%v", id, l.Cmd, l.Enabled)
		}
	}
}

func TestDiscoverRustGradleMavenDotnetRubyPHP(t *testing.T) {
	cases := []struct {
		fixture string
		want    map[string]string
	}{
		{"rust", map[string]string{"rust-fetch": "cargo fetch", "rust-build": "cargo build", "rust-clippy": "cargo clippy", "rust-test": "cargo test --no-fail-fast"}},
		{"gradle", map[string]string{"gradle-build": "gradle build -x test --console=plain", "gradle-test": "gradle test --continue --console=plain", "gradle-spotlesscheck": "gradle spotlessCheck --console=plain"}},
		{"maven", map[string]string{"maven-package": "mvn -B -DskipTests package", "maven-test": "mvn -B test"}},
		{"dotnet", map[string]string{"dotnet-restore": "dotnet restore App.sln", "dotnet-build": "dotnet build --no-restore App.sln", "dotnet-test": "dotnet test --no-restore App.sln"}},
		{"ruby", map[string]string{"ruby-bundle-install": "bundle install", "ruby-rspec": "bundle exec rspec", "ruby-rubocop": "bundle exec rubocop"}},
		{"php", map[string]string{"php-composer-install": "composer install --no-interaction --prefer-dist", "php-phpunit": "vendor/bin/phpunit"}},
	}
	for _, c := range cases {
		res := discoverFixture(t, c.fixture)
		for id, cmd := range c.want {
			if l := findLoop(t, res, id); l.Cmd != cmd {
				t.Errorf("%s/%s = %q, want %q", c.fixture, id, l.Cmd, cmd)
			}
		}
	}
}

func TestDiscoverMakeTargets(t *testing.T) {
	res := discoverFixture(t, "make")
	test := findLoop(t, res, "make-test")
	if test.Cmd != "make test" || test.Enabled || !contains(test.Needs, "docker") {
		t.Errorf("make test should need docker and be disabled: %+v", test)
	}
	if l := findLoop(t, res, "make-lint"); l.Kind != "lint" || !strings.HasPrefix(l.Source, "Makefile:") {
		t.Errorf("make lint = %+v", l)
	}
	for _, l := range res.Loops {
		if l.ID == "make-clean" || l.ID == "make-deploy" {
			t.Errorf("%s must not be a candidate", l.ID)
		}
	}
}

func TestDepsCounts(t *testing.T) {
	check := func(fixture string, want map[string]int) {
		t.Helper()
		files := walkFiles("testdata/" + fixture)
		d := scanDeps("testdata/"+fixture, files)
		got := map[string]int{}
		for _, m := range d.Manifests {
			got[m.Path] = m.Direct
		}
		for p, n := range want {
			if got[p] != n {
				t.Errorf("%s: %s direct = %d, want %d (all: %v)", fixture, p, got[p], n, got)
			}
		}
	}
	check("js-pnpm", map[string]int{"package.json": 3, "packages/api/package.json": 0})
	check("python-uv", map[string]int{"pyproject.toml": 5})
	check("go-mod", map[string]int{"go.mod": 1})
	check("rust", map[string]int{"Cargo.toml": 3})
	check("maven", map[string]int{"pom.xml": 2})
	check("gradle", map[string]int{"build.gradle.kts": 2})
	check("dotnet", map[string]int{"App.csproj": 1})
	check("ruby", map[string]int{"Gemfile": 3})
	check("php", map[string]int{"composer.json": 2})
	d := scanDeps("testdata/js-pnpm", walkFiles("testdata/js-pnpm"))
	if len(d.Lockfiles) != 1 || d.Lockfiles[0] != "pnpm-lock.yaml" {
		t.Errorf("lockfiles = %v", d.Lockfiles)
	}
}

func TestTOMLLite(t *testing.T) {
	doc := parseTOMLLite("[a]\nx = [\n  \"one\", # c\n  \"two\",\n]\ny = \"s # not a comment\"\n[b.c]\nz = { k = 1 }\n")
	if got := tomlArrayItems(doc.sections["a"]["x"]); got != 2 {
		t.Errorf("array items = %d", got)
	}
	if doc.sections["a"]["y"] != `"s # not a comment"` {
		t.Errorf("y = %q", doc.sections["a"]["y"])
	}
	if !doc.has("b.c") {
		t.Errorf("sections = %v", doc.sections)
	}
}
