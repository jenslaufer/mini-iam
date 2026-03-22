package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/jenslaufer/launch-kit/tenantctx"
)

const (
	csrfCookieName = "_csrf"
	csrfFieldName  = "csrf_token"
	csrfNonceLen   = 32
)

func generateNonce() (string, error) {
	b := make([]byte, csrfNonceLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func computeCSRFToken(nonce string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nonce))
	sig := hex.EncodeToString(mac.Sum(nil))
	return nonce + "." + sig
}

func validCSRFToken(token, nonce string, secret []byte) bool {
	if token == "" || nonce == "" {
		return false
	}
	expected := computeCSRFToken(nonce, secret)
	return hmac.Equal([]byte(token), []byte(expected))
}

// CSRFToken returns the CSRF token for embedding in forms.
func CSRFToken(r *http.Request) string {
	return tenantctx.CSRFTokenFromContext(r.Context())
}

// CSRFField returns an HTML hidden input with the CSRF token.
func CSRFField(r *http.Request) string {
	return fmt.Sprintf(`<input type="hidden" name="%s" value="%s">`, csrfFieldName, CSRFToken(r))
}

// CSRFMiddleware validates CSRF tokens on browser form POSTs.
// It skips requests with Authorization headers or JSON content type.
func CSRFMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var nonce string
			hasCookie := false

			if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
				nonce = c.Value
				hasCookie = true
			} else {
				n, err := generateNonce()
				if err != nil {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				nonce = n
			}

			// Set/refresh cookie
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookieName,
				Value:    nonce,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   r.TLS != nil,
			})

			// Store token in context for form rendering
			token := computeCSRFToken(nonce, secret)
			ctx := tenantctx.WithCSRFToken(r.Context(), token)
			r = r.WithContext(ctx)

			// Validate on state-changing methods for browser form flows
			if hasCookie && isStateChanging(r.Method) && !skipCSRF(r) {
				if err := r.ParseForm(); err != nil {
					http.Error(w, "Forbidden - invalid CSRF token", http.StatusForbidden)
					return
				}
				formToken := r.PostFormValue(csrfFieldName)
				if !validCSRFToken(formToken, nonce, secret) {
					http.Error(w, "Forbidden - invalid CSRF token", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isStateChanging(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
}

func skipCSRF(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return true
	}
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		return true
	}
	return false
}
