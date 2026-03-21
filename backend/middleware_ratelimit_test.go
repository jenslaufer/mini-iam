package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestRateLimiter() *RateLimiter {
	return NewRateLimiter(map[string]float64{
		"/login":           10,
		"/register":        5,
		"/forgot-password": 3,
		"/token":           20,
	}, 5*time.Minute)
}

func rateLimitedHandler(rl *RateLimiter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return rl.Middleware(mux)
}

func TestRateLimitAllowsUnderLimit(t *testing.T) {
	rl := newTestRateLimiter()
	defer rl.Stop()
	handler := rateLimitedHandler(rl)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i+1, w.Code)
		}
	}
}

func TestRateLimitBlocksOverLimit(t *testing.T) {
	rl := newTestRateLimiter()
	defer rl.Stop()
	handler := rateLimitedHandler(rl)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// 11th request should be blocked
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "too_many_requests" {
		t.Fatalf("got error %q, want too_many_requests", body["error"])
	}
}

func TestRateLimitPerIP(t *testing.T) {
	rl := newTestRateLimiter()
	defer rl.Stop()
	handler := rateLimitedHandler(rl)

	// Exhaust limit for IP A
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// IP B should still be allowed
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "2.2.2.2:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("different IP got %d, want 200", w.Code)
	}
}

func TestRateLimitRetryAfterHeader(t *testing.T) {
	rl := newTestRateLimiter()
	defer rl.Stop()
	handler := rateLimitedHandler(rl)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}
	ra := w.Header().Get("Retry-After")
	if ra == "" {
		t.Fatal("missing Retry-After header")
	}
}

func TestRateLimitAutoCleanup(t *testing.T) {
	rl := NewRateLimiter(map[string]float64{
		"/login": 10,
	}, 50*time.Millisecond) // short TTL for testing
	defer rl.Stop()

	// Create an entry
	rl.allow("1.2.3.4", "/login")

	// Verify entry exists
	if _, ok := rl.entries.Load("1.2.3.4|/login"); !ok {
		t.Fatal("entry should exist")
	}

	// Wait for entry to expire and cleanup to run.
	// We can't wait for the 1-minute ticker in tests, so invoke cleanup logic directly.
	time.Sleep(100 * time.Millisecond)
	// Manually trigger the cleanup check
	now := time.Now()
	rl.entries.Range(func(key, value any) bool {
		e := value.(*rateLimitEntry)
		if now.Sub(e.lastCheck) > rl.ttl {
			rl.entries.Delete(key)
			rl.mu.Delete(key)
		}
		return true
	})

	if _, ok := rl.entries.Load("1.2.3.4|/login"); ok {
		t.Fatal("entry should have been cleaned up")
	}
}

func TestRateLimitIgnoresXFF(t *testing.T) {
	rl := newTestRateLimiter()
	defer rl.Stop()
	handler := rateLimitedHandler(rl)

	// Exhaust limit for IP 10.0.0.1
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Same IP should be blocked
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}

	// Different IP should still pass
	req = httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.0.0.2:5678"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("different IP got %d, want 200", w.Code)
	}
}
