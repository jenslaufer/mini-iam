package iam

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

type testEnv struct {
	srv    *httptest.Server
	store  *Store
	tokens *TokenService
}

func newHandlerEnv(t *testing.T) *testEnv {
	t.Helper()
	db := newTestDB(t)

	// Add contacts table for activate tests
	db.Exec(`CREATE TABLE IF NOT EXISTS contacts (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
		user_id TEXT REFERENCES users(id), unsubscribed INTEGER NOT NULL DEFAULT 0,
		unsubscribe_token TEXT UNIQUE NOT NULL, invite_token TEXT UNIQUE,
		consent_source TEXT NOT NULL, consent_at DATETIME NOT NULL, created_at DATETIME NOT NULL,
		UNIQUE(tenant_id, email)
	)`)

	store := NewStore(db)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tokens := NewTokenService(key, "http://test-issuer")
	registry := NewStaticTokenRegistry(tokens)
	h := NewHandler(store, registry, "http://test-issuer")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/register", h.Register)
	mux.HandleFunc("/login", h.Login)
	mux.HandleFunc("/authorize", h.Authorize)
	mux.HandleFunc("/token", h.Token)
	mux.HandleFunc("/userinfo", h.UserInfo)
	mux.HandleFunc("/jwks", h.JWKS)
	mux.HandleFunc("/.well-known/openid-configuration", h.Discovery)
	mux.HandleFunc("/revoke", h.Revoke)
	mux.HandleFunc("/clients", h.CreateClient)
	mux.HandleFunc("/admin/users", h.AdminListUsers)
	mux.HandleFunc("/admin/users/", h.AdminUserByID)
	mux.HandleFunc("/admin/clients", h.AdminListClients)
	mux.HandleFunc("/admin/clients/", h.AdminDeleteClient)
	mux.HandleFunc("/activate/", h.Activate)
	mux.HandleFunc("/password", h.ChangePassword)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })

	return &testEnv{srv: srv, store: store, tokens: tokens}
}

func adminToken(t *testing.T, env *testEnv) string {
	t.Helper()
	env.store.SeedAdmin("admin@test.com", "adminpass", "Admin")
	resp := postJSON(t, env, "/login", `{"email":"admin@test.com","password":"adminpass"}`)
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	return tok.AccessToken
}

func postJSON(t *testing.T, env *testEnv, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(env.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func doReq(t *testing.T, env *testEnv, method, path, token, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, env.srv.URL+path, bodyReader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return m
}

// --- Health ---

func TestHealthEndpoint(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/health", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	m := readJSON(t, resp)
	if m["status"] != "ok" {
		t.Errorf("status = %v", m["status"])
	}
}

// --- Register ---

func TestRegister(t *testing.T) {
	env := newHandlerEnv(t)
	resp := postJSON(t, env, "/register", `{"email":"new@example.com","password":"secret12","name":"New"}`)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	m := readJSON(t, resp)
	if m["email"] != "new@example.com" {
		t.Errorf("email = %v", m["email"])
	}
}

func TestRegisterDuplicate(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"dup@example.com","password":"secret12","name":"Dup"}`).Body.Close()
	resp := postJSON(t, env, "/register", `{"email":"dup@example.com","password":"secret12","name":"Dup2"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestRegisterInvalidEmail(t *testing.T) {
	env := newHandlerEnv(t)
	resp := postJSON(t, env, "/register", `{"email":"bad","password":"secret12","name":"Bad"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestRegisterNoPassword(t *testing.T) {
	env := newHandlerEnv(t)
	resp := postJSON(t, env, "/register", `{"email":"np@example.com","name":"NP"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestRegisterNoName(t *testing.T) {
	env := newHandlerEnv(t)
	resp := postJSON(t, env, "/register", `{"email":"nn@example.com","password":"secret12"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestRegisterMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/register", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- Login ---

func TestLogin(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"login@example.com","password":"secret12","name":"Login"}`).Body.Close()

	resp := postJSON(t, env, "/login", `{"email":"login@example.com","password":"secret12"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	m := readJSON(t, resp)
	if m["access_token"] == nil || m["access_token"] == "" {
		t.Error("no access_token")
	}
	if m["refresh_token"] == nil || m["refresh_token"] == "" {
		t.Error("no refresh_token")
	}
	if m["id_token"] == nil || m["id_token"] == "" {
		t.Error("no id_token")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"lw@example.com","password":"secret12","name":"LW"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"lw@example.com","password":"wrong"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestLoginMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/login", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- UserInfo ---

func TestUserInfo(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"ui@example.com","password":"secret12","name":"UI"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"ui@example.com","password":"secret12"}`)
	tok := readJSON(t, resp)["access_token"].(string)

	resp = doReq(t, env, "GET", "/userinfo", tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	m := readJSON(t, resp)
	if m["email"] != "ui@example.com" {
		t.Errorf("email = %v", m["email"])
	}
}

func TestUserInfoNoToken(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/userinfo", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestUserInfoInvalidToken(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/userinfo", "bad-token", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- JWKS ---

func TestJWKSEndpoint(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/jwks", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	m := readJSON(t, resp)
	if m["keys"] == nil {
		t.Error("no keys in JWKS")
	}
}

// --- Discovery ---

func TestDiscovery(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/.well-known/openid-configuration", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	m := readJSON(t, resp)
	if m["issuer"] != "http://test-issuer" {
		t.Errorf("issuer = %v", m["issuer"])
	}
	if m["authorization_endpoint"] == nil {
		t.Error("missing authorization_endpoint")
	}
}

// --- Revoke ---

func TestRevoke(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"rev@example.com","password":"secret12","name":"Rev"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"rev@example.com","password":"secret12"}`)
	tok := readJSON(t, resp)
	rt := tok["refresh_token"].(string)

	// Revoke
	resp, _ = http.PostForm(env.srv.URL+"/revoke", url.Values{"token": {rt}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Revoke nonexistent — should still return 200 per RFC 7009
	resp, _ = http.PostForm(env.srv.URL+"/revoke", url.Values{"token": {"nonexistent"}})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestRevokeNoToken(t *testing.T) {
	env := newHandlerEnv(t)
	resp, _ := http.PostForm(env.srv.URL+"/revoke", url.Values{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestRevokeMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/revoke", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- CreateClient ---

func TestCreateClientEndpoint(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)
	resp := doReq(t, env, "POST", "/clients", tok,
		`{"name":"MyApp","redirect_uris":["http://localhost/cb"]}`)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	m := readJSON(t, resp)
	if m["client_id"] == nil || m["client_id"] == "" {
		t.Error("no client_id")
	}
	if m["client_secret"] == nil || m["client_secret"] == "" {
		t.Error("no client_secret")
	}
}

func TestCreateClientNoName(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)
	resp := doReq(t, env, "POST", "/clients", tok, `{"redirect_uris":["http://localhost/cb"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestCreateClientNoRedirectURIs(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)
	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"NoURIs"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestCreateClientMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/clients", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- Token Endpoint ---

func TestTokenRefreshTokenGrant(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"tok@example.com","password":"secret12","name":"Tok"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"tok@example.com","password":"secret12"}`)
	tok := readJSON(t, resp)
	rt := tok["refresh_token"].(string)

	resp, _ = http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {rt},
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	m := readJSON(t, resp)
	if m["access_token"] == nil {
		t.Error("no access_token")
	}
	if m["refresh_token"] == nil {
		t.Error("no new refresh_token (rotation)")
	}
}

func TestTokenInvalidRefreshToken(t *testing.T) {
	env := newHandlerEnv(t)
	resp, _ := http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"invalid"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestTokenNoRefreshToken(t *testing.T) {
	env := newHandlerEnv(t)
	resp, _ := http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type": {"refresh_token"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestTokenUnsupportedGrant(t *testing.T) {
	env := newHandlerEnv(t)
	resp, _ := http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type": {"client_credentials"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestTokenMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/token", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- Authorize ---

func TestAuthorizeGET(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/authorize?client_id=test&redirect_uri=http://localhost/cb", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Sign In") {
		t.Error("login form not rendered")
	}
}

func TestAuthorizePOST(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create client and user
	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"AuthApp","redirect_uris":["http://localhost/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	postJSON(t, env, "/register", `{"email":"auth@example.com","password":"secret12","name":"Auth"}`).Body.Close()

	// POST authorize (don't follow redirects)
	httpClient := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{
		"client_id":    {clientID},
		"redirect_uri": {"http://localhost/cb"},
		"email":        {"auth@example.com"},
		"password":     {"secret12"},
		"state":        {"mystate"},
		"scope":        {"openid"},
	}
	resp, err := httpClient.PostForm(env.srv.URL+"/authorize", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "code=") {
		t.Errorf("redirect missing code: %s", loc)
	}
	if !strings.Contains(loc, "state=mystate") {
		t.Errorf("redirect missing state: %s", loc)
	}
}

func TestAuthorizePOSTWrongPassword(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"App","redirect_uris":["http://localhost/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	postJSON(t, env, "/register", `{"email":"wp@example.com","password":"secret12","name":"WP"}`).Body.Close()

	form := url.Values{
		"client_id":    {clientID},
		"redirect_uri": {"http://localhost/cb"},
		"email":        {"wp@example.com"},
		"password":     {"wrong"},
	}
	resp, _ = http.PostForm(env.srv.URL+"/authorize", form)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAuthorizeMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "DELETE", "/authorize", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- Full Auth Code Flow with PKCE ---

func TestFullAuthCodeFlowWithPKCE(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create client
	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"PKCE","redirect_uris":["http://localhost/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	// Register user
	postJSON(t, env, "/register", `{"email":"pkce@example.com","password":"secret12","name":"PKCE"}`).Body.Close()

	// Generate PKCE
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Authorize
	httpClient := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{
		"client_id":             {clientID},
		"redirect_uri":         {"http://localhost/cb"},
		"email":                {"pkce@example.com"},
		"password":             {"secret12"},
		"scope":                {"openid"},
		"code_challenge":       {challenge},
		"code_challenge_method": {"S256"},
	}
	resp, _ = httpClient.PostForm(env.srv.URL+"/authorize", form)
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp.Body.Close()
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("no code in redirect")
	}

	// Exchange code for tokens
	resp, _ = http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost/cb"},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("token exchange: status = %d, body = %s", resp.StatusCode, b)
	}
	tokenResp := readJSON(t, resp)
	if tokenResp["access_token"] == nil {
		t.Error("no access_token")
	}
	if tokenResp["id_token"] == nil {
		t.Error("no id_token")
	}
}

// --- Admin User Endpoints ---

func TestAdminListUsers(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "GET", "/admin/users", tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var users []any
	json.NewDecoder(resp.Body).Decode(&users)
	if len(users) < 1 {
		t.Error("expected at least 1 user (admin)")
	}
}

func TestAdminListUsersUnauthorized(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/admin/users", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestAdminListUsersForbiddenForNonAdmin(t *testing.T) {
	env := newHandlerEnv(t)

	postJSON(t, env, "/register", `{"email":"regular@example.com","password":"secret12","name":"Regular"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"regular@example.com","password":"secret12"}`)
	tok := readJSON(t, resp)["access_token"].(string)

	resp = doReq(t, env, "GET", "/admin/users", tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestAdminGetUserByID(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Register a user
	resp := postJSON(t, env, "/register", `{"email":"get@example.com","password":"secret12","name":"Get"}`)
	created := readJSON(t, resp)
	id := created["id"].(string)

	resp = doReq(t, env, "GET", "/admin/users/"+id, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got := readJSON(t, resp)
	if got["email"] != "get@example.com" {
		t.Errorf("email = %v", got["email"])
	}
}

func TestAdminUpdateUser(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := postJSON(t, env, "/register", `{"email":"upd@example.com","password":"secret12","name":"Old"}`)
	created := readJSON(t, resp)
	id := created["id"].(string)

	resp = doReq(t, env, "PUT", "/admin/users/"+id, tok, `{"name":"New","role":"admin"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	updated := readJSON(t, resp)
	if updated["name"] != "New" {
		t.Errorf("name = %v", updated["name"])
	}
	if updated["role"] != "admin" {
		t.Errorf("role = %v", updated["role"])
	}
}

func TestAdminUpdateUserInvalidRole(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := postJSON(t, env, "/register", `{"email":"role@example.com","password":"secret12","name":"Role"}`)
	created := readJSON(t, resp)
	id := created["id"].(string)

	resp = doReq(t, env, "PUT", "/admin/users/"+id, tok, `{"role":"superadmin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminDeleteUser(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := postJSON(t, env, "/register", `{"email":"del@example.com","password":"secret12","name":"Del"}`)
	created := readJSON(t, resp)
	id := created["id"].(string)

	resp = doReq(t, env, "DELETE", "/admin/users/"+id, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doReq(t, env, "GET", "/admin/users/"+id, tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("after delete: status = %d", resp.StatusCode)
	}
}

func TestAdminDeleteSelf(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	admin, _ := env.store.GetUserByEmail("admin@test.com")
	resp := doReq(t, env, "DELETE", "/admin/users/"+admin.ID, tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (cannot delete yourself)", resp.StatusCode)
	}
}

// --- Admin Client Endpoints ---

func TestAdminListClients(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "GET", "/admin/clients", tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminDeleteClientEndpoint(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"DelApp","redirect_uris":["http://del/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	resp = doReq(t, env, "DELETE", "/admin/clients/"+clientID, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminDeleteClientNotFound(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "DELETE", "/admin/clients/nonexistent", tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- Admin Update Client ---

func TestUpdateClient(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create client
	resp := doReq(t, env, "POST", "/clients", tok,
		`{"name":"OrigApp","redirect_uris":["http://orig/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	// Update
	resp = doReq(t, env, "PUT", "/admin/clients/"+clientID, tok,
		`{"name":"NewApp","redirect_uris":["http://new/cb"]}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	updated := readJSON(t, resp)
	if updated["name"] != "NewApp" {
		t.Errorf("name = %v, want NewApp", updated["name"])
	}
	uris := updated["redirect_uris"].([]any)
	if len(uris) != 1 || uris[0] != "http://new/cb" {
		t.Errorf("redirect_uris = %v", uris)
	}

	// GET to verify persistence
	resp = doReq(t, env, "GET", "/admin/clients/"+clientID, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", resp.StatusCode)
	}
	got := readJSON(t, resp)
	if got["name"] != "NewApp" {
		t.Errorf("after get: name = %v", got["name"])
	}
}

func TestUpdateClientNotFound(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "PUT", "/admin/clients/nonexistent", tok,
		`{"name":"X","redirect_uris":["http://x/cb"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUpdateClientPartial(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/clients", tok,
		`{"name":"Partial","redirect_uris":["http://a/cb","http://b/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	// Update only name
	resp = doReq(t, env, "PUT", "/admin/clients/"+clientID, tok,
		`{"name":"Renamed"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("name-only: status = %d, body = %s", resp.StatusCode, b)
	}
	updated := readJSON(t, resp)
	if updated["name"] != "Renamed" {
		t.Errorf("name = %v", updated["name"])
	}
	uris := updated["redirect_uris"].([]any)
	if len(uris) != 2 {
		t.Errorf("redirect_uris should be preserved, got %v", uris)
	}

	// Update only redirect_uris
	resp = doReq(t, env, "PUT", "/admin/clients/"+clientID, tok,
		`{"redirect_uris":["http://only/cb"]}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("uris-only: status = %d, body = %s", resp.StatusCode, b)
	}
	updated = readJSON(t, resp)
	if updated["name"] != "Renamed" {
		t.Errorf("name should be preserved, got %v", updated["name"])
	}
	uris = updated["redirect_uris"].([]any)
	if len(uris) != 1 || uris[0] != "http://only/cb" {
		t.Errorf("redirect_uris = %v", uris)
	}
}

func TestUpdateClientSecretUnchanged(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/clients", tok,
		`{"name":"SecretApp","redirect_uris":["http://s/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)
	originalSecret := client["client_secret"].(string)

	// Update client
	resp = doReq(t, env, "PUT", "/admin/clients/"+clientID, tok,
		`{"name":"Updated"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	updated := readJSON(t, resp)
	// Response should not contain secret
	if updated["client_secret"] != nil {
		t.Error("update response should not expose client_secret")
	}

	// Original secret should still work
	c, err := env.store.GetClient(clientID)
	if err != nil {
		t.Fatal(err)
	}
	if !env.store.ValidateClientSecret(c, originalSecret) {
		t.Error("original secret no longer valid after update")
	}
}

func TestUpdateClientRequiresAdmin(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/clients", tok,
		`{"name":"AdminOnly","redirect_uris":["http://a/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	// Without token
	resp = doReq(t, env, "PUT", "/admin/clients/"+clientID, "",
		`{"name":"Hacked"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}

	// Non-admin token
	postJSON(t, env, "/register", `{"email":"regular@example.com","password":"longpassword","name":"Reg"}`).Body.Close()
	loginResp := postJSON(t, env, "/login", `{"email":"regular@example.com","password":"longpassword"}`)
	userTok := readJSON(t, loginResp)["access_token"].(string)

	resp = doReq(t, env, "PUT", "/admin/clients/"+clientID, userTok,
		`{"name":"Hacked"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin: status = %d, want 403", resp.StatusCode)
	}
}

// --- Activate Endpoint ---

func TestActivateGET(t *testing.T) {
	env := newHandlerEnv(t)
	db := env.store.DB()
	db.Exec(`INSERT INTO contacts (id, email, name, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		VALUES ('c1', 'act@example.com', 'Act', 'unsub1', 'act-token', 'api', datetime('now'), datetime('now'))`)

	resp := doReq(t, env, "GET", "/activate/act-token", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "act@example.com") {
		t.Error("activation form should show email")
	}
}

func TestActivateGETInvalidToken(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/activate/bad-token", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestActivatePOSTJSON(t *testing.T) {
	env := newHandlerEnv(t)
	db := env.store.DB()
	db.Exec(`INSERT INTO contacts (id, email, name, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		VALUES ('c2', 'actpost@example.com', 'ActPost', 'unsub2', 'act-post-token', 'api', datetime('now'), datetime('now'))`)

	resp := doReq(t, env, "POST", "/activate/act-post-token", "", `{"password":"longpassword"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	m := readJSON(t, resp)
	if m["status"] != "activated" {
		t.Errorf("status = %v", m["status"])
	}
}

func TestActivatePOSTShortPassword(t *testing.T) {
	env := newHandlerEnv(t)
	db := env.store.DB()
	db.Exec(`INSERT INTO contacts (id, email, name, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		VALUES ('c3', 'short@example.com', 'Short', 'unsub3', 'act-short-token', 'api', datetime('now'), datetime('now'))`)

	resp := doReq(t, env, "POST", "/activate/act-short-token", "", `{"password":"short"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- C-2: Password minimum on /register ---

func TestRegisterShortPassword(t *testing.T) {
	env := newHandlerEnv(t)
	resp := postJSON(t, env, "/register", `{"email":"short@example.com","password":"abc","name":"Short"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("short password: status = %d, want 400", resp.StatusCode)
	}
}

// --- M-4: Cache-Control on token responses ---

func TestLoginResponseCacheHeaders(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"cache@example.com","password":"longpassword","name":"Cache"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"cache@example.com","password":"longpassword"}`)
	defer resp.Body.Close()
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", resp.Header.Get("Cache-Control"))
	}
	if resp.Header.Get("Pragma") != "no-cache" {
		t.Errorf("Pragma = %q, want no-cache", resp.Header.Get("Pragma"))
	}
}

func TestTokenResponseCacheHeaders(t *testing.T) {
	env := newHandlerEnv(t)
	postJSON(t, env, "/register", `{"email":"tcache@example.com","password":"longpassword","name":"TC"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"tcache@example.com","password":"longpassword"}`)
	tok := readJSON(t, resp)
	rt := tok["refresh_token"].(string)

	resp, _ = http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {rt},
	})
	defer resp.Body.Close()
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", resp.Header.Get("Cache-Control"))
	}
}

// --- M-7: Email length validation ---

func TestRegisterEmailTooLong(t *testing.T) {
	env := newHandlerEnv(t)
	long := strings.Repeat("a", 250) + "@b.co" // 255 chars > 254 limit
	body := fmt.Sprintf(`{"email":"%s","password":"longpassword","name":"Long"}`, long)
	resp := postJSON(t, env, "/register", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("long email: status = %d, want 400", resp.StatusCode)
	}
}

// --- H-1: PKCE enforcement (no PKCE, no client_secret = must be rejected) ---

func TestTokenAuthCodeNoPKCENoSecret(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create client
	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"NoPKCE","redirect_uris":["http://localhost/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	// Register user
	postJSON(t, env, "/register", `{"email":"nopkce@example.com","password":"longpassword","name":"NP"}`).Body.Close()

	// Authorize WITHOUT code_challenge
	httpClient := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{
		"client_id":    {clientID},
		"redirect_uri": {"http://localhost/cb"},
		"email":        {"nopkce@example.com"},
		"password":     {"longpassword"},
		"scope":        {"openid"},
	}
	resp, _ = httpClient.PostForm(env.srv.URL+"/authorize", form)
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp.Body.Close()
	code := loc.Query().Get("code")

	// Exchange WITHOUT code_verifier AND WITHOUT client_secret
	resp, _ = http.PostForm(env.srv.URL+"/token", url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {"http://localhost/cb"},
		"client_id":    {clientID},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("no PKCE, no secret: status = %d, want 400", resp.StatusCode)
	}
}

// --- Registration policy ---

type denyRegistration struct{}

func (denyRegistration) IsRegistrationEnabled(string) bool { return false }

func TestRegisterDisabledByPolicy(t *testing.T) {
	env := newHandlerEnv(t)
	// Patch handler to deny registration
	// We need to reach the handler — recreate with policy
	db := env.store.DB()
	store := NewStore(db)
	key := env.tokens
	registry := NewStaticTokenRegistry(key)
	h := NewHandler(store, registry, "http://test-issuer")
	h.Registration = denyRegistration{}

	mux := http.NewServeMux()
	mux.HandleFunc("/register", h.Register)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/register", "application/json",
		strings.NewReader(`{"email":"blocked@example.com","password":"longpassword","name":"Blocked"}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("registration disabled: status = %d, want 403", resp.StatusCode)
	}
}

// --- ChangePassword ---

func userToken(t *testing.T, env *testEnv, email, password string) string {
	t.Helper()
	postJSON(t, env, "/register", fmt.Sprintf(`{"email":"%s","password":"%s","name":"User"}`, email, password)).Body.Close()
	resp := postJSON(t, env, "/login", fmt.Sprintf(`{"email":"%s","password":"%s"}`, email, password))
	tok := readJSON(t, resp)["access_token"].(string)
	return tok
}

func TestChangePasswordSuccess(t *testing.T) {
	env := newHandlerEnv(t)
	tok := userToken(t, env, "chpw@example.com", "oldpass12")

	resp := doReq(t, env, "POST", "/password", tok,
		`{"current_password":"oldpass12","new_password":"newpass12"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, b)
	}
	m := readJSON(t, resp)
	if m["status"] != "password_changed" {
		t.Errorf("status = %v", m["status"])
	}

	// Old password should fail login
	resp = postJSON(t, env, "/login", `{"email":"chpw@example.com","password":"oldpass12"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old password login: status = %d, want 401", resp.StatusCode)
	}

	// New password should work
	resp = postJSON(t, env, "/login", `{"email":"chpw@example.com","password":"newpass12"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("new password login: status = %d, want 200", resp.StatusCode)
	}
}

func TestChangePasswordWrongCurrent(t *testing.T) {
	env := newHandlerEnv(t)
	tok := userToken(t, env, "wrong@example.com", "correct1")

	resp := doReq(t, env, "POST", "/password", tok,
		`{"current_password":"incorrect","new_password":"newpass12"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong current password: status = %d, want 401", resp.StatusCode)
	}
}

func TestChangePasswordTooShort(t *testing.T) {
	env := newHandlerEnv(t)
	tok := userToken(t, env, "short@example.com", "oldpass12")

	resp := doReq(t, env, "POST", "/password", tok,
		`{"current_password":"oldpass12","new_password":"short"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("too short: status = %d, want 400", resp.StatusCode)
	}
}

func TestChangePasswordTooLong(t *testing.T) {
	env := newHandlerEnv(t)
	tok := userToken(t, env, "toolong@example.com", "oldpass12")
	tooLong := strings.Repeat("a", 73)

	resp := doReq(t, env, "POST", "/password", tok,
		fmt.Sprintf(`{"current_password":"oldpass12","new_password":"%s"}`, tooLong))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("too long: status = %d, want 400", resp.StatusCode)
	}
}

func TestChangePasswordNoAuth(t *testing.T) {
	env := newHandlerEnv(t)

	resp := doReq(t, env, "POST", "/password", "",
		`{"current_password":"x","new_password":"newpass12"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want 401", resp.StatusCode)
	}
}

func TestChangePasswordMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)

	resp := doReq(t, env, "GET", "/password", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: status = %d, want 405", resp.StatusCode)
	}
}
