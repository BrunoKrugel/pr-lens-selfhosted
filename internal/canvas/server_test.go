package canvas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const baseURL = "https://lens.example.com"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(baseURL)
}

func do(s *Server, method, path string, headers map[string]string, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, baseURL+path, reader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func mint(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rec := do(s, http.MethodPost, "/api/canvas", nil, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint status = %d, want 201", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("mint body not json: %v", err)
	}
	return m
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, status, rec.Body.String())
	}
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("error envelope not json: %v", err)
	}
	if e.Error.Code != code {
		t.Fatalf("error code = %q, want %q", e.Error.Code, code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
}

func TestMintCreatesEmptyCanvas(t *testing.T) {
	s := newTestServer(t)
	m := mint(t, s)

	id, _ := m["id"].(string)
	token, _ := m["writeToken"].(string)
	rev, _ := m["rev"].(float64)
	view, _ := m["viewUrl"].(string)
	edit, _ := m["editUrl"].(string)
	embed, _ := m["embedUrl"].(string)

	if len(id) != 22 || len(token) != 22 {
		t.Fatalf("id/token not 22 base64url chars: id=%q token=%q", id, token)
	}
	for _, v := range []string{id, token} {
		if strings.ContainsAny(v, "+/=") {
			t.Fatalf("%q is not base64url, no padding", v)
		}
	}
	if rev != 0 {
		t.Fatalf("mint rev = %v, want 0", rev)
	}
	wantView := baseURL + "/c/" + id
	if view != wantView {
		t.Fatalf("viewUrl = %q, want %q", view, wantView)
	}
	if edit != wantView+"#w="+token {
		t.Fatalf("editUrl = %q, want fragment carrying token", edit)
	}
	if embed != wantView+".svg" {
		t.Fatalf("embedUrl = %q, want %q", embed, wantView+".svg")
	}
	if rec := do(s, http.MethodGet, "/api/canvas/"+id, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("fetch on minted-but-unpushed canvas status = %d, want 404", rec.Code)
	}
}

func TestFetchAnswersLatestRevision(t *testing.T) {
	s := newTestServer(t)
	m := mint(t, s)
	id, _ := m["id"].(string)
	token, _ := m["writeToken"].(string)

	doc := `{"kind":"graph","schemaVersion":"0.1.1"}`
	rec := do(s, http.MethodPut, "/api/canvas/"+id, map[string]string{
		"Authorization": "Bearer " + token,
		"If-Match":      "0",
		"Content-Type":  "application/json",
	}, doc)
	if rec.Code != http.StatusOK {
		t.Fatalf("push status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var pushed struct {
		Rev int `json:"rev"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pushed); err != nil || pushed.Rev != 1 {
		t.Fatalf("push rev = %v, want 1", rec.Body.String())
	}

	rec = do(s, http.MethodGet, "/api/canvas/"+id, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch status = %d, want 200", rec.Code)
	}
	var got struct {
		ID       string          `json:"id"`
		Rev      int             `json:"rev"`
		Document json.RawMessage `json:"document"`
		Tiles    []any           `json:"tiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("fetch body not json: %v", err)
	}
	if got.ID != id || got.Rev != 1 {
		t.Fatalf("fetch id/rev = %q/%d, want %q/1", got.ID, got.Rev, id)
	}
	if string(got.Document) != doc {
		t.Fatalf("document = %s, want %s", got.Document, doc)
	}
	if got.Tiles == nil || len(got.Tiles) != 0 {
		t.Fatalf("tiles = %v, want empty non-null list", got.Tiles)
	}
}

func TestPushCheckOrder(t *testing.T) {
	s := newTestServer(t)
	m := mint(t, s)
	id, _ := m["id"].(string)
	token, _ := m["writeToken"].(string)
	doc := `{"kind":"graph","schemaVersion":"0.1.1"}`
	auth := map[string]string{"Authorization": "Bearer " + token, "If-Match": "0"}

	assertError(t, do(s, http.MethodPut, "/api/canvas/"+id, nil, doc),
		http.StatusBadRequest, "INVALID_REQUEST")

	assertError(t, do(s, http.MethodPut, "/api/canvas/"+id,
		map[string]string{"Authorization": "Bearer " + token, "If-Match": "abc"}, doc),
		http.StatusBadRequest, "INVALID_REQUEST")

	assertError(t, do(s, http.MethodPut, "/api/canvas/"+id, auth, strings.Repeat("a", 5_000_001)),
		http.StatusRequestEntityTooLarge, "TOO_LARGE")

	assertError(t, do(s, http.MethodPut, "/api/canvas/"+id, auth, "not json"),
		http.StatusBadRequest, "INVALID_REQUEST")

	assertError(t, do(s, http.MethodPut, "/api/canvas/"+id,
		map[string]string{"Authorization": "Bearer wrongwrongwrongwrong", "If-Match": "0"}, doc),
		http.StatusNotFound, "NOT_FOUND")

	assertError(t, do(s, http.MethodPut, "/api/canvas/nope-nope-nope-nope", auth, doc),
		http.StatusNotFound, "NOT_FOUND")

	assertError(t, do(s, http.MethodPut, "/api/canvas/"+id, auth, `{"kind":"other"}`),
		http.StatusUnprocessableEntity, "INVALID_DOCUMENT")

	rec := do(s, http.MethodPut, "/api/canvas/"+id, auth, doc)
	if rec.Code != http.StatusOK {
		t.Fatalf("first push status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var stale struct {
		Error struct {
			Code string `json:"code"`
			Rev  int    `json:"rev"`
		} `json:"error"`
	}
	if err := json.Unmarshal(do(s, http.MethodPut, "/api/canvas/"+id, auth, doc).Body.Bytes(), &stale); err != nil {
		t.Fatalf("stale push body not json: %v", err)
	}
	if stale.Error.Code != "REVISION_MOVED" || stale.Error.Rev != 1 {
		t.Fatalf("stale push = %s, want REVISION_MOVED with rev 1", do(s, http.MethodPut, "/api/canvas/"+id, auth, doc).Body.String())
	}

	quoted := map[string]string{"Authorization": "Bearer " + token, "If-Match": `"1"`}
	if rec = do(s, http.MethodPut, "/api/canvas/"+id, quoted, doc); rec.Code != http.StatusOK {
		t.Fatalf("quoted If-Match status = %d, want 200", rec.Code)
	}
}

func TestRotateReplayRules(t *testing.T) {
	s := newTestServer(t)
	m := mint(t, s)
	id, _ := m["id"].(string)
	token, _ := m["writeToken"].(string)
	next := "Ab3dEf5gHi7jKl9mNo1pQr"

	rotate := func(bearer, body string) *httptest.ResponseRecorder {
		h := map[string]string{"Content-Type": "application/json"}
		if bearer != "" {
			h["Authorization"] = "Bearer " + bearer
		}
		return do(s, http.MethodPost, "/api/canvas/"+id+"/rotate", h, body)
	}

	assertError(t, rotate(token, `{"writeToken":"short"}`), http.StatusBadRequest, "INVALID_REQUEST")
	assertError(t, rotate(token, `{}`), http.StatusBadRequest, "INVALID_REQUEST")
	assertError(t, rotate("", fmt.Sprintf(`{"writeToken":%q}`, next)), http.StatusNotFound, "NOT_FOUND")
	assertError(t, rotate("wrongwrongwrongwrong", fmt.Sprintf(`{"writeToken":%q}`, next)), http.StatusNotFound, "NOT_FOUND")

	rec := rotate(token, fmt.Sprintf(`{"writeToken":%q}`, next))
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, want 200", rec.Code)
	}
	var r struct {
		ID      string `json:"id"`
		EditURL string `json:"editUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || r.ID != id {
		t.Fatalf("rotate body = %s", rec.Body.String())
	}
	if r.EditURL != baseURL+"/c/"+id+"#w="+next {
		t.Fatalf("editUrl = %q, want new token fragment", r.EditURL)
	}

	oldDoc := `{"kind":"graph","schemaVersion":"0.1.1"}`
	assertError(t, do(s, http.MethodPut, "/api/canvas/"+id, map[string]string{
		"Authorization": "Bearer " + token, "If-Match": "0",
	}, oldDoc), http.StatusNotFound, "NOT_FOUND")

	replay := rotate(token, fmt.Sprintf(`{"writeToken":%q}`, next))
	if replay.Code != http.StatusOK {
		t.Fatalf("rotate replay status = %d, want 200", replay.Code)
	}
	if r := rotate(next, fmt.Sprintf(`{"writeToken":%q}`, next)); r.Code != http.StatusOK {
		t.Fatalf("token check rotate status = %d, want 200", r.Code)
	}
}

func TestDeleteRemovesCanvas(t *testing.T) {
	s := newTestServer(t)
	m := mint(t, s)
	id, _ := m["id"].(string)
	token, _ := m["writeToken"].(string)
	h := map[string]string{"Authorization": "Bearer " + token}

	assertError(t, do(s, http.MethodDelete, "/api/canvas/"+id, nil, ""), http.StatusNotFound, "NOT_FOUND")
	assertError(t, do(s, http.MethodDelete, "/api/canvas/"+id,
		map[string]string{"Authorization": "Bearer wrongwrongwrong"}, ""), http.StatusNotFound, "NOT_FOUND")

	rec := do(s, http.MethodDelete, "/api/canvas/"+id, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", rec.Code)
	}
	var d struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || !d.Deleted {
		t.Fatalf("delete body = %s, want deleted true", rec.Body.String())
	}
	assertError(t, do(s, http.MethodGet, "/api/canvas/"+id, nil, ""), http.StatusNotFound, "NOT_FOUND")
}

func TestCanvasPageAnswersEveryId(t *testing.T) {
	s := newTestServer(t)
	if rec := do(s, http.MethodGet, "/c/"+strings.Repeat("a", 22), nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("/c/{id} status = %d, want 200", rec.Code)
	}
}

func TestUnknownRouteIsErrorEnvelope(t *testing.T) {
	s := newTestServer(t)
	assertError(t, do(s, http.MethodGet, "/api/other", nil, ""), http.StatusNotFound, "NOT_FOUND")
}

func TestStoreSurvivesMultipleCanvases(t *testing.T) {
	s := newTestServer(t)
	a := mint(t, s)
	b := mint(t, s)
	if a["id"] == b["id"] {
		t.Fatalf("two mints share an id")
	}
	idA, _ := a["id"].(string)
	rec := do(s, http.MethodGet, "/api/canvas/"+idA, nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second mint should not touch the first canvas")
	}
}

var _ = bytes.MinRead

func TestCanvasPageServesViewer(t *testing.T) {
	s := newTestServer(t)
	rec := do(s, http.MethodGet, "/c/"+strings.Repeat("a", 22), nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("canvas page status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="app"`) || !strings.Contains(body, "viewer.js") {
		t.Fatalf("canvas page missing app root or viewer script:\n%s", body)
	}

	rec = do(s, http.MethodGet, "/c/"+strings.Repeat("a", 22)+".svg", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("canvas .svg page status = %d, want 200", rec.Code)
	}

	rec = do(s, http.MethodGet, "/viewer.js", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer.js status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("viewer.js content type = %q", ct)
	}
}

func TestPolyfillsServed(t *testing.T) {
	s := newTestServer(t)
	rec := do(s, http.MethodGet, "/polyfills.js", nil, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Buffer") {
		t.Fatalf("polyfills.js status = %d, body %d bytes", rec.Code, rec.Body.Len())
	}
}
