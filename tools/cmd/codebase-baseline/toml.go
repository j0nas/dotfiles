package main

import (
	"regexp"
	"sort"
	"strings"
)

// tomlDoc is a deliberately small reading of a TOML file: sections to keys to
// the raw text of each value. It is enough to count dependencies and spot tool
// configuration without a TOML library.
type tomlDoc struct {
	sections map[string]map[string]string
}

var reTomlHeader = regexp.MustCompile(`^\[\[?\s*([^\[\]]+?)\s*\]\]?\s*$`)

func parseTOMLLite(text string) *tomlDoc {
	doc := &tomlDoc{sections: map[string]map[string]string{"": {}}}
	lines := strings.Split(text, "\n")
	cur := ""
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(stripTomlComment(lines[i]))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if m := reTomlHeader.FindStringSubmatch(line); m != nil {
				cur = strings.ReplaceAll(strings.ReplaceAll(m[1], `"`, ""), `'`, "")
				if doc.sections[cur] == nil {
					doc.sections[cur] = map[string]string{}
				}
				continue
			}
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.Trim(strings.TrimSpace(line[:eq]), `"'`)
		val := strings.TrimSpace(line[eq+1:])
		for {
			if (strings.HasPrefix(val, `"""`) && strings.Count(val, `"""`) < 2) ||
				(strings.HasPrefix(val, `'''`) && strings.Count(val, `'''`) < 2) ||
				tomlDepth(val) > 0 {
				if i+1 >= len(lines) {
					break
				}
				i++
				val += "\n" + strings.TrimSpace(stripTomlComment(lines[i]))
				continue
			}
			break
		}
		doc.sections[cur][key] = val
	}
	return doc
}

func stripTomlComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == '\\' && q == '"' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '#':
			return s[:i]
		}
	}
	return s
}

// tomlDepth is how many [ or { are still open in s, ignoring strings.
func tomlDepth(s string) int {
	var q byte
	d := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == '\\' && q == '"' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '[' || c == '{':
			d++
		case c == ']' || c == '}':
			d--
		}
	}
	return d
}

// tomlArrayItems counts the strings sitting directly inside the outermost array.
func tomlArrayItems(v string) int {
	var q byte
	d, n := 0, 0
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case q != 0:
			if c == '\\' && q == '"' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
			if d == 1 {
				n++
			}
		case c == '[' || c == '{':
			d++
		case c == ']' || c == '}':
			d--
		}
	}
	return n
}

func (d *tomlDoc) has(section string) bool {
	_, ok := d.sections[section]
	return ok
}

// sectionsWithPrefix lists section names equal to prefix or beneath it.
func (d *tomlDoc) sectionsWithPrefix(prefix string) []string {
	var out []string
	for s := range d.sections {
		if s == prefix || strings.HasPrefix(s, prefix+".") {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
