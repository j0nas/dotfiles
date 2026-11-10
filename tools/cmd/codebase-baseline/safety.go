package main

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// maxProjects caps how many projects of one ecosystem discover considers.
const maxProjects = 40

// deployTools are commands that deploy, publish or change live systems. A loop
// whose resolved text mentions one is off by default and says which.
var deployTools = []struct {
	Name string
	Re   *regexp.Regexp
}{
	{"sentry-cli", regexp.MustCompile(`(?i)\bsentry-cli\b`)},
	{"vercel", regexp.MustCompile(`(?i)\bvercel\b`)},
	{"netlify", regexp.MustCompile(`(?i)\bnetlify\s+deploy\b`)},
	{"terraform", regexp.MustCompile(`(?i)\bterraform\b`)},
	{"kubectl", regexp.MustCompile(`(?i)\bkubectl\b`)},
	{"helm", regexp.MustCompile(`(?i)\bhelm\b`)},
	{"aws", regexp.MustCompile(`(?i)\baws\s`)},
	{"gcloud", regexp.MustCompile(`(?i)\bgcloud\b`)},
	{"az", regexp.MustCompile(`(?i)\baz\s`)},
	{"firebase", regexp.MustCompile(`(?i)\bfirebase\s+deploy\b`)},
	{"wrangler", regexp.MustCompile(`(?i)\bwrangler\b`)},
	{"flyctl", regexp.MustCompile(`(?i)\bflyctl\b|\bfly\s+deploy\b`)},
	{"railway", regexp.MustCompile(`(?i)\brailway\b`)},
	{"heroku", regexp.MustCompile(`(?i)\bheroku\b`)},
	{"docker-push", regexp.MustCompile(`(?i)\bdocker\s+push\b`)},
	{"npm-publish", regexp.MustCompile(`(?i)\bnpm\s+publish\b`)},
	{"pnpm-publish", regexp.MustCompile(`(?i)\bpnpm\s+publish\b`)},
	{"twine-upload", regexp.MustCompile(`(?i)\btwine\s+upload\b`)},
	{"gh-release", regexp.MustCompile(`(?i)\bgh\s+release\b`)},
	{"prisma-migrate-deploy", regexp.MustCompile(`(?i)\bprisma\s+migrate\s+deploy\b`)},
	{"prisma-db-push", regexp.MustCompile(`(?i)\bprisma\s+db\s+push\b`)},
}

// deployToolsIn lists the deploy or publish tools a command text names.
func deployToolsIn(text string) []string {
	var out []string
	for _, dt := range deployTools {
		if dt.Re.MatchString(text) {
			out = append(out, dt.Name)
		}
	}
	return out
}

var reNestedRun = regexp.MustCompile(`(?:^|[\s;&|(])(?:npm|pnpm|yarn|bun)(?:\s+run)?(?:\s+-{1,2}[\w=-]+)*\s+([A-Za-z0-9][A-Za-z0-9:_.-]*)`)

// resolveScript returns the text a package.json script runs: its own command,
// its pre and post hooks, and one level of other scripts it calls through
// npm, pnpm, yarn or bun, each with their own hooks.
func resolveScript(scripts map[string]string, name string) string {
	var parts []string
	add := func(n string) {
		for _, pre := range []string{"pre", ""} {
			if s, ok := scripts[pre+n]; ok {
				parts = append(parts, s)
			}
		}
		if s, ok := scripts["post"+n]; ok {
			parts = append(parts, s)
		}
	}
	add(name)
	for _, m := range reNestedRun.FindAllStringSubmatch(scripts[name], -1) {
		if m[1] != name {
			if _, ok := scripts[m[1]]; ok {
				add(m[1])
			}
		}
	}
	return strings.Join(parts, " ; ")
}

type externalHit struct {
	File   string `json:"file"`
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
}

var (
	reCfgName   = regexp.MustCompile(`^(application[\w.-]*\.(ya?ml|properties)|appsettings[\w.-]*\.json|settings[\w.-]*\.py|(docker-)?compose[\w.-]*\.ya?ml)$`)
	reCfgDir    = regexp.MustCompile(`(^|/)config/.+\.(ya?ml|json|toml)$`)
	reJDBC      = regexp.MustCompile(`(?i)jdbc:[a-z0-9]+:(?:thin:@)?(?://|@//)?([^/:;?\s'"]+)`)
	reDBURL     = regexp.MustCompile(`(?i)\b(postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|rediss?|amqps?)://([^\s'"<>]+)`)
	reHTTPURL   = regexp.MustCompile(`(?i)\bhttps?://([^/\s'"<>?#]+)`)
	reSchemaish = regexp.MustCompile(`(?i)xmlns|\$schema|schemaLocation|xsi:|doctype`)
)

var docHosts = map[string]bool{"www.w3.org": true, "json-schema.org": true, "schemas.xmlsoap.org": true, "maven.apache.org": true, "www.apache.org": true,
	"schema.management.azure.com": true, "json.schemastore.org": true, "www.schemastore.org": true, "docs.github.com": true, "github.com": true}

// hostOf takes the host out of an authority such as user:pass@host:5432.
func hostOf(authority string) string {
	if i := strings.LastIndexByte(authority, '@'); i >= 0 {
		authority = authority[i+1:]
	}
	if u, err := url.Parse("//" + authority); err == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	h := strings.ToLower(authority)
	if i := strings.IndexAny(h, ":/"); i >= 0 {
		h = h[:i]
	}
	return h
}

// isLocalHost is true for the hosts a self-contained test run may talk to:
// loopback, compose service names (no dot), placeholders and example domains.
func isLocalHost(h string) bool {
	switch {
	case h == "", h == "localhost", h == "0.0.0.0", h == "::1", h == "host.docker.internal", h == "db", h == "redis":
		return true
	case strings.ContainsAny(h, "${}<>%*"), strings.HasPrefix(h, "127."), strings.HasPrefix(h, "10."), strings.HasPrefix(h, "192.168."):
		return true
	case !strings.Contains(h, "."):
		return true // a compose service name
	}
	for _, suf := range []string{".local", ".localhost", ".internal", ".test", ".invalid", ".example"} {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	return strings.HasPrefix(h, "example.") || strings.Contains(h, ".example.") || h == "example.com" || h == "example.org" || h == "example.net"
}

func isConfigFile(rel string) bool {
	base := path.Base(rel)
	return reCfgName.MatchString(base) || reCfgDir.MatchString(rel) || reEnvExample.MatchString(rel)
}

// externalHostIn returns the first non-local host that a config text points at,
// with the scheme. Comment lines and schema declarations are skipped.
func externalHostIn(text string) (scheme, host string) {
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "<!--") || reSchemaish.MatchString(t) {
			continue
		}
		for _, m := range reJDBC.FindAllStringSubmatch(t, -1) {
			if h := hostOf(m[1]); !isLocalHost(h) {
				return "jdbc", h
			}
		}
		for _, m := range reDBURL.FindAllStringSubmatch(t, -1) {
			if h := hostOf(m[2]); !isLocalHost(h) {
				return strings.ToLower(m[1]), h
			}
		}
		for _, m := range reHTTPURL.FindAllStringSubmatch(t, -1) {
			if h := hostOf(m[1]); !isLocalHost(h) && !docHosts[h] {
				return "http", h
			}
		}
	}
	return "", ""
}

// scanExternalConfig reads the tracked config files for connection strings and
// URLs that point outside the machine. Only the scheme and host are recorded,
// never credentials.
func (d *discoverer) scanExternalConfig() {
	for _, f := range d.files {
		if !isConfigFile(f) {
			continue
		}
		text, ok := readText(d.repo, f, 1<<20)
		if !ok {
			continue
		}
		if scheme, host := externalHostIn(text); host != "" {
			d.sig.ExternalConfig = append(d.sig.ExternalConfig, externalHit{File: f, Scheme: scheme, Host: host})
		}
	}
}

// nearestRoot is the directory of the closest manifest at or above rel's folder.
func nearestRoot(rel string) string {
	dir := path.Dir(rel)
	for {
		if projectRoots[dir] || dir == "." || dir == "/" {
			return dir
		}
		dir = path.Dir(dir)
	}
}

// applyExternalConfig switches off the test, e2e and integration loops of a
// project whose config talks to a real service.
func (d *discoverer) applyExternalConfig() {
	for _, hit := range d.sig.ExternalConfig {
		root := nearestRoot(hit.File)
		for i := range d.loops {
			l := &d.loops[i]
			if l.Kind != "test" && l.Kind != "e2e" && !strings.Contains(strings.ToLower(l.ID), "integration") {
				continue
			}
			if root != "." && l.Cwd != root && !strings.HasPrefix(l.Cwd, root+"/") {
				continue
			}
			need := "external-config:" + hit.File
			if !contains(l.Needs, need) {
				l.Needs = append(l.Needs, need)
				l.Note = strings.TrimSpace(l.Note + fmt.Sprintf(" %s points at %s://%s.", hit.File, hit.Scheme, hit.Host))
			}
			l.Enabled = false
		}
	}
}
