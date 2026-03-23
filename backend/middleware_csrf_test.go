package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// passthrough handler for wrapping with CSRF middleware
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// --- CSRF Token Basics ---

func TestCSRFPostWithoutTokenReturns403(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("POST", "/authorize", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF token: got %d, want 403", rr.Code)
	}
}

func TestCSRFPostWithValidTokenSucceeds(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	// First GET to obtain a token cookie
	getReq := httptest.NewRequest("GET", "/authorize", nil)
	getRR := httptest.NewRecorder()
	handler.ServeHTTP(getRR, getReq)

	token := extractCSRFCookie(t, getRR)
	if token == "" {
		t.Fatal("GET did not set csrf_token cookie")
	}

	// POST with valid token
	postReq := httptest.NewRequest("POST", "/authorize", strings.NewReader("csrf_token="+token))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	postRR := httptest.NewRecorder()
	handler.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusOK {
		t.Errorf("POST with valid CSRF token: got %d, want 200", postRR.Code)
	}
}

func TestCSRFInvalidTokenReturns403(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	// GET to obtain a real token cookie
	getReq := httptest.NewRequest("GET", "/authorize", nil)
	getRR := httptest.NewRecorder()
	handler.ServeHTTP(getRR, getReq)

	token := extractCSRFCookie(t, getRR)
	if token == "" {
		t.Fatal("GET did not set csrf_token cookie")
	}

	// POST with wrong token in form body
	postReq := httptest.NewRequest("POST", "/authorize", strings.NewReader("csrf_token=wrongtoken"))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	postRR := httptest.NewRecorder()
	handler.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusForbidden {
		t.Errorf("POST with wrong CSRF token: got %d, want 403", postRR.Code)
	}
}

func TestCSRFExpiredTokenReturns403(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	// Use a token that looks valid but is expired.
	// The middleware should embed expiry in the token or track it server-side.
	expiredToken := "expired.token.value"
	postReq := httptest.NewRequest("POST", "/authorize", strings.NewReader("csrf_token="+expiredToken))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: "csrf_token", Value: expiredToken})
	postRR := httptest.NewRecorder()
	handler.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusForbidden {
		t.Errorf("POST with expired CSRF token: got %d, want 403", postRR.Code)
	}
}

func TestCSRFTokenSingleUse(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	// GET to obtain token
	getReq := httptest.NewRequest("GET", "/authorize", nil)
	getRR := httptest.NewRecorder()
	handler.ServeHTTP(getRR, getReq)

	token := extractCSRFCookie(t, getRR)
	if token == "" {
		t.Fatal("GET did not set csrf_token cookie")
	}

	// First POST — should succeed
	postReq1 := httptest.NewRequest("POST", "/authorize", strings.NewReader("csrf_token="+token))
	postReq1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq1.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	postRR1 := httptest.NewRecorder()
	handler.ServeHTTP(postRR1, postReq1)

	if postRR1.Code != http.StatusOK {
		t.Fatalf("first POST with CSRF token: got %d, want 200", postRR1.Code)
	}

	// Second POST with same token — should fail
	postReq2 := httptest.NewRequest("POST", "/authorize", strings.NewReader("csrf_token="+token))
	postReq2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq2.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	postRR2 := httptest.NewRecorder()
	handler.ServeHTTP(postRR2, postReq2)

	if postRR2.Code != http.StatusForbidden {
		t.Errorf("second POST with same CSRF token: got %d, want 403", postRR2.Code)
	}
}

func TestCSRFTokenPerSession(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	// Two separate GET requests should produce different tokens
	getReq1 := httptest.NewRequest("GET", "/authorize", nil)
	getRR1 := httptest.NewRecorder()
	handler.ServeHTTP(getRR1, getReq1)
	token1 := extractCSRFCookie(t, getRR1)

	getReq2 := httptest.NewRequest("GET", "/authorize", nil)
	getRR2 := httptest.NewRecorder()
	handler.ServeHTTP(getRR2, getReq2)
	token2 := extractCSRFCookie(t, getRR2)

	if token1 == "" || token2 == "" {
		t.Fatal("GET requests did not set csrf_token cookies")
	}
	if token1 == token2 {
		t.Error("different sessions got the same CSRF token")
	}
}

// --- Bypass Rules ---

func TestCSRFBearerAuthSkipsValidation(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("POST", "/authorize", nil)
	req.Header.Set("Authorization", "Bearer some-jwt-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("POST with Bearer auth: got %d, want 200 (CSRF bypass)", rr.Code)
	}
}

func TestCSRFJSONContentTypeSkipsValidation(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("POST", "/authorize", strings.NewReader(`{"foo":"bar"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("POST with application/json: got %d, want 200 (CSRF bypass)", rr.Code)
	}
}

func TestCSRFGetRequestNoValidation(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("GET", "/authorize", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET request: got %d, want 200", rr.Code)
	}
}

func TestCSRFHeadRequestNoValidation(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("HEAD", "/authorize", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("HEAD request: got %d, want 200", rr.Code)
	}
}

func TestCSRFOptionsRequestNoValidation(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("OPTIONS", "/authorize", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("OPTIONS request: got %d, want 200", rr.Code)
	}
}

// --- Cookie Handling ---

func TestCSRFGetRequestSetsTokenCookie(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("GET", "/authorize", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	token := extractCSRFCookie(t, rr)
	if token == "" {
		t.Error("GET request did not set csrf_token cookie")
	}
}

func TestCSRFCookieHttpOnly(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("GET", "/authorize", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookie := findCSRFCookie(rr)
	if cookie == nil {
		t.Fatal("no csrf_token cookie set")
	}
	if !cookie.HttpOnly {
		t.Error("csrf_token cookie missing HttpOnly flag")
	}
}

func TestCSRFCookieSameSite(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("GET", "/authorize", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookie := findCSRFCookie(rr)
	if cookie == nil {
		t.Fatal("no csrf_token cookie set")
	}
	if cookie.SameSite != http.SameSiteStrictMode && cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("csrf_token cookie SameSite = %v, want Strict or Lax", cookie.SameSite)
	}
}

func TestCSRFNoCookieOnAPIEndpoints(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	// /admin/* endpoints are API-only, no cookie needed
	for _, path := range []string{"/admin/users", "/admin/campaigns", "/health"} {
		req := httptest.NewRequest("GET", path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		cookie := findCSRFCookie(rr)
		if cookie != nil {
			t.Errorf("GET %s should not set csrf_token cookie, but did", path)
		}
	}
}

// --- Integration: Protected Endpoints ---

func TestCSRFProtectsAuthorizeEndpoint(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("POST", "/authorize", strings.NewReader("response_type=code"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("POST /authorize without CSRF token: got %d, want 403", rr.Code)
	}
}

func TestCSRFProtectsUnsubscribeEndpoint(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("POST", "/unsubscribe/some-token", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("POST /unsubscribe without CSRF token: got %d, want 403", rr.Code)
	}
}

func TestCSRFProtectsActivateEndpoint(t *testing.T) {
	handler := CSRFMiddleware(okHandler)
	req := httptest.NewRequest("POST", "/activate/some-token", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("POST /activate without CSRF token: got %d, want 403", rr.Code)
	}
}

// --- Test Helpers ---

// extractCSRFCookie returns the csrf_token cookie value from the response, or "".
func extractCSRFCookie(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	c := findCSRFCookie(rr)
	if c == nil {
		return ""
	}
	return c.Value
}

// findCSRFCookie returns the csrf_token Set-Cookie from the response, or nil.
func findCSRFCookie(rr *httptest.ResponseRecorder) *http.Cookie {
	resp := http.Response{Header: rr.Header()}
	for _, c := range resp.Cookies() {
		if c.Name == "csrf_token" {
			return c
		}
	}
	return nil
}
