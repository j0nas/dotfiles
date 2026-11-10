package main

import (
	"strings"
	"testing"
	"time"
)

func timeNow() time.Time { return time.Unix(1700000000, 0) }

func TestParseOutputTestRunners(t *testing.T) {
	cases := []struct {
		name, cmd, out string
		exit           int
		want           map[string]any
	}{
		{"jest", "jest", "Test Suites: 1 failed, 3 passed, 4 total\nTests:       2 failed, 1 skipped, 40 passed, 43 total\n", 1,
			map[string]any{"tool": "jest", "passed": 40, "failed": 2, "skipped": 1, "total": 43}},
		{"vitest", "vitest run", " Test Files  1 failed | 2 passed (3)\n      Tests  3 failed | 20 passed | 2 skipped (25)\n", 1,
			map[string]any{"tool": "vitest", "passed": 20, "failed": 3, "skipped": 2, "total": 25}},
		{"node tap", "node --test", "# tests 875\n# suites 10\n# pass 873\n# fail 2\n# cancelled 0\n# skipped 0\n# todo 0\n", 1,
			map[string]any{"tool": "node:test", "passed": 873, "failed": 2, "skipped": 0, "total": 875}},
		{"node spec", "node --test", "ℹ tests 12\nℹ suites 1\nℹ pass 11\nℹ fail 1\nℹ cancelled 0\nℹ skipped 0\nℹ todo 0\n", 1,
			map[string]any{"tool": "node:test", "passed": 11, "failed": 1, "skipped": 0, "total": 12}},
		{"mocha", "mocha", "  12 passing (30ms)\n  1 failing\n  2 pending\n", 1,
			map[string]any{"tool": "mocha", "passed": 12, "failed": 1, "skipped": 2, "total": 15}},
		{"pytest", "pytest", "=========== 3 failed, 10 passed, 2 skipped, 1 warning in 1.20s ===========\n", 1,
			map[string]any{"tool": "pytest", "passed": 10, "failed": 3, "skipped": 2, "total": 15}},
		{"pytest quiet", "pytest -q", "..........\n10 passed in 0.50s\n", 0,
			map[string]any{"tool": "pytest", "passed": 10, "failed": 0, "skipped": 0, "total": 10}},
		{"go verbose", "go test -v", "=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n--- FAIL: TestB (0.00s)\n    --- PASS: TestB/sub (0.00s)\n--- SKIP: TestC (0.00s)\nFAIL\ngithub.com/x/y\t0.01s\nok  \tgithub.com/x/z\t0.02s\n?   \tgithub.com/x/w\t[no test files]\nFAIL\tgithub.com/x/y\t0.01s\n", 1,
			map[string]any{"tool": "go test", "passed": 1, "failed": 1, "skipped": 1, "total": 3, "packages_ok": 1, "packages_failed": 1, "packages_without_tests": 1, "subtests_total": 1}},
		{"cargo", "cargo test", "test result: ok. 5 passed; 0 failed; 1 ignored; 0 measured; 0 filtered out\ntest result: FAILED. 2 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out\n", 101,
			map[string]any{"tool": "cargo test", "passed": 7, "failed": 1, "skipped": 1, "total": 9}},
		{"rspec", "rspec", "Finished in 1.2 seconds\n20 examples, 2 failures, 3 pending\n", 1,
			map[string]any{"tool": "rspec", "passed": 15, "failed": 2, "skipped": 3, "total": 20}},
		{"phpunit ok", "phpunit", "OK (14 tests, 30 assertions)\n", 0,
			map[string]any{"tool": "phpunit", "passed": 14, "failed": 0, "skipped": 0, "total": 14, "assertions": 30}},
		{"phpunit fail", "phpunit", "Tests: 10, Assertions: 20, Failures: 2, Skipped: 1.\n", 1,
			map[string]any{"tool": "phpunit", "passed": 7, "failed": 2, "skipped": 1, "total": 10}},
		{"surefire", "mvn test", "[INFO] Tests run: 3, Failures: 0, Errors: 0, Skipped: 0, Time elapsed: 0.1 s - in A\n[INFO] Tests run: 12, Failures: 1, Errors: 1, Skipped: 2\n", 1,
			map[string]any{"tool": "surefire", "passed": 8, "failed": 2, "skipped": 2, "total": 12}},
		{"gradle", "gradle test", "10 tests completed, 3 failed\n", 1,
			map[string]any{"tool": "gradle test", "passed": 7, "failed": 3, "skipped": 0, "total": 10}},
		{"dotnet", "dotnet test", "Passed!  - Failed:     0, Passed:    12, Skipped:     1, Total:    13, Duration: 1 s\nFailed!  - Failed:     2, Passed:     5, Skipped:     0, Total:     7, Duration: 1 s\n", 1,
			map[string]any{"tool": "dotnet test", "passed": 17, "failed": 2, "skipped": 1, "total": 20}},
	}
	for _, c := range cases {
		got := parseOutput("test", c.cmd, c.out, c.exit)
		if got == nil {
			t.Errorf("%s: nothing parsed", c.name)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v (all: %v)", c.name, k, got[k], v, got)
			}
		}
	}
}

func TestParseOutputCheckers(t *testing.T) {
	cases := []struct {
		name, kind, cmd, out string
		exit                 int
		want                 map[string]any
	}{
		{"tsc errors", "typecheck", "pnpm exec tsc --noEmit", "src/a.ts(1,2): error TS2322: x\nsrc/b.ts(3,4): error TS2304: y\n", 2, map[string]any{"tool": "tsc", "errors": 2}},
		{"tsc clean", "typecheck", "pnpm exec tsc --noEmit", "", 0, map[string]any{"tool": "tsc", "errors": 0}},
		{"eslint summary", "lint", "eslint .", "\x1b[33m✖ 12 problems (2 errors, 10 warnings)\x1b[0m\n", 1, map[string]any{"tool": "eslint", "errors": 2, "warnings": 10}},
		{"next lint", "lint", "next lint", "./app/a.tsx\n12:5  Warning: Unexpected any.  @typescript-eslint/no-explicit-any\n13:1  Warning: x  y\n", 0, map[string]any{"tool": "next lint", "errors": 0, "warnings": 2}},
		{"ruff", "lint", "ruff check .", "Found 7 errors.\n", 1, map[string]any{"tool": "ruff", "errors": 7}},
		{"ruff clean", "lint", "ruff check .", "All checks passed!\n", 0, map[string]any{"tool": "ruff", "errors": 0}},
		{"mypy", "typecheck", "mypy .", "Found 4 errors in 2 files (checked 10 source files)\n", 1, map[string]any{"tool": "mypy", "errors": 4}},
		{"flake8", "lint", "flake8", "a.py:1:1: E501 line too long\nb.py:2:3: F401 unused\n", 1, map[string]any{"tool": "flake8", "errors": 2}},
		{"rubocop", "lint", "bundle exec rubocop", "12 files inspected, 3 offenses detected\n", 1, map[string]any{"tool": "rubocop", "errors": 3}},
		{"go vet", "lint", "go vet ./...", "./a.go:3:1: unreachable code\n", 1, map[string]any{"tool": "go lint", "errors": 1}},
		{"clippy", "lint", "cargo clippy", "warning: unused variable\nwarning: `x` (bin \"x\") generated 1 warning\nerror[E0425]: cannot find\n", 101, map[string]any{"tool": "clippy", "warnings_and_errors": 2}},
		{"dotnet build", "build", "dotnet build", "    3 Warning(s)\n    0 Error(s)\n", 0, map[string]any{"tool": "dotnet build", "warnings": 3, "errors": 0}},
	}
	for _, c := range cases {
		got := parseOutput(c.kind, c.cmd, c.out, c.exit)
		if got == nil {
			t.Errorf("%s: nothing parsed", c.name)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v (all: %v)", c.name, k, got[k], v, got)
			}
		}
	}
	if got := parseOutput("build", "make", "compiling...\ndone\n", 0); got != nil {
		t.Errorf("unrecognised output should parse to nil, got %v", got)
	}
}

func TestRedactSecrets(t *testing.T) {
	cases := []struct{ in, notWant string }{
		{"DATABASE_URL=postgres://user:hunter2pass@db.example.com/app", "hunter2pass"},
		{"Authorization: Bearer abcdefghijklmnop1234", "abcdefghijklmnop1234"},
		{"export API_KEY=supersecretvalue123", "supersecretvalue123"},
		{`{"client_secret": "s3cr3tValueHere"}`, "s3cr3tValueHere"},
		{"token ghp_abcdefghijklmnopqrstuvwxyz0123456789", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
		{"key AKIAABCDEFGHIJKLMNOP end", "AKIAABCDEFGHIJKLMNOP"},
		{"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123DEF456", "eyJhbGciOiJIUzI1NiJ9"},
		{"stripe sk-live1234567890abcdefgh", "sk-live1234567890abcdefgh"},
		{"blob aB3dE5gH7jK9mN1pQ3sT5vW7yZ9bC1dE3fG5hJ7", "aB3dE5gH7jK9mN1pQ3sT5vW7yZ9bC1dE3fG5hJ7"},
	}
	for _, c := range cases {
		got := redactSecrets(c.in, nil)
		if strings.Contains(got, c.notWant) {
			t.Errorf("redactSecrets(%q) = %q still holds the secret", c.in, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Errorf("redactSecrets(%q) = %q has no marker", c.in, got)
		}
	}
	keep := []string{
		"src/app/lib/db/actions/events/queries.ts:12:5 error",
		"commit e7c6af248f454456213496b17a7d3c0ddfd10eab",
		"/Users/jonas/Desktop/projects/consulting/peers/node_modules/.pnpm/next@15.0.0/node_modules/next/dist/server",
		"Unexpected token < in JSON at position 0",
	}
	for _, k := range keep {
		if got := redactSecrets(k, nil); got != k {
			t.Errorf("redactSecrets(%q) changed it to %q", k, got)
		}
	}
	got := redactSecrets("connecting with dummy-pass-xyz to db, flag ab", map[string]string{"DB_PASSWORD": "dummy-pass-xyz", "SHORT": "ab"})
	if strings.Contains(got, "dummy-pass-xyz") || !strings.Contains(got, "ab") {
		t.Errorf("extra_env values should be redacted, short ones kept: %q", got)
	}
}
