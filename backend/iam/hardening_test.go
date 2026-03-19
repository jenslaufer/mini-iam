package iam

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// --- Generic error messages ---
// Verify that internal error details are never leaked to clients.

func TestTokenAuthCodeInvalidCode_NoInternalError(t *testing.T) {
	env := newHandlerEnv(t)

	resp, _ := http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {"totally-invalid-code"},
		"redirect_uri": {"http://localhost/cb"},
		"client_id":    {"some-client"},
	})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected error response for invalid auth code, got 200")
	}

	m := readJSON(t, resp)
	errMsg, _ := m["error_description"].(string)

	// Must return a user-facing message, not a raw database or Go error.
	for _, leak := range []string{"sql:", "no rows", "sqlite", "database", "scan", "query"} {
		if strings.Contains(strings.ToLower(errMsg), leak) {
			t.Errorf("error_description leaks internal detail %q: %q", leak, errMsg)
		}
	}

	// Must communicate the problem clearly without internals.
	if errMsg == "" {
		t.Error("error_description should not be empty")
	}
}

func TestTokenRefreshTokenInvalid_NoInternalError(t *testing.T) {
	env := newHandlerEnv(t)

	resp, _ := http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"not-a-real-token"},
	})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected error response for invalid refresh token, got 200")
	}

	m := readJSON(t, resp)
	errMsg, _ := m["error_description"].(string)

	for _, leak := range []string{"sql:", "no rows", "sqlite", "database", "scan", "query"} {
		if strings.Contains(strings.ToLower(errMsg), leak) {
			t.Errorf("error_description leaks internal detail %q: %q", leak, errMsg)
		}
	}

	if errMsg == "" {
		t.Error("error_description should not be empty")
	}
}

func TestActivateInvalidToken_GenericError(t *testing.T) {
	env := newHandlerEnv(t)

	// POST /activate/{token} in JSON mode with a token that does not exist.
	resp := doReq(t, env, "POST", "/activate/no-such-invite-token", "", `{"password":"longpassword"}`)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected error response for invalid invite token, got 200")
	}

	m := readJSON(t, resp)
	errMsg, _ := m["error_description"].(string)

	for _, leak := range []string{"sql:", "no rows", "sqlite", "database", "scan", "query"} {
		if strings.Contains(strings.ToLower(errMsg), leak) {
			t.Errorf("error_description leaks internal detail %q: %q", leak, errMsg)
		}
	}

	// Must contain a user-facing message about an invalid or expired invite.
	lower := strings.ToLower(errMsg)
	if !strings.Contains(lower, "invalid") && !strings.Contains(lower, "expired") && !strings.Contains(lower, "invite") {
		t.Errorf("error_description should mention invalid/expired invite, got: %q", errMsg)
	}
}

// --- UUID path validation ---
// Non-UUID path segments must be rejected with 400 before touching the database.

func TestAdminGetUser_NonUUIDPath_Rejected(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	cases := []struct {
		name string
		path string
	}{
		{"bare word", "/admin/users/not-a-uuid"},
		{"short hex", "/admin/users/abc"},
		{"numeric only", "/admin/users/12345"},
		{"sql injection", "/admin/users/' OR 1=1--"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doReq(t, env, "GET", tc.path, tok, "")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("path %q: status = %d, want 400", tc.path, resp.StatusCode)
			}
		})
	}
}

func TestAdminGetUser_ValidUUIDPath_NotRejectedAs400(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// A well-formed UUID that does not exist in the DB should yield 404, not 400.
	resp := doReq(t, env, "GET", "/admin/users/550e8400-e29b-41d4-a716-446655440000", tok, "")
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		t.Error("valid UUID format should not be rejected with 400")
	}
	// Expect 404 because the user does not exist.
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown but valid UUID", resp.StatusCode)
	}
}

func TestAdminGetClient_NonUUIDPath_Rejected(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	cases := []struct {
		name string
		path string
	}{
		{"bare word", "/admin/clients/not-a-uuid"},
		{"short string", "/admin/clients/abc"},
		{"sql injection", "/admin/clients/' OR 1=1--"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doReq(t, env, "GET", tc.path, tok, "")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("path %q: status = %d, want 400", tc.path, resp.StatusCode)
			}
		})
	}
}

func TestAdminGetClient_ValidUUIDPath_NotRejectedAs400(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "GET", "/admin/clients/550e8400-e29b-41d4-a716-446655440000", tok, "")
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		t.Error("valid UUID format should not be rejected with 400")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown but valid UUID", resp.StatusCode)
	}
}

// --- Request body size limit ---
// Oversized request bodies must be rejected to prevent resource exhaustion.

func TestRegister_OversizedBody_Rejected(t *testing.T) {
	env := newHandlerEnv(t)

	// Build a payload well above 1 MB.
	large := `{"email":"x@y.z","password":"` + strings.Repeat("a", 2*1024*1024) + `","name":"Big"}`
	resp := postJSON(t, env, "/register", large)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		t.Fatalf("oversized register body should be rejected, got %d", resp.StatusCode)
	}
	// Accept 413 (Request Entity Too Large) or 400 (Bad Request).
	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized register: status = %d, want 413 or 400", resp.StatusCode)
	}
}

func TestLogin_OversizedBody_Rejected(t *testing.T) {
	env := newHandlerEnv(t)

	large := `{"email":"x@y.z","password":"` + strings.Repeat("a", 2*1024*1024) + `"}`
	resp := postJSON(t, env, "/login", large)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("oversized login body should be rejected, got 200")
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized login: status = %d, want 413 or 400", resp.StatusCode)
	}
}
