package main

import (
	"regexp"
	"strings"
)

var redactRules = []struct {
	Re   *regexp.Regexp
	Repl string
}{
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@'"]+:[^@\s/'"]+@`), `${1}[REDACTED]@`},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), `${1} [REDACTED]`},
	{regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*(?:api[_-]?key|secret|token|passw(?:or)?d|passwd|credential|private[_-]?key|access[_-]?key|auth[_-]?key|client[_-]?secret)[A-Za-z0-9_.-]*)(["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&"']+)`), `${1}${2}[REDACTED]`},
	{regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`), `[REDACTED]`},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`), `[REDACTED]`},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`), `[REDACTED]`},
	{regexp.MustCompile(`\b[sr]k_(?:live|test)_[0-9A-Za-z]{10,}`), `[REDACTED]`},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`), `[REDACTED]`},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), `[REDACTED]`},
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), `[REDACTED]`},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`), `[REDACTED]`},
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`), `[REDACTED PRIVATE KEY]`},
}

var reLongToken = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)

// looksLikeSecretToken is true for long mixed-case alphanumeric runs with digits
// that are not paths or identifiers.
func looksLikeSecretToken(s string) bool {
	if strings.Count(s, "/") > 1 || strings.Contains(s, "node_modules") {
		return false
	}
	var lower, upper, digit bool
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= '0' && c <= '9':
			digit = true
		}
	}
	return lower && upper && digit
}

// redactSecrets masks anything in s that looks like a credential, and every
// literal value the caller handed to the loop through extra_env.
func redactSecrets(s string, envValues map[string]string) string {
	for name, v := range envValues {
		if len(v) >= 6 {
			s = strings.ReplaceAll(s, v, "[REDACTED:"+name+"]")
		}
	}
	for _, r := range redactRules {
		s = r.Re.ReplaceAllString(s, r.Repl)
	}
	for _, loc := range reverseSpans(awsSecretSpans(s)) {
		s = s[:loc[0]] + "[REDACTED]" + s[loc[1]:]
	}
	s = reLongToken.ReplaceAllStringFunc(s, func(m string) string {
		if looksLikeSecretToken(m) {
			return "[REDACTED]"
		}
		return m
	})
	return s
}

var reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07`)

func stripANSI(s string) string { return reANSI.ReplaceAllString(s, "") }

// reAWSSecret matches the 40-character base64 shape of an AWS secret access key.
var reAWSSecret = regexp.MustCompile(`[A-Za-z0-9/+]{40}`)

// looksLikeAWSSecret keeps the matches that have the mix of a real key: upper,
// lower and digits, plus a slash or plus sign, and no path-like look.
func looksLikeAWSSecret(s string) bool {
	if len(s) != 40 || !strings.ContainsAny(s, "/+") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.Contains(s, "//") {
		return false
	}
	var lower, upper, digit bool
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= '0' && c <= '9':
			digit = true
		}
	}
	return lower && upper && digit
}

func isTokenEdge(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("/+_.=-", b) >= 0
}

// awsSecretSpans finds standalone 40-character AWS-secret-shaped tokens: not a
// piece of a longer token, path or identifier.
func awsSecretSpans(s string) [][2]int {
	var out [][2]int
	for _, m := range reAWSSecret.FindAllStringIndex(s, -1) {
		if m[0] > 0 && isTokenEdge(s[m[0]-1]) || m[1] < len(s) && isTokenEdge(s[m[1]]) {
			continue
		}
		if looksLikeAWSSecret(s[m[0]:m[1]]) {
			out = append(out, [2]int{m[0], m[1]})
		}
	}
	return out
}

func reverseSpans(in [][2]int) [][2]int {
	out := make([][2]int, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
