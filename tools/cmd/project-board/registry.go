package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// status is one column of the board, in column order.
type status struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

var statuses = []status{
	{"active", "Active", "being worked on now (touched in the last ~3 weeks, unfinished)"},
	{"waiting", "Waiting", "blocked on something outside your control: parts in the mail, a person, a decision"},
	{"paused", "Paused", "started, unfinished, parked: where you left off and the next step matter most here"},
	{"idea", "Ideas", "researched or planned, nothing real built yet"},
	{"live", "Live", "shipped and in use or running; only upkeep left"},
	{"done", "Done", "finished one-off, nothing left"},
	{"dropped", "Dropped", "abandoned, deprecated, to decommission"},
}

func knownStatus(s string) bool {
	return slices.ContainsFunc(statuses, func(st status) bool { return st.ID == s })
}

func statusIDs() []string {
	ids := make([]string, len(statuses))
	for i, s := range statuses {
		ids[i] = s.ID
	}
	return ids
}

// Length limits for the curated prose; validate and set warn past them.
const (
	maxSummary  = 120
	maxLeftOff  = 350
	maxNext     = 160
	maxLoops    = 6
	maxLoopText = 140
)

// project is one registry entry. Keys the tool doesn't know are kept in extra
// and written back unchanged, so other tools and hand edits can add fields.
type project struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	Summary   string            `json:"summary,omitempty"`
	LeftOff   string            `json:"left_off,omitempty"`
	Next      string            `json:"next,omitempty"`
	BlockedOn string            `json:"blocked_on,omitempty"`
	OpenLoops []string          `json:"open_loops,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Links     map[string]string `json:"links,omitempty"`
	Related   []string          `json:"related,omitempty"`
	Hidden    bool              `json:"hidden,omitempty"`
	Pinned    bool              `json:"pinned,omitempty"`
	Updated   string            `json:"updated,omitempty"`

	extra map[string]json.RawMessage
}

type projectFields project // the same fields without the custom (un)marshalers

func (p *project) UnmarshalJSON(b []byte) error {
	var f projectFields
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	extra, err := extraKeys(b, projectKeys)
	if err != nil {
		return err
	}
	*p = project(f)
	p.extra = extra
	return nil
}

func (p project) MarshalJSON() ([]byte, error) {
	return withExtra(projectFields(p), p.extra)
}

// registry is projects.json.
type registry struct {
	Version  int        `json:"version"`
	Projects []*project `json:"projects"`

	extra map[string]json.RawMessage
}

type registryFields registry

func (r *registry) UnmarshalJSON(b []byte) error {
	var f registryFields
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	extra, err := extraKeys(b, registryKeys)
	if err != nil {
		return err
	}
	*r = registry(f)
	r.extra = extra
	return nil
}

func (r registry) MarshalJSON() ([]byte, error) {
	if r.Projects == nil {
		r.Projects = []*project{}
	}
	return withExtra(registryFields(r), r.extra)
}

var (
	projectKeys  = jsonKeys(reflect.TypeFor[projectFields]())
	registryKeys = jsonKeys(reflect.TypeFor[registryFields]())
)

// jsonKeys lists the JSON names of a struct's exported fields.
func jsonKeys(t reflect.Type) []string {
	var keys []string
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			name = f.Name
		}
		keys = append(keys, name)
	}
	return keys
}

// extraKeys returns the keys of the JSON object b that aren't in known.
func extraKeys(b []byte, known []string) (map[string]json.RawMessage, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	for _, k := range known {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}

// withExtra encodes v, a struct, and appends the extra keys (sorted) to the
// object, leaving out any that v already has.
func withExtra(v any, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := encode(v)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var buf bytes.Buffer
	buf.Write(b[:len(b)-1]) // without the closing brace
	empty := bytes.Equal(bytes.TrimSpace(b), []byte("{}"))
	for _, k := range keys {
		kb, err := encode(k)
		if err != nil {
			return nil, err
		}
		if !empty {
			buf.WriteByte(',')
		}
		empty = false
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(extra[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// encode marshals v without escaping <, > and &, so prose in projects.json stays
// readable, and without the trailing newline.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// loadRegistry reads projects.json. A missing file is an empty registry.
func loadRegistry(path string) (*registry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &registry{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var r registry
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r.Projects = slices.DeleteFunc(r.Projects, func(p *project) bool { return p == nil })
	return &r, nil
}

// save writes the registry atomically: 2-space indent, projects sorted by id,
// trailing newline.
func (r *registry) save(path string) error {
	if r.Version == 0 {
		r.Version = 1
	}
	slices.SortStableFunc(r.Projects, func(a, b *project) int { return strings.Compare(a.ID, b.ID) })
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return err
	}
	return writeAtomic(path, buf.Bytes())
}

// find returns the entry with id, or nil.
func (r *registry) find(id string) *project {
	for _, p := range r.Projects {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// patchable lists the curated fields POST /api/project and set may change.
var patchable = []string{"name", "status", "summary", "left_off", "next", "blocked_on", "open_loops", "tags", "links", "related", "hidden", "pinned"}

// readOnlyKeys may appear in a patch (a client sending back a whole project) and
// are ignored.
var readOnlyKeys = []string{"updated", "untriaged", "facts", "warnings"}

// applyPatch sets the curated fields in patch on p. A null value clears a field.
func applyPatch(p *project, patch map[string]json.RawMessage) error {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		raw := patch[k]
		null := bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
		if k == "id" {
			var id string
			if err := json.Unmarshal(raw, &id); err != nil || id != p.ID {
				return fmt.Errorf("id %s doesn't match the project %q", raw, p.ID)
			}
			continue
		}
		if slices.Contains(readOnlyKeys, k) {
			continue
		}
		if !slices.Contains(patchable, k) {
			return fmt.Errorf("unknown field %q (can set: %s)", k, strings.Join(patchable, ", "))
		}
		var err error
		switch k {
		case "name", "status", "summary", "left_off", "next", "blocked_on":
			var s string
			if !null {
				err = json.Unmarshal(raw, &s)
			}
			s = strings.TrimSpace(s)
			if err == nil {
				err = setString(p, k, s)
			}
		case "open_loops", "tags", "related":
			var list []string
			if !null {
				err = json.Unmarshal(raw, &list)
			}
			list = cleanList(list)
			switch k {
			case "open_loops":
				p.OpenLoops = list
			case "tags":
				p.Tags = list
			case "related":
				p.Related = list
			}
		case "links":
			var links map[string]string
			if !null {
				err = json.Unmarshal(raw, &links)
			}
			for lk, v := range links {
				if v = strings.TrimSpace(v); v == "" {
					delete(links, lk)
				} else {
					links[lk] = v
				}
			}
			if len(links) == 0 {
				links = nil
			}
			p.Links = links
		case "hidden", "pinned":
			var v bool
			if !null {
				err = json.Unmarshal(raw, &v)
			}
			if k == "hidden" {
				p.Hidden = v
			} else {
				p.Pinned = v
			}
		}
		if err != nil {
			var typeErr *json.UnmarshalTypeError
			if errors.As(err, &typeErr) {
				return fmt.Errorf("%s: expected %s, got %s", k, typeErr.Type, typeErr.Value)
			}
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	return nil
}

func setString(p *project, field, s string) error {
	switch field {
	case "name":
		if s == "" {
			return errors.New("name can't be empty")
		}
		p.Name = s
	case "status":
		if !knownStatus(s) {
			return fmt.Errorf("unknown status %q: use one of %s", s, strings.Join(statusIDs(), ", "))
		}
		p.Status = s
	case "summary":
		p.Summary = s
	case "left_off":
		p.LeftOff = s
	case "next":
		p.Next = s
	case "blocked_on":
		p.BlockedOn = s
	}
	return nil
}

// cleanList trims items and drops empty ones and repeats.
func cleanList(list []string) []string {
	var out []string
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// lengthWarnings reports curated fields past their length limits.
func lengthWarnings(p *project) []string {
	var w []string
	check := func(field, v string, limit int) {
		if n := utf8.RuneCountInString(v); n > limit {
			w = append(w, fmt.Sprintf("%s is %d characters (limit %d)", field, n, limit))
		}
	}
	check("summary", p.Summary, maxSummary)
	check("left_off", p.LeftOff, maxLeftOff)
	check("next", p.Next, maxNext)
	if len(p.OpenLoops) > maxLoops {
		w = append(w, fmt.Sprintf("%d open_loops (limit %d)", len(p.OpenLoops), maxLoops))
	}
	for i, l := range p.OpenLoops {
		check(fmt.Sprintf("open_loops[%d]", i+1), l, maxLoopText)
	}
	return w
}

// validateRegistry checks projects.json. Errors make the file unusable or wrong;
// warnings are worth fixing.
func validateRegistry(r *registry, folders map[string]bool) (errs, warns []string) {
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, p := range r.Projects {
		ids[p.ID] = true
	}
	for i, p := range r.Projects {
		label := p.ID
		if label == "" {
			label = fmt.Sprintf("projects[%d]", i)
		}
		if p.ID == "" {
			errs = append(errs, label+": no id")
		} else if seen[p.ID] {
			errs = append(errs, label+": duplicate id")
		}
		seen[p.ID] = true
		if p.Name == "" {
			errs = append(errs, label+": no name")
		}
		if p.Status == "" {
			errs = append(errs, label+": no status")
		} else if !knownStatus(p.Status) {
			errs = append(errs, fmt.Sprintf("%s: unknown status %q (use one of %s)", label, p.Status, strings.Join(statusIDs(), ", ")))
		}
		if p.ID != "" && !folders[p.ID] {
			warns = append(warns, label+": no folder with this id under the root")
		}
		for _, w := range lengthWarnings(p) {
			warns = append(warns, label+": "+w)
		}
		if p.Updated != "" {
			if _, err := time.Parse("2006-01-02", p.Updated); err != nil {
				warns = append(warns, fmt.Sprintf("%s: updated %q isn't YYYY-MM-DD", label, p.Updated))
			}
		}
		for _, rel := range p.Related {
			if !ids[rel] && !folders[rel] {
				warns = append(warns, fmt.Sprintf("%s: related %q matches no project", label, rel))
			}
		}
	}
	return errs, warns
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename has happened
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
