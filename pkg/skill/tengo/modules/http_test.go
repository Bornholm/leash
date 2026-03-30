package modules_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tengosdk "github.com/d5/tengo/v2"

	"github.com/bornholm/leash/pkg/skill"
	skilltengo "github.com/bornholm/leash/pkg/skill/tengo"
	"github.com/bornholm/leash/pkg/skill/tengo/modules"
)

// --- Helpers ---

func getStr(t *testing.T, m *tengosdk.ImmutableMap, key string) string {
	t.Helper()
	v, ok := m.Value[key]
	if !ok {
		t.Fatalf("response missing key %q", key)
	}
	s, ok := v.(*tengosdk.String)
	if !ok {
		t.Fatalf("response[%q] is %T, want *tengosdk.String", key, v)
	}
	return s.Value
}

func getInt(t *testing.T, m *tengosdk.ImmutableMap, key string) int64 {
	t.Helper()
	v, ok := m.Value[key]
	if !ok {
		t.Fatalf("response missing key %q", key)
	}
	n, ok := v.(*tengosdk.Int)
	if !ok {
		t.Fatalf("response[%q] is %T, want *tengosdk.Int", key, v)
	}
	return n.Value
}

func getHeaders(t *testing.T, m *tengosdk.ImmutableMap) *tengosdk.ImmutableMap {
	t.Helper()
	v, ok := m.Value["headers"]
	if !ok {
		t.Fatal("response missing key \"headers\"")
	}
	hm, ok := v.(*tengosdk.ImmutableMap)
	if !ok {
		t.Fatalf("response[\"headers\"] is %T, want *tengosdk.ImmutableMap", v)
	}
	return hm
}

func callFn(t *testing.T, name string, args ...tengosdk.Object) *tengosdk.ImmutableMap {
	t.Helper()
	fn, ok := modules.HTTPModule[name].(*tengosdk.UserFunction)
	if !ok {
		t.Fatalf("modules.HTTPModule[%q] is not a *UserFunction", name)
	}
	result, err := fn.Value(args...)
	if err != nil {
		t.Fatalf("http.%s: unexpected Go error: %v", name, err)
	}
	m, ok := result.(*tengosdk.ImmutableMap)
	if !ok {
		t.Fatalf("http.%s returned %T, want *tengosdk.ImmutableMap", name, result)
	}
	return m
}

func str(s string) *tengosdk.String { return &tengosdk.String{Value: s} }

func tengoMap(pairs ...string) *tengosdk.Map {
	m := &tengosdk.Map{Value: map[string]tengosdk.Object{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Value[pairs[i]] = str(pairs[i+1])
	}
	return m
}

// --- Tests TengoMapToHeaders ---

func TestTengoMapToHeaders_EmptyMap(t *testing.T) {
	fn := modules.HTTPModule["get"].(*tengosdk.UserFunction)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Passer une map vide comme headers
	resp := callFn(t, "get", str(srv.URL), tengoMap())
	if getInt(t, resp, "status") != 200 {
		t.Errorf("status = %d, want 200", getInt(t, resp, "status"))
	}
	_ = fn
}

func TestTengoMapToHeaders_ImmutableMap(t *testing.T) {
	// Vérifier que *ImmutableMap est accepté comme headers
	var receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Test")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	immutableHeaders := &tengosdk.ImmutableMap{
		Value: map[string]tengosdk.Object{"X-Test": str("immutable-value")},
	}

	fn := modules.HTTPModule["get"].(*tengosdk.UserFunction)
	result, err := fn.Value(str(srv.URL), immutableHeaders)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result

	if receivedHeader != "immutable-value" {
		t.Errorf("X-Test header = %q, want %q", receivedHeader, "immutable-value")
	}
}

func TestTengoMapToHeaders_NonStringValue_Error(t *testing.T) {
	fn := modules.HTTPModule["get"].(*tengosdk.UserFunction)
	badHeaders := &tengosdk.Map{
		Value: map[string]tengosdk.Object{"X-Bad": &tengosdk.Int{Value: 42}},
	}
	_, err := fn.Value(str("http://localhost"), badHeaders)
	if err == nil {
		t.Fatal("expected error for non-string header value, got nil")
	}
	if !strings.Contains(err.Error(), "X-Bad") {
		t.Errorf("error should mention the bad key, got: %v", err)
	}
}

func TestTengoMapToHeaders_NonMapType_Error(t *testing.T) {
	fn := modules.HTTPModule["get"].(*tengosdk.UserFunction)
	_, err := fn.Value(str("http://localhost"), &tengosdk.Int{Value: 42})
	if err == nil {
		t.Fatal("expected error for non-map headers argument, got nil")
	}
	if !strings.Contains(err.Error(), "map") {
		t.Errorf("error should mention 'map', got: %v", err)
	}
}

// --- Tests GET ---

func TestHTTPModule_Get_Simple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resp := callFn(t, "get", str(srv.URL))
	if getInt(t, resp, "status") != 200 {
		t.Errorf("status = %d, want 200", getInt(t, resp, "status"))
	}
	if getStr(t, resp, "body") != `{"ok":true}` {
		t.Errorf("body = %q, want {\"ok\":true}", getStr(t, resp, "body"))
	}
	if getStr(t, resp, "err") != "" {
		t.Errorf("error = %q, want empty", getStr(t, resp, "err"))
	}
}

func TestHTTPModule_Get_WithHeaders(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resp := callFn(t, "get", str(srv.URL), tengoMap("Authorization", "Bearer test-token"))
	if getInt(t, resp, "status") != 200 {
		t.Errorf("status = %d, want 200", getInt(t, resp, "status"))
	}
	if receivedAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", receivedAuth, "Bearer test-token")
	}
}

func TestHTTPModule_Get_WrongArgCount(t *testing.T) {
	fn := modules.HTTPModule["get"].(*tengosdk.UserFunction)

	_, err := fn.Value()
	if err == nil {
		t.Fatal("expected error for 0 args, got nil")
	}

	_, err = fn.Value(str("http://localhost"), tengoMap(), tengoMap())
	if err == nil {
		t.Fatal("expected error for 3 args, got nil")
	}
}

func TestHTTPModule_Get_NonStringURL_Error(t *testing.T) {
	fn := modules.HTTPModule["get"].(*tengosdk.UserFunction)
	_, err := fn.Value(&tengosdk.Int{Value: 42})
	if err == nil {
		t.Fatal("expected error for non-string url, got nil")
	}
}

func TestHTTPModule_Get_InvalidURL_NoGoError(t *testing.T) {
	resp := callFn(t, "get", str("not-a-valid-url://\x00"))
	if getStr(t, resp, "err") == "" {
		t.Error("expected non-empty error field for invalid URL")
	}
	if getInt(t, resp, "status") != 0 {
		t.Errorf("status = %d, want 0 for network error", getInt(t, resp, "status"))
	}
}

func TestHTTPModule_Get_ConnectionRefused_NoGoError(t *testing.T) {
	resp := callFn(t, "get", str("http://127.0.0.1:1"))
	if getStr(t, resp, "err") == "" {
		t.Error("expected non-empty error field for refused connection")
	}
	if getInt(t, resp, "status") != 0 {
		t.Errorf("status = %d, want 0 for network error", getInt(t, resp, "status"))
	}
}

// --- Tests POST ---

func TestHTTPModule_Post(t *testing.T) {
	var receivedBody, receivedCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		receivedCT = r.Header.Get("Content-Type")
		w.WriteHeader(201)
		w.Write([]byte("created"))
	}))
	defer srv.Close()

	resp := callFn(t, "post", str(srv.URL), str(`{"name":"test"}`), str("application/json"))
	if getInt(t, resp, "status") != 201 {
		t.Errorf("status = %d, want 201", getInt(t, resp, "status"))
	}
	if getStr(t, resp, "body") != "created" {
		t.Errorf("body = %q, want \"created\"", getStr(t, resp, "body"))
	}
	if receivedBody != `{"name":"test"}` {
		t.Errorf("received body = %q, want %q", receivedBody, `{"name":"test"}`)
	}
	if receivedCT != "application/json" {
		t.Errorf("Content-Type = %q, want %q", receivedCT, "application/json")
	}
}

func TestHTTPModule_Post_WithCustomHeaders(t *testing.T) {
	var receivedID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedID = r.Header.Get("X-Request-ID")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resp := callFn(t, "post",
		str(srv.URL),
		str("body"),
		str("text/plain"),
		tengoMap("X-Request-ID", "abc-123"),
	)
	if getInt(t, resp, "status") != 200 {
		t.Errorf("status = %d, want 200", getInt(t, resp, "status"))
	}
	if receivedID != "abc-123" {
		t.Errorf("X-Request-ID = %q, want %q", receivedID, "abc-123")
	}
}

func TestHTTPModule_Post_WrongArgCount(t *testing.T) {
	fn := modules.HTTPModule["post"].(*tengosdk.UserFunction)

	_, err := fn.Value(str("http://localhost"), str("body"))
	if err == nil {
		t.Fatal("expected error for 2 args, got nil")
	}

	_, err = fn.Value(str("http://localhost"), str("body"), str("text/plain"), tengoMap(), tengoMap())
	if err == nil {
		t.Fatal("expected error for 5 args, got nil")
	}
}

// --- Tests PUT ---

func TestHTTPModule_Put(t *testing.T) {
	var receivedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resp := callFn(t, "put", str(srv.URL), str(`{"key":"val"}`), str("application/json"))
	if getInt(t, resp, "status") != 200 {
		t.Errorf("status = %d, want 200", getInt(t, resp, "status"))
	}
	if receivedMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", receivedMethod)
	}
}

// --- Tests DELETE ---

func TestHTTPModule_Delete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	resp := callFn(t, "delete", str(srv.URL))
	if getInt(t, resp, "status") != 204 {
		t.Errorf("status = %d, want 204", getInt(t, resp, "status"))
	}
}

func TestHTTPModule_Delete_WithHeaders(t *testing.T) {
	var receivedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("Authorization")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	callFn(t, "delete", str(srv.URL), tengoMap("Authorization", "Bearer delete-token"))
	if receivedToken != "Bearer delete-token" {
		t.Errorf("Authorization = %q, want %q", receivedToken, "Bearer delete-token")
	}
}

// --- Tests request générique ---

func TestHTTPModule_Request_Generic_PATCH(t *testing.T) {
	var receivedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resp := callFn(t, "request",
		str("patch"),
		str(srv.URL),
		str(`{"delta":1}`),
		str("application/json"),
		tengoMap(),
	)
	if getInt(t, resp, "status") != 200 {
		t.Errorf("status = %d, want 200", getInt(t, resp, "status"))
	}
	if receivedMethod != "PATCH" {
		t.Errorf("method = %q, want PATCH (lowercased input uppercased)", receivedMethod)
	}
}

func TestHTTPModule_Request_WrongArgCount(t *testing.T) {
	fn := modules.HTTPModule["request"].(*tengosdk.UserFunction)

	_, err := fn.Value(str("GET"), str("http://localhost"), str(""), str(""))
	if err == nil {
		t.Fatal("expected error for 4 args, got nil")
	}
}

func TestHTTPModule_Request_InvalidHeaderType(t *testing.T) {
	fn := modules.HTTPModule["request"].(*tengosdk.UserFunction)
	_, err := fn.Value(
		str("GET"),
		str("http://localhost"),
		str(""),
		str(""),
		&tengosdk.Int{Value: 99},
	)
	if err == nil {
		t.Fatal("expected error for non-map headers, got nil")
	}
}

// --- Tests headers de réponse ---

func TestHTTPModule_ResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(200)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	resp := callFn(t, "get", str(srv.URL))
	headers := getHeaders(t, resp)

	ctVal, ok := headers.Value["Content-Type"]
	if !ok {
		t.Fatal("response headers missing Content-Type")
	}
	if s, ok := ctVal.(*tengosdk.String); !ok || s.Value != "application/json" {
		t.Errorf("Content-Type = %v, want \"application/json\"", ctVal)
	}

	customVal, ok := headers.Value["X-Custom-Header"]
	if !ok {
		t.Fatal("response headers missing X-Custom-Header")
	}
	if s, ok := customVal.(*tengosdk.String); !ok || s.Value != "custom-value" {
		t.Errorf("X-Custom-Header = %v, want \"custom-value\"", customVal)
	}
}

// --- Test bout-en-bout via LoadScript ---

func TestHTTPModule_EndToEnd_ViaLoadScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("hello from server"))
	}))
	defer srv.Close()

	script := []byte(`/* skill
name: http_fetch
description: Fetches a URL and writes the body
category: http
args:
  - name: url
    description: URL to fetch
    required: true
*/

http := import("http")
resp := http.get(args[0])
if resp.err != "" {
    ewrite("error: " + resp.err + "\n")
    exit_code = 1
} else {
    write(resp.body)
}
`)

	sk, err := skilltengo.LoadScript(script)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	call := &skill.Call{
		Args:   []string{srv.URL},
		Flags:  map[string]string{},
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v (stderr: %s)", err, stderr.String())
	}

	if got := stdout.String(); got != "hello from server" {
		t.Errorf("stdout = %q, want %q", got, "hello from server")
	}
}
