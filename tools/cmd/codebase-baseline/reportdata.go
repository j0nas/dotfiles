package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// The page's script reduces every string in a data block to this set of tags.
var richTextTags = map[string]bool{"b": true, "i": true, "em": true, "strong": true, "code": true, "a": true, "sup": true, "sub": true, "span": true, "br": true, "small": true, "abbr": true, "dfn": true}

var reYearMonth = regexp.MustCompile(`^\d{4}-\d{2}$`)

// loadData reads data/<block>.json: it must parse, must not repeat a key, its
// strings must be safe and in the page's rich-text subset, and its shape must
// be one the page can draw. It returns the compact text to embed.
func (b *builder) loadData(blk tmplBlock) (string, any, bool) {
	file := "data/" + blk.ID + ".json"
	raw, err := os.ReadFile(filepath.Join(b.o.src, "data", blk.ID+".json"))
	if err != nil {
		b.problem("%s: %v", file, err)
		return "", nil, false
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		b.problem("%s: %s", file, jsonErrText(raw, err))
		return "", nil, false
	}
	desc := file
	if m, ok := probe.(map[string]any); ok {
		if _, has := m["from"]; has {
			var ok2 bool
			switch blk.ID {
			case "scoreboard-data":
				raw, ok2 = b.scoreboardFromFile(file, raw)
			case "treemap-data":
				raw, ok2 = b.treemapFromFile(file, raw)
			default:
				b.problem("%s: \"from\" works only in scoreboard-data.json and treemap-data.json", file)
			}
			if !ok2 {
				return "", nil, false
			}
			desc = file + " (from its \"from\" file)"
		}
	}
	if dups := duplicateKeys(raw); len(dups) > 0 {
		for _, d := range dups {
			b.problem("%s: the key %s appears twice", desc, d)
		}
		return "", nil, false
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		b.problem("%s: %v", desc, err)
		return "", nil, false
	}

	// Same example content as the template?
	var tv any
	d2 := json.NewDecoder(strings.NewReader(blk.JSON))
	d2.UseNumber()
	if d2.Decode(&tv) == nil && reflect.DeepEqual(v, tv) {
		b.dummy("%s (equals the example data)", file)
	}

	n0 := len(b.res.problems)
	walkJSONStrings(v, "", func(p, s string) {
		if why := scriptyString(s); why != "" {
			b.problem("%s at %s: contains %s, which would run when the page renders text fields as HTML", desc, p, why)
		}
		if !strings.ContainsAny(s, "<") {
			return
		}
		root := parseHTML(s)
		var walk func(n *hnode)
		walk = func(n *hnode) {
			if n.tag != "" && n.tag != "#root" {
				if !richTextTags[n.tag] {
					b.problem("%s at %s: <%s> is stripped by the page (text fields keep only b, i, em, strong, code, a, sup, sub, span, br, small, abbr, dfn); write a literal < as &lt;", desc, p, n.tag)
				}
				if h, ok := n.attrs["href"]; ok && n.tag == "a" && !strings.HasPrefix(h, "#") && !strings.HasPrefix(h, "https://") {
					b.problem("%s at %s: the page keeps only # and https:// links, not %q", desc, p, truncate(h, 40))
				}
			}
			for _, c := range n.children {
				walk(c)
			}
		}
		walk(root)
	})
	if len(b.res.problems) > n0 {
		return "", nil, false
	}
	if bytes.Contains(bytes.ToLower(raw), []byte("<script")) {
		b.problem("%s: contains <script (in a key); a block ending early would break the page", desc)
		return "", nil, false
	}

	n1 := len(b.res.problems)
	b.checkBlockShape(blk.ID, desc, v)
	if len(b.res.problems) > n1 {
		return "", nil, false
	}

	var cb bytes.Buffer
	if err := json.Compact(&cb, raw); err != nil {
		b.problem("%s: %v", desc, err)
		return "", nil, false
	}
	// </ would end the <script> block; <!-- starts an escape inside it.
	text := strings.ReplaceAll(cb.String(), "</", `<\/`)
	text = strings.ReplaceAll(text, "<!--", `<!--`)
	return text, v, true
}

// duplicateKeys lists object keys that appear twice in one object.
func duplicateKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var dups []string
	var read func(path string) bool
	read = func(path string) bool {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		d, ok := tok.(json.Delim)
		if !ok {
			return true
		}
		switch d {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return false
				}
				k, _ := kt.(string)
				if seen[k] {
					if path == "" {
						dups = append(dups, fmt.Sprintf("%q", k))
					} else {
						dups = append(dups, fmt.Sprintf("%q in %s", k, path))
					}
				}
				seen[k] = true
				p := k
				if path != "" {
					p = path + "." + k
				}
				if !read(p) {
					return false
				}
			}
			_, err = dec.Token()
			return err == nil
		case '[':
			i := 0
			for dec.More() {
				if !read(fmt.Sprintf("%s[%d]", path, i)) {
					return false
				}
				i++
			}
			_, err = dec.Token()
			return err == nil
		}
		return true
	}
	read("")
	return dups
}

// ---- scoreboard rows from the file `scoreboard` wrote ----

type sbPointer struct {
	From string `json:"from"`
	Note string `json:"note"`
}

type sbFileDoc struct {
	Note string      `json:"note"`
	Rows []sbPageRow `json:"rows"`
}

// sbPageRow keeps the fields the page reads; the tool's other fields (source,
// headline) name snapshot fields and stay out of the report.
type sbPageRow struct {
	Metric string          `json:"metric"`
	Value  json.RawMessage `json:"value"`
	First  json.RawMessage `json:"first"`
	Better string          `json:"better"`
	Trend  json.RawMessage `json:"trend"`
	How    string          `json:"how"`
	AsOf   string          `json:"asOf,omitempty"`
	Unit   string          `json:"unit,omitempty"`
	Delta  string          `json:"delta,omitempty"`
}

func (b *builder) scoreboardFromFile(file string, raw []byte) ([]byte, bool) {
	var ptr sbPointer
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ptr); err != nil {
		b.problem("%s: %v (a pointer file is {\"from\": \"<path>\", \"note\": \"<text>\"})", file, err)
		return nil, false
	}
	if ptr.From == "" {
		b.problem("%s: \"from\" is empty", file)
		return nil, false
	}
	p := ptr.From
	if !filepath.IsAbs(p) {
		p = filepath.Join(b.o.src, p)
	}
	sb, err := os.ReadFile(p)
	if err != nil {
		b.problem("%s: from %s: %v", file, ptr.From, err)
		return nil, false
	}
	var doc sbFileDoc
	if err := json.Unmarshal(sb, &doc); err != nil {
		b.problem("%s: from %s: %s", file, ptr.From, jsonErrText(sb, err))
		return nil, false
	}
	if len(doc.Rows) == 0 {
		b.problem("%s: from %s: no rows (is it the file codebase-baseline scoreboard wrote?)", file, ptr.From)
		return nil, false
	}
	for i := range doc.Rows {
		if t := bytes.TrimSpace(doc.Rows[i].Trend); len(t) == 0 || string(t) == "null" {
			doc.Rows[i].Trend = json.RawMessage("[]")
		}
		if len(bytes.TrimSpace(doc.Rows[i].First)) == 0 {
			doc.Rows[i].First = json.RawMessage("null")
		}
		if len(bytes.TrimSpace(doc.Rows[i].Value)) == 0 {
			doc.Rows[i].Value = json.RawMessage("null")
		}
	}
	note := doc.Note
	if ptr.Note != "" {
		note = ptr.Note
	}
	out, err := json.Marshal(struct {
		Note string      `json:"note,omitempty"`
		Rows []sbPageRow `json:"rows"`
	}{note, doc.Rows})
	if err != nil {
		b.problem("%s: %v", file, err)
		return nil, false
	}
	return out, true
}

// treemapFromFile reads the file `hotspots` wrote ({tree, churnLabel, ...}) and
// keeps what the page draws: {name, churnLabel, children}.
func (b *builder) treemapFromFile(file string, raw []byte) ([]byte, bool) {
	var ptr struct {
		From       string `json:"from"`
		ChurnLabel string `json:"churnLabel"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ptr); err != nil {
		b.problem("%s: %v (a pointer file is {\"from\": \"<path>\"}, optionally with \"churnLabel\")", file, err)
		return nil, false
	}
	p := ptr.From
	if p == "" {
		b.problem("%s: \"from\" is empty", file)
		return nil, false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(b.o.src, p)
	}
	tb, err := os.ReadFile(p)
	if err != nil {
		b.problem("%s: from %s: %v", file, ptr.From, err)
		return nil, false
	}
	var doc struct {
		ChurnLabel string `json:"churnLabel"`
		Tree       *struct {
			Name     string          `json:"name"`
			Children json.RawMessage `json:"children"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(tb, &doc); err != nil {
		b.problem("%s: from %s: %s", file, ptr.From, jsonErrText(tb, err))
		return nil, false
	}
	if doc.Tree == nil || len(doc.Tree.Children) == 0 {
		b.problem("%s: from %s: no tree (is it the file codebase-baseline hotspots wrote?)", file, ptr.From)
		return nil, false
	}
	label := doc.ChurnLabel
	if ptr.ChurnLabel != "" {
		label = ptr.ChurnLabel
	}
	out, err := json.Marshal(struct {
		Name       string          `json:"name"`
		ChurnLabel string          `json:"churnLabel,omitempty"`
		Children   json.RawMessage `json:"children"`
	}{doc.Tree.Name, label, doc.Tree.Children})
	if err != nil {
		b.problem("%s: %v", file, err)
		return nil, false
	}
	return out, true
}

// ---- shapes ----

func asMap(v any) (map[string]any, bool) { m, ok := v.(map[string]any); return m, ok }
func asList(v any) ([]any, bool)         { l, ok := v.([]any); return l, ok }
func asStr(v any) (string, bool)         { s, ok := v.(string); return s, ok }
func isNum(v any) bool                   { _, ok := v.(json.Number); return ok }
func numVal(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

func (b *builder) checkBlockShape(id, desc string, v any) {
	bad := func(format string, args ...any) { b.problem("%s: %s", desc, fmt.Sprintf(format, args...)) }
	switch id {
	case "meta-data":
		m, ok := asMap(v)
		if !ok {
			bad("must be an object {project, repo, ref, surveyed, asOf}")
			return
		}
		if p, _ := asStr(m["project"]); strings.TrimSpace(p) == "" {
			bad("project is missing (it names the page)")
		}
		if d, _ := asStr(m["asOf"]); !reISODate.MatchString(d) {
			bad("asOf must be an ISO date (YYYY-MM-DD)")
		}
	case "summary-data":
		m, ok := asMap(v)
		if !ok {
			bad("must be an object {verdict, facts, risks, actions}")
			return
		}
		if s, _ := asStr(m["verdict"]); strings.TrimSpace(s) == "" {
			bad("verdict is missing")
		}
		facts, _ := asList(m["facts"])
		risks, _ := asList(m["risks"])
		actions, _ := asList(m["actions"])
		for _, c := range []struct {
			what    string
			n, want int
		}{{"facts", len(facts), 5}, {"risks", len(risks), 5}, {"actions", len(actions), 3}} {
			if c.n != c.want {
				bad("needs exactly %d %s, has %d", c.want, c.what, c.n)
			}
		}
		for i, f := range facts {
			fm, _ := asMap(f)
			if s, _ := asStr(fm["v"]); s == "" {
				bad("facts[%d] needs a string v", i)
			}
			if s, _ := asStr(fm["l"]); s == "" {
				bad("facts[%d] needs a string l", i)
			}
		}
		for i, r := range risks {
			rm, _ := asMap(r)
			switch sev, _ := asStr(rm["sev"]); sev {
			case "crit", "high", "med", "good", "low":
			default:
				bad("risks[%d].sev must be crit, high, med, good or low", i)
			}
			if s, _ := asStr(rm["t"]); s == "" {
				bad("risks[%d] needs a string t", i)
			}
		}
		for i, a := range actions {
			if s, _ := asStr(a); s == "" {
				bad("actions[%d] must be a string", i)
			}
		}
	case "scoreboard-data":
		m, ok := asMap(v)
		rows, _ := asList(m["rows"])
		if !ok || len(rows) == 0 {
			bad("needs rows: a list of {metric, value, first, better, trend, how, asOf}")
			return
		}
		for i, r := range rows {
			rm, _ := asMap(r)
			where := fmt.Sprintf("rows[%d]", i)
			if s, _ := asStr(rm["metric"]); s != "" {
				where = fmt.Sprintf("row %q", truncate(s, 50))
			} else {
				bad("%s has no metric", where)
			}
			if _, has := rm["value"]; !has {
				bad("%s has no value", where)
			}
			if s, _ := asStr(rm["how"]); s == "" {
				bad("%s has no how (the command or count that reproduces it)", where)
			}
			switch s, _ := asStr(rm["better"]); s {
			case "up", "down", "none":
			default:
				bad("%s: better must be up, down or none", where)
			}
			if tr, ok := asList(rm["trend"]); !ok {
				bad("%s: trend must be a list of numbers (empty for none)", where)
			} else {
				for _, p := range tr {
					if !isNum(p) {
						bad("%s: trend holds a non-number", where)
						break
					}
				}
			}
		}
	case "treemap-data":
		m, ok := asMap(v)
		if !ok {
			bad("must be {name, churnLabel, children}")
			return
		}
		if _, ok := asList(m["children"]); !ok {
			bad("children is missing (directories {name, children}, files {name, path, loc, churn})")
			return
		}
		leaves := 0
		var walk func(n map[string]any)
		walk = func(n map[string]any) {
			ch, isDir := asList(n["children"])
			if !isDir {
				leaves++
				if !isNum(n["loc"]) {
					bad("file %q has no numeric loc", n["name"])
				}
				return
			}
			for _, c := range ch {
				if cm, ok := asMap(c); ok {
					walk(cm)
				}
			}
		}
		walk(m)
		if leaves > 700 {
			b.warn("%s: %d files; the treemap is meant for about 600 (hotspots --max-leaves 600)", desc, leaves)
		}
	case "commits-data":
		charts := chartList(v)
		if _, isList := v.([]any); !isList && len(charts) != 1 {
			bad("must be a chart object, or a list of them")
			return
		}
		if l, isList := v.([]any); isList && len(l) != len(charts) {
			bad("every entry of the list must be a chart object")
			return
		}
		if len(charts) == 0 {
			bad("needs at least one chart {id, unit, months, series}")
		}
		for _, c := range charts {
			id, _ := asStr(c["id"])
			if id == "" {
				bad("a chart has no id (the id of the element that holds it)")
				continue
			}
			months, _ := asList(c["months"])
			series, _ := asList(c["series"])
			if len(months) == 0 {
				bad("chart %q has no months", id)
			}
			for _, m := range months {
				if s, _ := asStr(m); !reYearMonth.MatchString(s) {
					bad("chart %q: month %v is not YYYY-MM", id, m)
					break
				}
			}
			if len(series) == 0 || len(series) > 4 {
				bad("chart %q needs 1 to 4 series, has %d", id, len(series))
			}
			for i, s := range series {
				sm, _ := asMap(s)
				vals, _ := asList(sm["values"])
				if len(vals) != len(months) {
					bad("chart %q: series %d has %d values for %d months", id, i+1, len(vals), len(months))
				}
			}
			if len(months) > 36 {
				b.warn("%s: chart %q has %d months; the page is made for at most 36", desc, id, len(months))
			}
		}
	case "bars-data":
		m, ok := asMap(v)
		if !ok {
			bad("must be an object keyed by the id of each chart's element")
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cm, ok := asMap(m[k])
			rows, rok := asList(cm["rows"])
			if !ok || !rok {
				bad("%q must be {unit, rows: [...]} (and \"kind\": \"share\" for a split bar)", k)
				continue
			}
			switch kind, _ := asStr(cm["kind"]); kind {
			case "", "bars", "share":
			default:
				bad("%q: kind must be bars or share", k)
			}
			for i, r := range rows {
				rm, _ := asMap(r)
				if _, g := rm["group"]; g {
					continue
				}
				if s, _ := asStr(rm["l"]); s == "" {
					bad("%q rows[%d] has no label l", k, i)
				}
				if !isNum(rm["v"]) {
					bad("%q rows[%d] has no numeric v", k, i)
				}
				switch c, _ := asStr(rm["c"]); c {
				case "", "s1", "b2", "b3", "b4", "high", "crit":
				default:
					bad("%q rows[%d]: colour c %q is not one of \"\", b2, b3, b4, high, crit", k, i, c)
				}
			}
		}
	case "quality-data":
		m, ok := asMap(v)
		if !ok {
			bad("must be {autonomy: {level, names}, harness: {level, names}, target}")
			return
		}
		for _, ax := range []struct {
			key      string
			lo, hi   float64
			nameWant int
		}{{"autonomy", 0, 5, 6}, {"harness", 1, 5, 5}} {
			am, _ := asMap(m[ax.key])
			lv, ok := numVal(am["level"])
			if !ok || lv < ax.lo || lv > ax.hi {
				bad("%s.level must be a number from %v to %v", ax.key, ax.lo, ax.hi)
			}
			if names, has := asList(am["names"]); has && len(names) != ax.nameWant {
				bad("%s.names needs %d names, has %d", ax.key, ax.nameWant, len(names))
			}
		}
	}
}
