package exchange

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

// Server is the passive billboard HTTP API. Construct with NewServer and
// mount the returned http.Handler behind whatever transport you like
// (typically a plain http.Server on an internal network; terminate TLS at a
// reverse proxy when exposing it).
type Server struct {
	store Store
}

// NewServer returns an http.Handler serving the billboard API backed by store.
func NewServer(store Store) http.Handler {
	s := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("POST /token/rotate", s.handleRotate)
	mux.HandleFunc("GET /items", s.handleListItems)
	mux.HandleFunc("POST /items", s.handleCreateItem)
	mux.HandleFunc("GET /items/{id}", s.handleGetItem)
	mux.HandleFunc("POST /items/{id}/comments", s.handleAddComment)
	mux.HandleFunc("PATCH /items/{id}", s.handleSetStatus)
	return mux
}

// ---- wire DTOs ----

type registerReq struct {
	Handle string `json:"handle"`
}
type tokenResp struct {
	Handle string `json:"handle"`
	Token  string `json:"token"`
}
type createItemReq struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Assignees []string `json:"assignees,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}
type commentReq struct {
	Body string `json:"body"`
}
type statusReq struct {
	Status string `json:"status"`
}
type itemsResp struct {
	Items []adept.ExchangeItem `json:"items"`
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// bearer extracts the raw token from the Authorization header.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// authUser resolves the authenticated member from the bearer token. Writes a
// 401 and returns ok=false when the token is missing or unknown.
func (s *Server) authUser(w http.ResponseWriter, r *http.Request) (adept.ExchangeUser, bool) {
	tok := bearer(r)
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "missing bearer token")
		return adept.ExchangeUser{}, false
	}
	u, found, err := s.store.UserByTokenHash(hashToken(tok))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return adept.ExchangeUser{}, false
	}
	if !found {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return adept.ExchangeUser{}, false
	}
	return u, true
}

func itemID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		writeErr(w, http.StatusBadRequest, "invalid item id")
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

// ---- handlers ----

// handleRegister issues a member token to a caller who presents the current
// bootstrap token.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	boot, err := s.store.BootstrapHash()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if boot == "" || !tokenMatches(bearer(r), boot) {
		writeErr(w, http.StatusUnauthorized, "invalid bootstrap token")
		return
	}
	var req registerReq
	if !decode(w, r, &req) {
		return
	}
	handle := strings.TrimSpace(req.Handle)
	if handle == "" {
		writeErr(w, http.StatusBadRequest, "handle is required")
		return
	}
	token, err := generateToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	user := adept.ExchangeUser{Handle: handle, TokenHash: hashToken(token), Role: adept.ExchangeRoleMember}
	if err := s.store.CreateUser(user); err != nil {
		if errors.Is(err, adept.ErrExchangeHandleTaken) {
			writeErr(w, http.StatusConflict, "handle already registered")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tokenResp{Handle: handle, Token: token})
}

// handleRotate swaps the caller's token for a fresh one, invalidating the old
// token immediately.
func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authUser(w, r)
	if !ok {
		return
	}
	token, err := generateToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, found, err := s.store.RotateUserToken(hashToken(bearer(r)), hashToken(token)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	} else if !found {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	writeJSON(w, http.StatusOK, tokenResp{Handle: u.Handle, Token: token})
}

func (s *Server) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authUser(w, r)
	if !ok {
		return
	}
	var req createItemReq
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	item, err := s.store.CreateItem(adept.ExchangeItem{
		Author:    u.Handle,
		Title:     req.Title,
		Body:      req.Body,
		Assignees: req.Assignees,
		Tags:      req.Tags,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authUser(w, r)
	if !ok {
		return
	}
	f := ListFilter{Handle: u.Handle, Status: r.URL.Query().Get("status")}
	if r.URL.Query().Get("mine") != "" {
		f.Mine = true
	}
	if f.Status != "" && !adept.ValidExchangeStatus(f.Status) {
		writeErr(w, http.StatusBadRequest, "invalid status filter")
		return
	}
	items, err := s.store.ListItems(f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, itemsResp{Items: items})
}

func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authUser(w, r); !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	item, err := s.store.GetItem(id)
	if err != nil {
		s.writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authUser(w, r)
	if !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var req commentReq
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "comment body is required")
		return
	}
	item, err := s.store.AddComment(id, adept.ExchangeComment{Author: u.Handle, Body: req.Body})
	if err != nil {
		s.writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleSetStatus changes an item's status. Only the author may move it
// (close/reopen), matching the "author owns lifecycle" rule.
func (s *Server) handleSetStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authUser(w, r)
	if !ok {
		return
	}
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var req statusReq
	if !decode(w, r, &req) {
		return
	}
	if !adept.ValidExchangeStatus(req.Status) {
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	item, err := s.store.GetItem(id)
	if err != nil {
		s.writeStoreErr(w, err)
		return
	}
	if item.Author != u.Handle {
		writeErr(w, http.StatusForbidden, "only the author can change status")
		return
	}
	updated, err := s.store.SetStatus(id, req.Status)
	if err != nil {
		s.writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, adept.ErrExchangeItemNotFound) {
		writeErr(w, http.StatusNotFound, "item not found")
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}
