package iam

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
	resp := postJSON(t, env, "/register", `{"email":"new@example.com","password":"secret","name":"New"}`)
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
	postJSON(t, env, "/register", `{"email":"dup@example.com","password":"secret","name":"Dup"}`).Body.Close()
	resp := postJSON(t, env, "/register", `{"email":"dup@example.com","password":"secret","name":"Dup2"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestRegisterInvalidEmail(t *testing.T) {
	env := newHandlerEnv(t)
	resp := postJSON(t, env, "/register", `{"email":"bad","password":"secret","name":"Bad"}`)
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
	resp := postJSON(t, env, "/register", `{"email":"nn@example.com","password":"secret"}`)
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
	postJSON(t, env, "/register", `{"email":"login@example.com","password":"secret","name":"Login"}`).Body.Close()

	resp := postJSON(t, env, "/login", `{"email":"login@example.com","password":"secret"}`)
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
	postJSON(t, env, "/register", `{"email":"lw@example.com","password":"secret","name":"LW"}`).Body.Close()
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
	postJSON(t, env, "/register", `{"email":"ui@example.com","password":"secret","name":"UI"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"ui@example.com","password":"secret"}`)
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
	postJSON(t, env, "/register", `{"email":"rev@example.com","password":"secret","name":"Rev"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"rev@example.com","password":"secret"}`)
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
	resp := postJSON(t, env, "/clients",
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
	resp := postJSON(t, env, "/clients", `{"redirect_uris":["http://localhost/cb"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestCreateClientNoRedirectURIs(t *testing.T) {
	env := newHandlerEnv(t)
	resp := postJSON(t, env, "/clients", `{"name":"NoURIs"}`)
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
	postJSON(t, env, "/register", `{"email":"tok@example.com","password":"secret","name":"Tok"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"tok@example.com","password":"secret"}`)
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

	// Create client and user
	resp := postJSON(t, env, "/clients", `{"name":"AuthApp","redirect_uris":["http://localhost/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	postJSON(t, env, "/register", `{"email":"auth@example.com","password":"secret","name":"Auth"}`).Body.Close()

	// POST authorize (don't follow redirects)
	httpClient := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{
		"client_id":    {clientID},
		"redirect_uri": {"http://localhost/cb"},
		"email":        {"auth@example.com"},
		"password":     {"secret"},
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

	resp := postJSON(t, env, "/clients", `{"name":"App","redirect_uris":["http://localhost/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	postJSON(t, env, "/register", `{"email":"wp@example.com","password":"secret","name":"WP"}`).Body.Close()

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

	// Create client
	resp := postJSON(t, env, "/clients", `{"name":"PKCE","redirect_uris":["http://localhost/cb"]}`)
	client := readJSON(t, resp)
	clientID := client["client_id"].(string)

	// Register user
	postJSON(t, env, "/register", `{"email":"pkce@example.com","password":"secret","name":"PKCE"}`).Body.Close()

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
		"password":             {"secret"},
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
	tok := readJSON(t, resp)
	if tok["access_token"] == nil {
		t.Error("no access_token")
	}
	if tok["id_token"] == nil {
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

	postJSON(t, env, "/register", `{"email":"regular@example.com","password":"secret","name":"Regular"}`).Body.Close()
	resp := postJSON(t, env, "/login", `{"email":"regular@example.com","password":"secret"}`)
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
	resp := postJSON(t, env, "/register", `{"email":"get@example.com","password":"secret","name":"Get"}`)
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

	resp := postJSON(t, env, "/register", `{"email":"upd@example.com","password":"secret","name":"Old"}`)
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

	resp := postJSON(t, env, "/register", `{"email":"role@example.com","password":"secret","name":"Role"}`)
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

	resp := postJSON(t, env, "/register", `{"email":"del@example.com","password":"secret","name":"Del"}`)
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

	resp := postJSON(t, env, "/clients", `{"name":"DelApp","redirect_uris":["http://del/cb"]}`)
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
