package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBody = 1 << 20

func (a *app) serve(args []string) error {
	fs := a.flags("serve")
	port := fs.Int("port", 7777, "port on 127.0.0.1")
	noOpen := fs.Bool("no-open", false, "don't open the browser")
	if err := noArgs(fs, args); err != nil {
		return err
	}
	c := a.config()
	if _, err := os.Stat(c.root); err != nil {
		return fmt.Errorf("no projects folder at %s", c.root)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)))
	if err != nil {
		return fmt.Errorf("listening on port %d: %w (another project-board running? try --port)", *port, err)
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/", actual)
	fmt.Fprintf(a.stdout, "project-board: serving %s (Ctrl-C stops it)\n", url)
	if !*noOpen {
		if err := a.openURL(url); err != nil {
			fmt.Fprintln(a.stderr, "project-board: opening the browser:", err)
		}
	}
	srv := &http.Server{Handler: newServer(a, actual), ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(ln)
}

// server serves the live board. Every request rescans or rereads the registry,
// so edits made elsewhere (set, a text editor) show up on refresh.
type server struct {
	a     *app
	hosts []string // the Host headers we answer, against DNS rebinding
	mu    sync.Mutex
	mux   *http.ServeMux
}

func newServer(a *app, port int) *server {
	p := strconv.Itoa(port)
	s := &server{a: a, hosts: []string{"127.0.0.1:" + p, "localhost:" + p}, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /{$}", s.page)
	s.mux.HandleFunc("GET /api/board", s.board)
	s.mux.HandleFunc("POST /api/project", s.project)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !slices.Contains(s.hosts, r.Host) {
		writeError(w, http.StatusForbidden, "unexpected Host "+strconv.Quote(r.Host))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		// A browser sends Origin on every cross-origin POST; only the board's
		// own page may write.
		if o := r.Header.Get("Origin"); o != "" && !slices.Contains(s.hosts, trimScheme(o)) {
			writeError(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

// trimScheme turns an http:// origin into host:port; any other scheme yields ""
// so it never matches.
func trimScheme(origin string) string {
	if rest, ok := strings.CutPrefix(origin, "http://"); ok {
		return rest
	}
	return ""
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(boardHTML)
}

func (s *server) board(w http.ResponseWriter, r *http.Request) {
	b, err := s.a.config().buildBoard()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// project sets curated fields on one project, creating its registry entry when
// the folder exists but has none, and answers with the project as /api/board
// shows it.
func (s *server) project(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing ?id=")
		return
	}
	var patch map[string]json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	if err := dec.Decode(&patch); err != nil || patch == nil {
		writeError(w, http.StatusBadRequest, "the body must be a JSON object of fields to set")
		return
	}
	c := s.a.config()
	status, err := s.update(c, id, patch)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	b, err := c.buildBoard(id)
	if err != nil || len(b.Projects) == 0 {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("saved, but rescanning %s failed: %v", id, err))
		return
	}
	writeJSON(w, http.StatusOK, b.Projects[0])
}

func (s *server) update(c *config, id string, patch map[string]json.RawMessage) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, err := loadRegistry(c.registry)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	p := reg.find(id)
	if p == nil {
		// Only a real project folder gets a new entry; the id comes from the
		// folder listing, never from joining the request's id onto a path.
		ids, _, err := folders(c.root)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if !slices.Contains(ids, id) {
			return http.StatusNotFound, fmt.Errorf("no project %q: no registry entry and no folder", id)
		}
		p = &project{ID: id, Name: id[strings.LastIndex(id, "/")+1:], Status: "paused"}
		reg.Projects = append(reg.Projects, p)
	}
	if err := applyPatch(p, patch); err != nil {
		return http.StatusBadRequest, err
	}
	p.Updated = c.now.Format("2006-01-02")
	if err := reg.save(c.registry); err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusOK, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, code int, msg string) {
	b, _ := json.Marshal(map[string]string{"error": msg})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write(append(b, '\n'))
}
