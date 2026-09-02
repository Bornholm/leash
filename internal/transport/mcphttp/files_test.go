package mcphttp

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newFilesTestServer monte un serveur avec des bornes fichiers explicites.
func newFilesTestServer(t *testing.T, maxFile, quota int64) (*httptest.Server, *Manager, *ServerConfig) {
	t.Helper()
	cfg := &ServerConfig{
		hmacSecret:          []byte("test-hmac-secret"),
		WorkspaceRoot:       t.TempDir(),
		TTL:                 time.Hour,
		DiscHeader:          "X-Workspace",
		DiscURLParam:        "workspace",
		SandboxBackend:      SandboxBackendBwrap,
		MaxFileBytes:        maxFile,
		WorkspaceQuotaBytes: quota,
		APIKeys: []*APIKeyConfig{
			{Name: "default", keyHash: sha256.Sum256([]byte("valid-key"))},
		},
	}
	mgr := NewManager(cfg, testFactory)
	srv := NewServer(cfg, mgr)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		mgr.Shutdown()
	})
	return ts, mgr, cfg
}

func doFile(t *testing.T, ts *httptest.Server, method, path, workspace string, body []byte) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer valid-key")
	if workspace != "" {
		req.Header.Set("X-Workspace", workspace)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestFiles_PutGetDeleteRoundTrip(t *testing.T) {
	ts, _, _ := newFilesTestServer(t, 1<<20, 4<<20)
	payload := []byte("bonjour \x00\x01 binaire")

	resp := doFile(t, ts, http.MethodPut, "/files/sub/dir/clip.mp4", "org-1/member-1", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d, want 201", resp.StatusCode)
	}
	var entry fileEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		t.Fatalf("decoding PUT response: %v", err)
	}
	if entry.Path != "sub/dir/clip.mp4" || entry.Size != int64(len(payload)) {
		t.Fatalf("PUT result = %+v", entry)
	}

	get := doFile(t, ts, http.MethodGet, "/files/sub/dir/clip.mp4", "org-1/member-1", nil)
	defer get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", get.StatusCode)
	}
	if ct := get.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("Content-Type = %q, want video/mp4", ct)
	}
	got, _ := io.ReadAll(get.Body)
	if !bytes.Equal(got, payload) {
		t.Fatalf("GET body = %q, want %q", got, payload)
	}

	list := doFile(t, ts, http.MethodGet, "/files/", "org-1/member-1", nil)
	defer list.Body.Close()
	var listing fileListing
	if err := json.NewDecoder(list.Body).Decode(&listing); err != nil {
		t.Fatalf("decoding listing: %v", err)
	}
	if len(listing.Files) != 1 || listing.Files[0].Path != "sub/dir/clip.mp4" {
		t.Fatalf("listing = %+v", listing)
	}
	if listing.Total != int64(len(payload)) {
		t.Fatalf("total = %d, want %d", listing.Total, len(payload))
	}

	del := doFile(t, ts, http.MethodDelete, "/files/sub/dir/clip.mp4", "org-1/member-1", nil)
	defer del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", del.StatusCode)
	}

	after := doFile(t, ts, http.MethodGet, "/files/sub/dir/clip.mp4", "org-1/member-1", nil)
	defer after.Body.Close()
	if after.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after DELETE status = %d, want 404", after.StatusCode)
	}
}

func TestFiles_RequiresAuthentication(t *testing.T) {
	ts, _, _ := newFilesTestServer(t, 1<<20, 4<<20)
	resp, err := http.Get(ts.URL + "/files/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestFiles_MissingDiscriminantIsRejected(t *testing.T) {
	ts, _, _ := newFilesTestServer(t, 1<<20, 4<<20)
	resp := doFile(t, ts, http.MethodGet, "/files/", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestFiles_PathTraversalIsRejected(t *testing.T) {
	ts, _, cfg := newFilesTestServer(t, 1<<20, 4<<20)

	for _, p := range []string{
		"/files/../escaped.txt",
		"/files/sub/../../escaped.txt",
		"/files/%2e%2e/escaped.txt",
	} {
		resp := doFile(t, ts, http.MethodPut, p, "org-1/member-1", []byte("nope"))
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusCreated {
			t.Fatalf("PUT %s was accepted (body %q)", p, body)
		}
	}

	// Rien n'a été écrit hors des répertoires de workspace.
	entries, err := os.ReadDir(cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("reading workspace root: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			t.Fatalf("fichier créé hors workspace: %s", e.Name())
		}
	}
}

func TestFiles_SymlinkEscapeIsRejected(t *testing.T) {
	ts, mgr, _ := newFilesTestServer(t, 1<<20, 4<<20)

	// Crée le workspace via un premier PUT légitime.
	resp := doFile(t, ts, http.MethodPut, "/files/seed.txt", "org-1/member-1", []byte("seed"))
	resp.Body.Close()

	mgr.mu.Lock()
	var dir string
	for _, ws := range mgr.spaces {
		dir = ws.dir
	}
	mgr.mu.Unlock()
	if dir == "" {
		t.Fatal("aucun workspace créé")
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "evasion")); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	put := doFile(t, ts, http.MethodPut, "/files/evasion/pwned.txt", "org-1/member-1", []byte("pwned"))
	put.Body.Close()
	if put.StatusCode == http.StatusCreated {
		t.Fatal("écriture via symlink acceptée")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Fatal("le fichier a été écrit hors du workspace via un lien symbolique")
	}
}

func TestFiles_FileTooLargeIsRejected(t *testing.T) {
	ts, _, _ := newFilesTestServer(t, 16, 4<<20)
	resp := doFile(t, ts, http.MethodPut, "/files/big.bin", "org-1/member-1", bytes.Repeat([]byte("a"), 64))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestFiles_QuotaIsEnforced(t *testing.T) {
	ts, _, _ := newFilesTestServer(t, 1<<20, 100)

	first := doFile(t, ts, http.MethodPut, "/files/a.bin", "org-1/member-1", bytes.Repeat([]byte("a"), 80))
	first.Body.Close()
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("premier PUT status = %d, want 201", first.StatusCode)
	}

	second := doFile(t, ts, http.MethodPut, "/files/b.bin", "org-1/member-1", bytes.Repeat([]byte("b"), 80))
	defer second.Body.Close()
	if second.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("second PUT status = %d, want 507", second.StatusCode)
	}

	// Réécrire le même fichier ne doit pas compter deux fois sa taille.
	rewrite := doFile(t, ts, http.MethodPut, "/files/a.bin", "org-1/member-1", bytes.Repeat([]byte("c"), 90))
	defer rewrite.Body.Close()
	if rewrite.StatusCode != http.StatusCreated {
		t.Fatalf("réécriture status = %d, want 201", rewrite.StatusCode)
	}
}

func TestFiles_DistinctDiscriminantsGetDistinctDirectories(t *testing.T) {
	ts, _, _ := newFilesTestServer(t, 1<<20, 4<<20)

	a := doFile(t, ts, http.MethodPut, "/files/note.txt", "org-1/member-1", []byte("membre A"))
	a.Body.Close()
	b := doFile(t, ts, http.MethodPut, "/files/note.txt", "org-1/member-2", []byte("membre B"))
	b.Body.Close()

	get := doFile(t, ts, http.MethodGet, "/files/note.txt", "org-1/member-1", nil)
	defer get.Body.Close()
	got, _ := io.ReadAll(get.Body)
	if string(got) != "membre A" {
		t.Fatalf("contenu du membre 1 = %q, want %q", got, "membre A")
	}
}

func TestFiles_RequestRefreshesWorkspaceTTL(t *testing.T) {
	ts, mgr, _ := newFilesTestServer(t, 1<<20, 4<<20)

	resp := doFile(t, ts, http.MethodPut, "/files/note.txt", "org-1/member-1", []byte("x"))
	resp.Body.Close()

	mgr.mu.Lock()
	var ws *Workspace
	for _, w := range mgr.spaces {
		ws = w
	}
	mgr.mu.Unlock()
	if ws == nil {
		t.Fatal("aucun workspace créé")
	}

	ws.lastAccess.Store(0)
	get := doFile(t, ts, http.MethodGet, "/files/note.txt", "org-1/member-1", nil)
	get.Body.Close()

	if ws.lastAccess.Load() == 0 {
		t.Fatal("une requête fichier n'a pas rafraîchi le TTL du workspace")
	}
}

func TestFiles_UnknownFileReturns404(t *testing.T) {
	ts, _, _ := newFilesTestServer(t, 1<<20, 4<<20)
	resp := doFile(t, ts, http.MethodGet, "/files/absent.txt", "org-1/member-1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
