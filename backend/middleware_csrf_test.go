package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testCSRFSecret = []byte("test-secret-32-bytes-long-enough")

// okHandler returns a 200 handler, optionally capturing the CSRF token from context.
func csrfOKHandler(tokenOut *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tokenOut != nil {
			*tokenOut = CSRFToken(r)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func csrfWrapped(inner http.Handler) http.Handler {
	return CSRFMiddleware(testCSRFSecret)(inner)
}

// postForm builds a POST request with url-encoded body and attaches a _csrf cookie.
func postFormWithCookie(nonce, csrfToken string) *http.Request {
	body := "csrf_token=" + csrfToken + "&other=value"
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "_csrf", Value: nonce})
	return req
}

// TestCSRFPostWithoutTokenReturns403 checks that a POST with a _csrf cookie
// but no csrf_token form field is rejected.
func TestCSRFPostWithoutTokenReturns403(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader("other=value"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "_csrf", Value: "aabbcc"})

	rr := httptest.NewRecorder()
	csrfWrapped(csrfOKHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", rr.Code)
	}
}

// TestCSRFPostWithValidTokenSucceeds checks that a POST with matching cookie
// and form token passes through to the handler.
func TestCSRFPostWithValidTokenSucceeds(t *testing.T) {
	nonce := strings.Repeat("aa", 32) // 64-char hex nonce
	token := computeCSRFToken(nonce, testCSRFSecret)

	rr := httptest.NewRecorder()
	csrfWrapped(csrfOKHandler(nil)).ServeHTTP(rr, postFormWithCookie(nonce, token))

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
}

// TestCSRFBearerAuthSkipsValidation checks that requests with an Authorization
// Bearer header bypass CSRF validation entirely.
func TestCSRFBearerAuthSkipsValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/resource", strings.NewReader("other=value"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer sometoken")

	rr := httptest.NewRecorder()
	csrfWrapped(csrfOKHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
}

// TestCSRFJSONContentTypeSkipsValidation checks that JSON API requests bypass
// CSRF validation.
func TestCSRFJSONContentTypeSkipsValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/resource", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	csrfWrapped(csrfOKHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
}

// TestCSRFGetRequestSetsTokenInContext checks that a GET sets the _csrf cookie
// and places a non-empty token in the request context.
func TestCSRFGetRequestSetsTokenInContext(t *testing.T) {
	var capturedToken string
	req := httptest.NewRequest(http.MethodGet, "/page", nil)

	rr := httptest.NewRecorder()
	csrfWrapped(csrfOKHandler(&capturedToken)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
	if capturedToken == "" {
		t.Error("CSRFToken(r) returned empty string for GET request")
	}
	cookie := rr.Result().Cookies()
	var found bool
	for _, c := range cookie {
		if c.Name == "_csrf" {
			found = true
			break
		}
	}
	if !found {
		t.Error("_csrf cookie not set on GET response")
	}
}

// TestCSRFNoCookieSkipsValidation checks that a POST from a programmatic client
// (no _csrf cookie) passes through without CSRF enforcement.
func TestCSRFNoCookieSkipsValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader("other=value"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No _csrf cookie added.

	rr := httptest.NewRecorder()
	csrfWrapped(csrfOKHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
}

// TestCSRFInvalidTokenReturns403 checks that a tampered csrf_token is rejected.
func TestCSRFInvalidTokenReturns403(t *testing.T) {
	nonce := strings.Repeat("bb", 32)
	// Use a wrong token instead of computeCSRFToken(nonce, testCSRFSecret).
	wrongToken := nonce + ".deadbeefdeadbeef"

	rr := httptest.NewRecorder()
	csrfWrapped(csrfOKHandler(nil)).ServeHTTP(rr, postFormWithCookie(nonce, wrongToken))

	if rr.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", rr.Code)
	}
}
