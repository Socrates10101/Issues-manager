// Package api exposes the store over HTTP. Routes map 1:1 onto the issues CLI
// subcommands.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Socrates10101/issues-manager/internal/store"
)

// Server wires a store and optional bearer token into an http.Handler.
type Server struct {
	st    *store.Store
	token string
}

func New(st *store.Store, token string) *Server {
	return &Server{st: st, token: token}
}

// Handler returns the routed, auth-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /projects", s.handleProjects)
	mux.HandleFunc("GET /issues", s.handleList)
	mux.HandleFunc("POST /issues", s.handleCreate)
	mux.HandleFunc("GET /issues/{project}/{number}", s.handleGet)
	mux.HandleFunc("PATCH /issues/{project}/{number}", s.handleUpdate)
	mux.HandleFunc("POST /issues/{project}/{number}/archive", s.handleArchive)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			if r.Header.Get("Authorization") != "Bearer "+s.token {
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.Filter{Project: q.Get("project"), Status: q.Get("status")}
	switch q.Get("archived") {
	case "true", "1":
		t := true
		f.Archived = &t
	case "false", "0":
		fa := false
		f.Archived = &fa
	}
	list, err := s.st.List(f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []store.Issue{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.List(store.Filter{})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	seen := map[string]int{}
	for _, i := range list {
		seen[i.Project]++
	}
	type proj struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	out := []proj{}
	for name, count := range seen {
		out = append(out, proj{name, count})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	project, number, ok := pathID(w, r)
	if !ok {
		return
	}
	iss, err := s.st.Get(project, number)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		Title   string `json:"title"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	iss, err := s.st.Create(in.Project, in.Title, in.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, iss)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	project, number, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Title  *string `json:"title"`
		Status *string `json:"status"`
		Body   *string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	iss, err := s.st.Update(project, number, in.Title, in.Status, in.Body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	project, number, ok := pathID(w, r)
	if !ok {
		return
	}
	iss, err := s.st.Archive(project, number)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

func pathID(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	project := r.PathValue("project")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid number")
		return "", 0, false
	}
	return project, number, true
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrNotResolved):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrBadStatus):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
