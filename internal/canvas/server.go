// Package canvas implements version 1 of the PR Lens canvas API:
// five routes, one error envelope, and an in-memory store keyed by id.
//
// The contract is documented at https://github.com/coldteadotai/pr-lens/blob/main/docs/canvas-api.md.
package canvas

import (
	"canvas-server/internal/webui"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	idLen          = 22 // 128 bits, base64url, no padding
	maxBodyBytes   = 4_000_000
	revisionHeader = "If-Match"
	bearerPrefix   = "Bearer "
)

// Server answers the canvas API against an in-memory store.
type Server struct {
	base     string // base URL without trailing slash, e.g. https://lens.example.com
	mu       sync.Mutex
	canvases map[string]*canvas
}

type canvas struct {
	tokenHash [sha256.Size]byte
	rev       int
	document  json.RawMessage
}

// New builds a Server whose viewUrl, editUrl and embedUrl are rooted at base.
func New(base string) *Server {
	return &Server{base: strings.TrimRight(base, "/"), canvases: map[string]*canvas{}}
}

func newToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("canvas: reading entropy: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func hashToken(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

// tokenMatches compares a presented token against a stored hash in constant time.
func tokenMatches(hash [sha256.Size]byte, token string) bool {
	sum := hashToken(token)
	return subtle.ConstantTimeCompare(sum[:], hash[:]) == 1
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Rev     *int   `json:"rev,omitempty"`
}

var errNotFound = errors.New("canvas: not found")

func writeError(w http.ResponseWriter, status int, code, message string, rev *int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorDetail{Code: code, Message: message, Rev: rev}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) urls(id, token string) (view, edit, embed string) {
	view = s.base + "/c/" + id
	return view, view + "#w=" + token, view + ".svg"
}

type mintReply struct {
	ID         string `json:"id"`
	WriteToken string `json:"writeToken"`
	Rev        int    `json:"rev"`
	ViewURL    string `json:"viewUrl"`
	EditURL    string `json:"editUrl"`
	EmbedURL   string `json:"embedUrl"`
}

func (s *Server) mint(w http.ResponseWriter, r *http.Request) {
	id, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INVALID_REQUEST", "the server could not mint an id", nil)
		return
	}
	token, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INVALID_REQUEST", "the server could not mint a token", nil)
		return
	}
	s.mu.Lock()
	s.canvases[id] = &canvas{tokenHash: hashToken(token)}
	s.mu.Unlock()

	view, edit, embed := s.urls(id, token)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, mintReply{ID: id, WriteToken: token, Rev: 0, ViewURL: view, EditURL: edit, EmbedURL: embed})
}

type fetchReply struct {
	ID       string          `json:"id"`
	Rev      int             `json:"rev"`
	ViewURL  string          `json:"viewUrl"`
	EmbedURL string          `json:"embedUrl"`
	Document json.RawMessage `json:"document"`
	Tiles    []any           `json:"tiles"`
}

func (s *Server) fetch(w http.ResponseWriter, id string) {
	s.mu.Lock()
	c, ok := s.canvases[id]
	var rev int
	var document json.RawMessage
	if ok {
		rev, document = c.rev, c.document
	}
	s.mu.Unlock()
	if !ok || document == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no canvas answers to that id", nil)
		return
	}
	view, _, embed := s.urls(id, "")
	writeJSON(w, fetchReply{ID: id, Rev: rev, ViewURL: view, EmbedURL: embed, Document: document, Tiles: []any{}})
}

func parseIfMatch(header string) (int, bool) {
	v := strings.Trim(header, `"`)
	rev, err := strconv.Atoi(v)
	if err != nil || v == "" {
		return 0, false
	}
	return rev, true
}

type pushReply struct {
	ID       string `json:"id"`
	Rev      int    `json:"rev"`
	ViewURL  string `json:"viewUrl"`
	EditURL  string `json:"editUrl"`
	EmbedURL string `json:"embedUrl"`
	Tiles    []any  `json:"tiles"`
}

func (s *Server) push(w http.ResponseWriter, r *http.Request, id string) {
	// Check order follows the contract, first failure wins.
	wantRev, ok := parseIfMatch(r.Header.Get(revisionHeader))
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "the If-Match header is missing or not an integer revision", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "the body could not be read", nil)
		return
	}
	if len(body) > maxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "TOO_LARGE", "the document is above the 4,000,000-byte limit", nil)
		return
	}

	var doc json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "the body is not JSON", nil)
		return
	}

	var header struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(body, &header)

	s.mu.Lock()
	c, ok := s.canvases[id]
	if ok && !tokenMatches(c.tokenHash, bearer(r)) {
		ok = false
	}
	if ok && c.rev != wantRev {
		at := c.rev
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "REVISION_MOVED",
			"the canvas has moved on since you pulled it; pull again, then push", &at)
		return
	}
	if ok {
		if header.Kind != "graph" {
			s.mu.Unlock()
			writeError(w, http.StatusUnprocessableEntity, "INVALID_DOCUMENT",
				"the body is not a graph document", nil)
			return
		}
		c.rev = wantRev + 1
		c.document = doc
		rev := c.rev
		s.mu.Unlock()
		view, edit, embed := s.urls(id, bearer(r))
		writeJSON(w, pushReply{ID: id, Rev: rev, ViewURL: view, EditURL: edit, EmbedURL: embed, Tiles: []any{}})
		return
	}
	s.mu.Unlock()
	writeError(w, http.StatusNotFound, "NOT_FOUND", "no canvas answers to that id, or the token is wrong", nil)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	return strings.TrimPrefix(h, bearerPrefix)
}

func isValidToken(v string) bool {
	if len(v) != idLen {
		return false
	}
	for _, ch := range v {
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
		default:
			return false
		}
	}
	return true
}

type rotateReply struct {
	ID      string `json:"id"`
	EditURL string `json:"editUrl"`
}

func (s *Server) rotate(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		WriteToken string `json:"writeToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !isValidToken(req.WriteToken) {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "the body needs a writeToken of 22 base64url characters", nil)
		return
	}

	s.mu.Lock()
	c, ok := s.canvases[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no canvas answers to that id", nil)
		return
	}
	if tokenMatches(c.tokenHash, req.WriteToken) {
		s.mu.Unlock() // retry finishing the same rotation, or a token check: nothing changes
		_, edit, _ := s.urls(id, req.WriteToken)
		writeJSON(w, rotateReply{ID: id, EditURL: edit})
		return
	}
	if !tokenMatches(c.tokenHash, bearer(r)) {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no canvas answers to that id, or the token is wrong", nil)
		return
	}
	c.tokenHash = hashToken(req.WriteToken)
	s.mu.Unlock()
	_, edit, _ := s.urls(id, req.WriteToken)
	writeJSON(w, rotateReply{ID: id, EditURL: edit})
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	c, ok := s.canvases[id]
	matched := ok && tokenMatches(c.tokenHash, bearer(r))
	if matched {
		delete(s.canvases, id)
	}
	s.mu.Unlock()
	if !matched {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no canvas answers to that id, or the token is wrong", nil)
		return
	}
	writeJSON(w, struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}{ID: id, Deleted: true})
}

func servePage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(webui.Index)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case path == "api/canvas" && r.Method == http.MethodPost:
		s.mint(w, r)
	case strings.HasPrefix(path, "c/"):
		servePage(w)
	case path == "viewer.js", path == "polyfills.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		if path == "viewer.js" {
			_, _ = w.Write(webui.ViewerJS)
		} else {
			_, _ = w.Write(webui.Polyfills)
		}
	case strings.HasPrefix(path, "api/canvas/"):
		rest := strings.TrimPrefix(path, "api/canvas/")
		id, action, _ := strings.Cut(rest, "/")
		switch {
		case action == "" && r.Method == http.MethodGet:
			s.fetch(w, id)
		case action == "" && r.Method == http.MethodPut:
			s.push(w, r, id)
		case action == "rotate" && r.Method == http.MethodPost:
			s.rotate(w, r, id)
		case action == "" && r.Method == http.MethodDelete:
			s.delete(w, r, id)
		default:
			writeError(w, http.StatusNotFound, "NOT_FOUND", "that route does not exist", nil)
		}
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "that route does not exist", nil)
	}
}
