package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// defaultAllow holds terms a general technical reader knows. Anything else that
// looks like an identifier needs a definition in the report.
var defaultAllow = strings.Fields(`
API APIs URL URLs URI HTTP HTTPS HTML CSS JS TS JSX TSX JSON YAML TOML XML SQL NoSQL CI CD DB UI UX CPU GPU RAM ROM SSD AWS GCP S3 EC2 CDN DNS JWT SSO PR MR PRs MRs XSS CSRF SSRF IDOR SDK CLI OS GDPR PII LOC
FAQ ID IDs UUID SSH TLS SSL TCP UDP IP VPN REST GraphQL gRPC RPC CRUD ORM MVP KPI ROI SLA SLO SLI QA IT HR CEO CTO CFO CI/CD E2E PDF PNG JPG JPEG GIF SVG CSV EU UK US USA AI ML LLM LLMs GPT RSS CORS CSP WAF IAM RBAC ACL VM VPC SaaS PaaS IaaS IaC MFA OTP UTF ASCII
GET POST PUT DELETE PATCH HEAD OPTIONS README OK NB FYI TODO FIXME NOTE TBD ETA AM PM UTC GMT CET USD EUR GBP KB MB GB TB MD IDE
JavaScript TypeScript GitHub GitLab Bitbucket PostgreSQL MySQL MongoDB SQLite Redis Docker Kubernetes macOS iOS iPhone iPad iCloud YouTube LinkedIn OpenAI ChatGPT DevOps OAuth WebSocket WebSockets WebRTC GitHub's
Node.js Next.js React Vue Angular Python Java Kotlin Swift Rust Go Ruby PHP Linux Windows Android Chrome Safari Firefox
kB KiB MiB GiB SIGTERM SIGKILL SIGINT SIGHUP PK FK NOT NULL AND OR SHA1 SHA256 SHA512 DoS DDoS UTF-8 TLS1.2
Q1 Q2 Q3 Q4 H1 H2 H3 H4 H5 H6 V8 A4 B2B B2C K8s P2P I18n L10n A11y
LTS SLA SOC ISO NPM PNPM CDNs APIs' LGTM WIP DRY KISS YAGNI SOLID SPA SSR SSG ISR PWA HMR ESM CJS AST DOM BOM SEO CMS ERP CRM SMS MMS PIN QR NFC USB HDMI LED LCD OLED GUI TUI REPL IO I/O AJAX XHR SOAP WSDL
HMAC AES RSA ECDSA TOTP PKCE OIDC SAML LDAP mTLS PKI KMS HSM CVE CVEs CVSS OWASP RCE SQLi ReDoS TOCTOU HSTS SRI SAST DAST SBOM PCI HIPAA SOC2 SMTP IMAP FTP SFTP NTP IPv4 IPv6
TTL LRU OOM GC JIT WASM TDD BDD DDD OOP MVC CQRS ETL RFC SemVer SRE MTTR MTBF RTO RPO DORA P50 P90 P95 P99 EKS ECS ECR RDS SQS SNS GCS GKE CDK ALB ELB NAT ARN
DynamoDB BigQuery CloudWatch CloudFront PagerDuty OpenTelemetry PostHog LaunchDarkly SendGrid FastAPI NestJS SvelteKit WebAuthn DevTools
`)

// sessionPhrases are things said about the conversation that produced a report.
var sessionPhrases = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bas (we |i |previously )?discussed\b`),
	regexp.MustCompile(`(?i)\bas mentioned (earlier|above|before|previously)\b`),
	regexp.MustCompile(`(?i)\bearlier in this (session|conversation|chat|thread|report)\b`),
	regexp.MustCompile(`(?i)\byou asked\b`),
	regexp.MustCompile(`(?i)\bthis session\b`),
	regexp.MustCompile(`(?i)\bas we saw\b`),
	regexp.MustCompile(`(?i)\bper our\b`),
	regexp.MustCompile(`(?i)\bas (i|we) (said|noted|agreed|found)\b`),
	regexp.MustCompile(`(?i)\bin this conversation\b`),
	regexp.MustCompile(`(?i)\bwe discussed\b`),
	regexp.MustCompile(`(?i)\bthe user (asked|wanted|said|requested)\b`),
	regexp.MustCompile(`(?i)\bas requested\b`),
	regexp.MustCompile(`(?i)\blike you said\b`),
}

var rePlanBare = regexp.MustCompile(`(?i)\bthe plan\b`)

var (
	reTicketTok = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+[a-z]?\b`)
	reLabelTok  = regexp.MustCompile(`\b[A-Z]{1,3}\d+(?:\.\d+)*[a-z]?\b`)
	reVerTok    = regexp.MustCompile(`\bv\d+(?:\.\d+)*-[A-Za-z][\w.-]*\b`)
	reAcrTok    = regexp.MustCompile(`\b[A-Z]{2,}[0-9]*s?\b`)
	reCamelTok  = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]*)+\b|\b[a-z]+(?:[A-Z][a-z0-9]*)+\b`)
)

type token struct {
	Text, Norm string
	Start, End int
	Kind       string
}

// findTokens returns the identifier-like tokens of a text, earlier patterns
// taking precedence where they overlap.
func findTokens(text string) []token {
	var out []token
	taken := func(s, e int) bool {
		for _, t := range out {
			if s < t.End && e > t.Start {
				return true
			}
		}
		return false
	}
	for _, p := range []struct {
		re   *regexp.Regexp
		kind string
	}{{reTicketTok, "ticket"}, {reLabelTok, "label"}, {reVerTok, "version label"}, {reAcrTok, "acronym"}, {reCamelTok, "CamelCase"}} {
		for _, m := range p.re.FindAllStringIndex(text, -1) {
			if taken(m[0], m[1]) {
				continue
			}
			t := token{Text: text[m[0]:m[1]], Start: m[0], End: m[1], Kind: p.kind}
			t.Norm = t.Text
			if p.kind == "acronym" && len(t.Text) >= 3 && strings.HasSuffix(t.Text, "s") && !isLower(t.Text[len(t.Text)-2]) {
				t.Norm = t.Text[:len(t.Text)-1]
			}
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func isLower(b byte) bool { return b >= 'a' && b <= 'z' }

func readAllowFile(p string) ([]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if t != "" && !strings.HasPrefix(t, "#") {
			out = append(out, t)
		}
	}
	return out, sc.Err()
}

// ------------------------------------------------------------ repo harvest

type harvested struct {
	Labels    []string          `json:"labels"`
	Codenames []string          `json:"codenames"`
	Sources   map[string]string `json:"first_seen_in,omitempty"`
}

// reNoiseLabel drops codes that are not project labels: error codes with four or
// more digits, hex colours and ids, encodings and algorithm names.
var reNoiseLabel = regexp.MustCompile(`^[A-Z]{1,3}\d{4,}[a-z]?$|^[0-9A-F]{6}$|^[A-Z]\d?[0-9A-F]{5,}$|^(AES|SHA|UTF|RS|HS|ES|MD|BCP|ISO|RFC|MP|AV|VP|H|TLS)[- ]?\d+$|^(RS|HS|ES)\d+$|^(MP|AV|VP)\d$`)

var (
	rePhaseLabel = regexp.MustCompile(`\b(?:Phase|Wave|Milestone|Sprint|Stage)\s+\d+(?:\.\d+)*[a-z]?\b`)
	reADR        = regexp.MustCompile(`\bADR[- ]?\d+\b`)
	reRelLabel   = regexp.MustCompile(`\b(?:release|rel)-\d+(?:\.\d+)*\b`)
	reBranchLbl  = regexp.MustCompile(`^[a-z]{1,3}\d+[a-z]?$`)
	reBranchNum  = regexp.MustCompile(`^\d+[a-z]?$`)
	reBranchTkt  = regexp.MustCompile(`^[a-z]{2,6}$`)
	reWordTok    = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]{3,}`)
	reHexish     = regexp.MustCompile(`^[0-9a-f]{6,}$`)
)

func loadDictionary() map[string]bool {
	f, err := os.Open("/usr/share/dict/words")
	if err != nil {
		return nil
	}
	defer f.Close()
	d := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		d[strings.ToLower(sc.Text())] = true
	}
	return d
}

// harvestRepo reads the repo's docs, branch names, tags and commit subjects for
// the labels (phases, releases, tickets) and codenames its authors use among
// themselves.
func harvestRepo(repo string, allow map[string]bool) harvested {
	h := harvested{Sources: map[string]string{}}
	labels := map[string]string{}
	addLabel := func(l, src string) {
		if l == "" || allow[l] || reNoiseLabel.MatchString(l) {
			return
		}
		if _, ok := labels[l]; !ok {
			labels[l] = src
		}
	}
	scanLabels := func(text, src string) {
		for _, re := range []*regexp.Regexp{reTicketTok, reLabelTok, rePhaseLabel, reADR, reRelLabel, reVerTok} {
			for _, m := range re.FindAllString(text, -1) {
				addLabel(m, src)
			}
		}
	}
	capMid := map[string]int{}
	lowerSeen := map[string]bool{}
	files := listRepoFiles(repo)
	total := 0
	docCount := 0
	for _, f := range files {
		ext := strings.ToLower(path.Ext(f))
		inDocs := false
		for _, seg := range strings.Split(f, "/") {
			if docDirNames[seg] {
				inDocs = true
			}
		}
		if !(ext == ".md" || ext == ".mdx" || ext == ".rst" || ext == ".adoc" || (ext == ".txt" && inDocs)) {
			continue
		}
		if docCount >= 800 || total > 16<<20 {
			break
		}
		text, ok := readText(repo, f, 1<<20)
		if !ok {
			continue
		}
		docCount++
		total += len(text)
		scanLabels(text, f)
		scanProperNouns(text, capMid, lowerSeen)
	}
	if hasGit(repo) {
		if refs, err := git(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes", "refs/tags"); err == nil {
			for _, ref := range strings.Split(refs, "\n") {
				ref = strings.TrimSpace(ref)
				if ref == "" {
					continue
				}
				scanLabels(ref, "ref:"+ref)
				for _, seg := range strings.Split(ref, "/") {
					parts := strings.Split(seg, "-")
					if len(parts) > 0 && reBranchLbl.MatchString(parts[0]) && !reHexish.MatchString(parts[0]) {
						l := strings.ToUpper(parts[0][:1]) + parts[0][1:]
						l = strings.ToUpper(l)
						addLabel(l, "ref:"+ref)
						for i := 1; i < len(parts) && reBranchNum.MatchString(parts[i]); i++ {
							l += "." + strings.ToUpper(parts[i])
							addLabel(l, "ref:"+ref)
						}
					}
					if len(parts) > 1 && reBranchTkt.MatchString(parts[0]) && reBranchNum.MatchString(parts[1]) {
						addLabel(strings.ToUpper(parts[0])+"-"+parts[1], "ref:"+ref)
					}
				}
			}
		}
		if subj, err := git(repo, "log", "--all", "-n", "5000", "--format=%s"); err == nil {
			scanLabels(subj, "commit subjects")
		}
	}
	for l, src := range labels {
		h.Labels = append(h.Labels, l)
		h.Sources[l] = src
	}
	sort.Strings(h.Labels)

	// Codenames: words the authors repeat in names and headings that are no
	// dictionary word, dependency or well-known term.
	dict := loadDictionary()
	exclude := map[string]bool{strings.ToLower(path.Base(strings.TrimRight(repo, "/"))): true}
	for _, f := range files {
		if path.Base(f) == "package.json" {
			var pj struct {
				Name            string         `json:"name"`
				Dependencies    map[string]any `json:"dependencies"`
				DevDependencies map[string]any `json:"devDependencies"`
			}
			if t, ok := readText(repo, f, 4<<20); ok && json.Unmarshal([]byte(t), &pj) == nil {
				exclude[strings.ToLower(pj.Name)] = true
				for _, m := range []map[string]any{pj.Dependencies, pj.DevDependencies} {
					for d := range m {
						for _, part := range strings.FieldsFunc(strings.ToLower(d), func(r rune) bool { return r == '/' || r == '-' || r == '@' || r == '.' }) {
							exclude[part] = true
						}
					}
				}
			}
		}
	}
	// Authors' own names are not codenames.
	if hasGit(repo) {
		if out, err := git(repo, "log", "--all", "-n", "5000", "--format=%aN"); err == nil {
			for _, n := range strings.Fields(strings.ToLower(out)) {
				exclude[n] = true
			}
		}
	}
	// Proper-noun style: capitalised mid-sentence at least three times and never
	// written lower case in the docs, so not an ordinary word.
	for w, caps := range capMid {
		lw := strings.ToLower(w)
		if caps < 3 || lowerSeen[lw] || exclude[lw] || allow[w] || allow[lw] || len(w) < 4 {
			continue
		}
		if dict != nil && (dict[lw] || dict[strings.TrimSuffix(lw, "s")]) {
			continue
		}
		h.Codenames = append(h.Codenames, w)
	}
	sort.Strings(h.Codenames)
	return h
}

var (
	reCapWord   = regexp.MustCompile(`\b[A-Z][a-z]{3,}[A-Za-z0-9]*\b`)
	reLowerWord = regexp.MustCompile(`\b[a-z][a-z0-9]{3,}\b`)
)

// scanProperNouns counts capitalised words that do not start a sentence or a
// line, and notes every word seen in lower case.
func scanProperNouns(text string, capMid map[string]int, lowerSeen map[string]bool) {
	// Code blocks and spans would add identifiers that are not prose.
	text = reFence.ReplaceAllString(text, " ")
	text = reSpan.ReplaceAllString(text, " ")
	for _, m := range reLowerWord.FindAllString(text, -1) {
		lowerSeen[m] = true
	}
	for _, m := range reCapWord.FindAllStringIndex(text, -1) {
		i := m[0]
		j := i - 1
		for j >= 0 && (text[j] == ' ' || text[j] == '\t') {
			j--
		}
		if j < 0 || strings.ContainsRune(".!?:\n#*-|>(\"'`", rune(text[j])) {
			continue // sentence, line or list-item start
		}
		capMid[text[m[0]:m[1]]]++
	}
}

var (
	reFence = regexp.MustCompile("(?s)```.*?```")
	reSpan  = regexp.MustCompile("`[^`\n]*`")
)
