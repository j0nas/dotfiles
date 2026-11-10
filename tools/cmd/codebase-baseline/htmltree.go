package main

import (
	"html"
	"strings"
)

// hnode is a node of a tolerant HTML tree: an element (tag set) or a text node.
type hnode struct {
	tag      string
	attrs    map[string]string
	children []*hnode
	text     string
	parent   *hnode
	line     int
}

func (n *hnode) attr(k string) string { return n.attrs[k] }

func (n *hnode) hasClass(c string) bool {
	for _, f := range strings.Fields(n.attrs["class"]) {
		if f == c {
			return true
		}
	}
	return false
}

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

var rawTextTags = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}

// implicitly closed when one of the listed tags opens as the nearest open element.
var closedBy = map[string][]string{
	"li": {"li"}, "dt": {"dt", "dd"}, "dd": {"dt", "dd"}, "tr": {"tr", "td", "th"}, "td": {"td", "th"}, "th": {"td", "th"},
	"option": {"option"}, "thead": {"tbody"}, "tbody": {"thead"},
}

var closesP = map[string]bool{
	"p": true, "div": true, "ul": true, "ol": true, "dl": true, "table": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "pre": true, "blockquote": true, "section": true, "article": true,
	"aside": true, "header": true, "footer": true, "nav": true, "figure": true, "hr": true, "form": true, "details": true,
}

// parseHTML builds a tree without validating anything: unknown or stray tags
// are tolerated, unclosed elements end where their parent does.
func parseHTML(src string) *hnode {
	root := &hnode{tag: "#root", attrs: map[string]string{}}
	cur := root
	line := 1
	i := 0
	n := len(src)
	addText := func(s string, ln int) {
		if s == "" {
			return
		}
		cur.children = append(cur.children, &hnode{text: html.UnescapeString(s), parent: cur, line: ln})
	}
	for i < n {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			addText(src[i:], line)
			break
		}
		if lt > 0 {
			addText(src[i:i+lt], line)
			line += strings.Count(src[i:i+lt], "\n")
			i += lt
		}
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest, "-->")
			if end < 0 {
				end = len(rest) - 3
			}
			line += strings.Count(rest[:end+3], "\n")
			i += end + 3
			continue
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				end = len(rest) - 1
			}
			line += strings.Count(rest[:end+1], "\n")
			i += end + 1
			continue
		}
		if len(rest) < 2 || !(rest[1] == '/' || isAlpha(rest[1])) {
			addText("<", line)
			i++
			continue
		}
		end := tagEnd(rest)
		if end < 0 {
			addText(rest, line)
			break
		}
		raw := rest[1:end]
		startLine := line
		line += strings.Count(rest[:end+1], "\n")
		i += end + 1
		if raw[0] == '/' {
			name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "/")))
			for p := cur; p != nil && p != root; p = p.parent {
				if p.tag == name {
					cur = p.parent
					break
				}
			}
			continue
		}
		selfClose := strings.HasSuffix(raw, "/")
		raw = strings.TrimSuffix(raw, "/")
		name, attrs := parseTag(raw)
		if closers, ok := closedBy[name]; ok && cur != root && contains(closers, cur.tag) {
			cur = cur.parent
		}
		if closesP[name] && cur.tag == "p" {
			cur = cur.parent
		}
		el := &hnode{tag: name, attrs: attrs, parent: cur, line: startLine}
		cur.children = append(cur.children, el)
		if voidTags[name] || selfClose {
			continue
		}
		if rawTextTags[name] {
			closeTag := "</" + name
			idx := strings.Index(strings.ToLower(src[i:]), closeTag)
			if idx < 0 {
				idx = len(src) - i
			}
			if idx > 0 {
				el.children = append(el.children, &hnode{text: src[i : i+idx], parent: el, line: line})
			}
			line += strings.Count(src[i:i+idx], "\n")
			i += idx
			if e := strings.IndexByte(src[i:], '>'); e >= 0 {
				i += e + 1
			} else {
				i = n
			}
			continue
		}
		cur = el
	}
	return root
}

func isAlpha(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// tagEnd finds the closing > of the tag starting at s[0], skipping quoted values.
func tagEnd(s string) int {
	var q byte
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return i
		}
	}
	return -1
}

func parseTag(raw string) (string, map[string]string) {
	attrs := map[string]string{}
	i := 0
	for i < len(raw) && !isSpace(raw[i]) {
		i++
	}
	name := strings.ToLower(raw[:i])
	for i < len(raw) {
		for i < len(raw) && isSpace(raw[i]) {
			i++
		}
		start := i
		for i < len(raw) && !isSpace(raw[i]) && raw[i] != '=' {
			i++
		}
		key := strings.ToLower(raw[start:i])
		if key == "" {
			i++
			continue
		}
		for i < len(raw) && isSpace(raw[i]) {
			i++
		}
		val := ""
		if i < len(raw) && raw[i] == '=' {
			i++
			for i < len(raw) && isSpace(raw[i]) {
				i++
			}
			if i < len(raw) && (raw[i] == '"' || raw[i] == '\'') {
				q := raw[i]
				i++
				s := i
				for i < len(raw) && raw[i] != q {
					i++
				}
				val = raw[s:i]
				i++
			} else {
				s := i
				for i < len(raw) && !isSpace(raw[i]) {
					i++
				}
				val = raw[s:i]
			}
		}
		attrs[key] = html.UnescapeString(val)
	}
	return name, attrs
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }

// nodeText concatenates the text under n.
func nodeText(n *hnode) string {
	if n.tag == "" {
		return n.text
	}
	var b strings.Builder
	for _, c := range n.children {
		b.WriteString(nodeText(c))
	}
	return b.String()
}
