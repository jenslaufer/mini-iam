package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	expected := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains",
		"Content-Security-Policy":            "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'",
		"Referrer-Policy":                    "strict-origin-when-cross-origin",
		"Permissions-Policy":                 "camera=(), microphone=(), geolocation=(), payment=()",
		"Cross-Origin-Opener-Policy":         "same-origin",
		"X-Permitted-Cross-Domain-Policies":  "none",
	}
	for header, want := range expected {
		if got := rr.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestSecurityHeadersOnAPIResponse(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	expected := map[string]string{
		"X-Content-Type-Options":             "nosniff",
		"X-Frame-Options":                    "DENY",
		"Strict-Transport-Security":          "max-age=63072000; includeSubDomains",
		"Content-Security-Policy":            "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'",
		"Referrer-Policy":                    "strict-origin-when-cross-origin",
		"Permissions-Policy":                 "camera=(), microphone=(), geolocation=(), payment=()",
		"Cross-Origin-Opener-Policy":         "same-origin",
		"X-Permitted-Cross-Domain-Policies":  "none",
	}
	for header, want := range expected {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestCORSEmptyRejectsUnknownOrigin(t *testing.T) {
	handler := CORSMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("OPTIONS", "/token", nil)
	req.Header.Set("Origin", "http://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q, want empty for unknown origin with empty allowlist", got)
	}
}

func TestCORSAllowlistMatchesOrigin(t *testing.T) {
	handler := CORSMiddleware("http://app.example.com,http://localhost:3000")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("ACAO = %q, want http://localhost:3000", got)
	}
}

func TestCORSAllowlistRejectsNonMatchingOrigin(t *testing.T) {
	handler := CORSMiddleware("http://app.example.com")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Origin", "http://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO = %q, want empty for non-matching origin", got)
	}
}

func TestCORSWildcardStillWorks(t *testing.T) {
	handler := CORSMiddleware("*")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Origin", "http://anything.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ACAO = %q, want * for wildcard mode", got)
	}
}
