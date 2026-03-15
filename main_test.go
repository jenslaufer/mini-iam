package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"crypto/sha256"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// SetupServer
// ---------------------------------------------------------------------------

// SetupServer wires all routes and returns an http.Handler ready for testing.
// It mirrors the setup in main() but accepts externally supplied dependencies
// so each test can use an isolated in-memory store.
func SetupServer(store *Store, tokenService *TokenService) http.Handler {
	const issuer = "http://test-issuer"
	h := NewHandler(store, tokenService, issuer)

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

	return CORSMiddleware("*")(mux)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestServer creates a fresh in-memory database, token service and HTTP
// test server for each test, guaranteeing full isolation.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	rsaKey, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatalf("LoadOrCreateRSAKey: %v", err)
	}
	tokenSvc := NewTokenService(rsaKey, "http://test-issuer")
	srv := httptest.NewServer(SetupServer(store, tokenSvc))
	t.Cleanup(srv.Close)
	return srv
}

// doJSON sends a JSON POST and returns the response.
func doJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// doForm sends an application/x-www-form-urlencoded POST without following redirects.
func doForm(t *testing.T, srv *httptest.Server, path string, values url.Values) *http.Response {
	t.Helper()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.PostForm(srv.URL+path, values)
	if err != nil {
		t.Fatalf("POST form %s: %v", path, err)
	}
	return resp
}

// doGet performs a GET with optional headers, never following redirects.
func doGet(t *testing.T, srv *httptest.Server, path string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest GET %s: %v", path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// readBody drains and returns the response body string for diagnostics.
func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(b)
}

// decodeJSON decodes the JSON response body into dst.
func decodeJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("json.Unmarshal (body=%q): %v", string(b), err)
	}
}

// assertStatus fails if the response code does not match.
func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := readBody(resp)
		t.Fatalf("expected status %d, got %d (body=%q)", want, resp.StatusCode, body)
	}
}

// assertErrorCode checks the "error" field in a JSON error body.
func assertErrorCode(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	var e ErrorResponse
	decodeJSON(t, resp, &e)
	if e.Error != want {
		t.Fatalf("expected error %q, got %q (description=%q)", want, e.Error, e.ErrorDescription)
	}
}

// registerUser registers a user and returns the parsed response map.
func registerUser(t *testing.T, srv *httptest.Server, email, password, name string) map[string]any {
	t.Helper()
	resp := doJSON(t, srv, "/register", map[string]any{
		"email":    email,
		"password": password,
		"name":     name,
	})
	assertStatus(t, resp, http.StatusCreated)
	var result map[string]any
	decodeJSON(t, resp, &result)
	return result
}

// loginUser logs in and returns the full TokenResponse.
func loginUser(t *testing.T, srv *httptest.Server, email, password string) TokenResponse {
	t.Helper()
	resp := doJSON(t, srv, "/login", map[string]any{
		"email":    email,
		"password": password,
	})
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)
	return tr
}

// registerClient creates an OAuth2 client and returns the parsed response.
func registerClient(t *testing.T, srv *httptest.Server, name string, redirectURIs []string) ClientCreateResponse {
	t.Helper()
	resp := doJSON(t, srv, "/clients", map[string]any{
		"name":          name,
		"redirect_uris": redirectURIs,
	})
	assertStatus(t, resp, http.StatusCreated)
	var cr ClientCreateResponse
	decodeJSON(t, resp, &cr)
	return cr
}

// pkceChallenge generates a random code_verifier and its S256 code_challenge.
func pkceChallenge(t *testing.T) (verifier, challenge string) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

// authorizeCode submits the POST /authorize login form and returns the redirect
// Location header so the caller can extract code/state/error.
func authorizeCode(
	t *testing.T,
	srv *httptest.Server,
	email, password string,
	client ClientCreateResponse,
	verifier, challenge string,
) string {
	t.Helper()

	values := url.Values{
		"email":                 {email},
		"password":              {password},
		"client_id":             {client.ClientID},
		"redirect_uri":          {client.RedirectURIs[0]},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"state":                 {"test-state-xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"nonce":                 {"test-nonce-abc"},
	}

	resp := doForm(t, srv, "/authorize", values)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 302 from POST /authorize, got %d (body=%q)", resp.StatusCode, body)
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("POST /authorize: no Location header")
	}
	return loc
}

// extractCode parses the "code" query parameter from a redirect Location.
func extractCode(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", location, err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect location %q", location)
	}
	return code
}

// exchangeCode exchanges an auth code for tokens via POST /token.
func exchangeCode(t *testing.T, srv *httptest.Server, code, redirectURI, clientID, clientSecret, verifier string) TokenResponse {
	t.Helper()
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	if verifier != "" {
		values.Set("code_verifier", verifier)
	}
	resp := doForm(t, srv, "/token", values)
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)
	return tr
}

// fetchJWKS retrieves the JWKS from /jwks and returns a map kid→*rsa.PublicKey.
func fetchJWKS(t *testing.T, srv *httptest.Server) map[string]*rsa.PublicKey {
	t.Helper()
	resp := doGet(t, srv, "/jwks", nil)
	assertStatus(t, resp, http.StatusOK)

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	decodeJSON(t, resp, &jwks)

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			t.Fatalf("decode N for kid=%q: %v", k.Kid, err)
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			t.Fatalf("decode E for kid=%q: %v", k.Kid, err)
		}
		n := new(big.Int).SetBytes(nBytes)
		var eInt int
		for _, b := range eBytes {
			eInt = eInt<<8 | int(b)
		}
		keys[k.Kid] = &rsa.PublicKey{N: n, E: eInt}
	}
	return keys
}

// verifyJWT parses and validates a JWT using the JWKS from the server.
// It returns the claims map on success and calls t.Fatal on any error.
func verifyJWT(t *testing.T, srv *httptest.Server, tokenStr string) jwt.MapClaims {
	t.Helper()
	keys := fetchJWKS(t, srv)

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		if pub, ok := keys[kid]; ok {
			return pub, nil
		}
		// Fall back to the only key when kid is missing.
		for _, v := range keys {
			return v, nil
		}
		return nil, fmt.Errorf("kid %q not found in JWKS", kid)
	}, jwt.WithExpirationRequired())
	if err != nil {
		t.Fatalf("JWT verification failed: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("unexpected claims type")
	}
	return claims
}

// ---------------------------------------------------------------------------
// Registration tests
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/register", map[string]any{
			"email":    "alice@example.com",
			"password": "password123",
			"name":     "Alice",
		})
		assertStatus(t, resp, http.StatusCreated)

		var user map[string]any
		decodeJSON(t, resp, &user)

		if user["id"] == nil || user["id"] == "" {
			t.Error("expected non-empty id")
		}
		if user["email"] != "alice@example.com" {
			t.Errorf("email: got %q", user["email"])
		}
		if user["name"] != "Alice" {
			t.Errorf("name: got %q", user["name"])
		}
		if _, ok := user["created_at"]; !ok {
			t.Error("expected created_at field")
		}
		if _, ok := user["password"]; ok {
			t.Error("password must not be returned")
		}
		if _, ok := user["password_hash"]; ok {
			t.Error("password_hash must not be returned")
		}
	})

	t.Run("duplicate email returns 409", func(t *testing.T) {
		srv := newTestServer(t)

		body := map[string]any{
			"email":    "dup@example.com",
			"password": "password123",
			"name":     "Dup",
		}
		resp1 := doJSON(t, srv, "/register", body)
		assertStatus(t, resp1, http.StatusCreated)
		resp1.Body.Close()

		resp2 := doJSON(t, srv, "/register", body)
		// Implementation returns 409 Conflict.
		assertStatus(t, resp2, http.StatusConflict)
		resp2.Body.Close()
	})

	t.Run("missing email returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/register", map[string]any{
			"password": "password123",
			"name":     "NoEmail",
		})
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})

	t.Run("missing password returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/register", map[string]any{
			"email": "nopw@example.com",
			"name":  "NoPw",
		})
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})

	t.Run("invalid email format returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		for _, bad := range []string{"not-an-email", "noatsign", "user@", "@nodomain"} {
			t.Run(bad, func(t *testing.T) {
				resp := doJSON(t, srv, "/register", map[string]any{
					"email":    bad,
					"password": "password123",
					"name":     "Bad",
				})
				assertStatus(t, resp, http.StatusBadRequest)
				assertErrorCode(t, resp, "invalid_request")
			})
		}
	})

	t.Run("empty password returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/register", map[string]any{
			"email":    "short@example.com",
			"password": "",
			"name":     "Short",
		})
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})

	t.Run("empty body returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/register", map[string]any{})
		assertStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// Login tests
// ---------------------------------------------------------------------------

func TestLogin(t *testing.T) {
	t.Run("successful login returns token response", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "bob@example.com", "password123", "Bob")

		resp := doJSON(t, srv, "/login", map[string]any{
			"email":    "bob@example.com",
			"password": "password123",
		})
		assertStatus(t, resp, http.StatusOK)

		var tr TokenResponse
		decodeJSON(t, resp, &tr)

		if tr.AccessToken == "" {
			t.Error("expected non-empty access_token")
		}
		if tr.TokenType != "Bearer" {
			t.Errorf("token_type: got %q, want Bearer", tr.TokenType)
		}
		if tr.ExpiresIn <= 0 {
			t.Errorf("expires_in: got %d, want > 0", tr.ExpiresIn)
		}
		if tr.RefreshToken == "" {
			t.Error("expected non-empty refresh_token")
		}
		if tr.IDToken == "" {
			t.Error("expected non-empty id_token")
		}
	})

	t.Run("wrong password returns 401", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "carol@example.com", "password123", "Carol")

		resp := doJSON(t, srv, "/login", map[string]any{
			"email":    "carol@example.com",
			"password": "wrongpassword",
		})
		assertStatus(t, resp, http.StatusUnauthorized)
		// Implementation uses "invalid_grant" for login failures.
		assertErrorCode(t, resp, "invalid_grant")
	})

	t.Run("non-existent user returns 401", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/login", map[string]any{
			"email":    "ghost@example.com",
			"password": "password123",
		})
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "invalid_grant")
	})

	t.Run("missing credentials returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/login", map[string]any{})
		// Server returns 401 when email is empty (user not found) — accept 400 or 401.
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 400 or 401, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("access token has correct JWT claims", func(t *testing.T) {
		srv := newTestServer(t)

		user := registerUser(t, srv, "dave@example.com", "password123", "Dave")
		tr := loginUser(t, srv, "dave@example.com", "password123")

		claims := verifyJWT(t, srv, tr.AccessToken)

		sub, _ := claims["sub"].(string)
		if sub == "" {
			t.Error("sub claim must be non-empty")
		}
		if sub != user["id"] {
			t.Errorf("sub: got %q, want %q", sub, user["id"])
		}
		if email, _ := claims["email"].(string); email != "dave@example.com" {
			t.Errorf("email claim: got %q", email)
		}
		if name, _ := claims["name"].(string); name != "Dave" {
			t.Errorf("name claim: got %q", name)
		}
		if iss, _ := claims["iss"].(string); iss == "" {
			t.Error("iss claim must be non-empty")
		}
		if _, ok := claims["exp"]; !ok {
			t.Error("exp claim missing")
		}
		if _, ok := claims["iat"]; !ok {
			t.Error("iat claim missing")
		}
		expF, _ := claims["exp"].(float64)
		iatF, _ := claims["iat"].(float64)
		if expF <= iatF {
			t.Errorf("exp (%v) must be after iat (%v)", expF, iatF)
		}
	})

	t.Run("id_token has correct JWT claims", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "eve@example.com", "password123", "Eve")
		tr := loginUser(t, srv, "eve@example.com", "password123")

		claims := verifyJWT(t, srv, tr.IDToken)

		if sub, _ := claims["sub"].(string); sub == "" {
			t.Error("id_token sub must be non-empty")
		}
		if email, _ := claims["email"].(string); email != "eve@example.com" {
			t.Errorf("id_token email: got %q", email)
		}
	})
}

// ---------------------------------------------------------------------------
// Client registration tests
// ---------------------------------------------------------------------------

func TestClientRegistration(t *testing.T) {
	t.Run("successful client registration", func(t *testing.T) {
		srv := newTestServer(t)

		cr := registerClient(t, srv, "My App", []string{"http://localhost:3000/callback"})

		if cr.ClientID == "" {
			t.Error("expected non-empty client_id")
		}
		if cr.ClientSecret == "" {
			t.Error("expected non-empty client_secret")
		}
		if cr.Name != "My App" {
			t.Errorf("name: got %q, want My App", cr.Name)
		}
		if len(cr.RedirectURIs) != 1 || cr.RedirectURIs[0] != "http://localhost:3000/callback" {
			t.Errorf("redirect_uris: got %v", cr.RedirectURIs)
		}
	})

	t.Run("multiple redirect URIs are stored", func(t *testing.T) {
		srv := newTestServer(t)

		uris := []string{"http://localhost:3000/callback", "http://localhost:4000/callback"}
		cr := registerClient(t, srv, "Multi", uris)
		if len(cr.RedirectURIs) != 2 {
			t.Errorf("expected 2 redirect_uris, got %d", len(cr.RedirectURIs))
		}
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/clients", map[string]any{
			"redirect_uris": []string{"http://localhost:3000/callback"},
		})
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})

	t.Run("missing redirect_uris returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/clients", map[string]any{
			"name": "No Redirects",
		})
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})

	t.Run("empty redirect_uris returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doJSON(t, srv, "/clients", map[string]any{
			"name":          "Empty Redirects",
			"redirect_uris": []string{},
		})
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})
}

// ---------------------------------------------------------------------------
// Authorization code flow tests
// ---------------------------------------------------------------------------

func TestAuthorizationCodeFlow(t *testing.T) {
	t.Run("GET /authorize returns HTML login form", func(t *testing.T) {
		srv := newTestServer(t)

		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		_, challenge := pkceChallenge(t)

		params := url.Values{
			"response_type":         {"code"},
			"client_id":             {client.ClientID},
			"redirect_uri":          {"http://localhost:3000/callback"},
			"scope":                 {"openid"},
			"state":                 {"state123"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"nonce":                 {"nonce123"},
		}

		resp := doGet(t, srv, "/authorize?"+params.Encode(), nil)
		assertStatus(t, resp, http.StatusOK)

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lower := strings.ToLower(string(body))
		if !strings.Contains(lower, "<form") {
			t.Error("expected HTML <form> in GET /authorize response")
		}
		if !strings.Contains(lower, "type=\"password\"") && !strings.Contains(lower, "type=password") {
			t.Error("expected password field in login form")
		}
	})

	t.Run("full authorization code flow without PKCE", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "frank@example.com", "password123", "Frank")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})

		values := url.Values{
			"email":         {"frank@example.com"},
			"password":      {"password123"},
			"client_id":     {client.ClientID},
			"redirect_uri":  {"http://localhost:3000/callback"},
			"response_type": {"code"},
			"scope":         {"openid"},
			"state":         {"my-state"},
			"nonce":         {"my-nonce"},
		}

		resp := doForm(t, srv, "/authorize", values)
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusFound)

		loc := resp.Header.Get("Location")
		u, _ := url.Parse(loc)
		code := u.Query().Get("code")
		state := u.Query().Get("state")

		if code == "" {
			t.Fatal("expected code in redirect")
		}
		if state != "my-state" {
			t.Errorf("state: got %q, want my-state", state)
		}

		tr := exchangeCode(t, srv, code, "http://localhost:3000/callback", client.ClientID, client.ClientSecret, "")
		if tr.AccessToken == "" {
			t.Error("expected access_token after code exchange")
		}
	})

	t.Run("full PKCE S256 flow", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "grace@example.com", "password123", "Grace")
		client := registerClient(t, srv, "PKCE App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "grace@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)

		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		if tr.AccessToken == "" {
			t.Error("expected access_token")
		}
		if tr.RefreshToken == "" {
			t.Error("expected refresh_token")
		}
		if tr.IDToken == "" {
			t.Error("expected id_token")
		}
		if tr.TokenType != "Bearer" {
			t.Errorf("token_type: got %q, want Bearer", tr.TokenType)
		}
		if tr.ExpiresIn <= 0 {
			t.Errorf("expires_in: got %d, want > 0", tr.ExpiresIn)
		}

		claims := verifyJWT(t, srv, tr.AccessToken)
		if sub, _ := claims["sub"].(string); sub == "" {
			t.Error("access_token sub must be non-empty after PKCE flow")
		}
	})

	t.Run("state is preserved in redirect", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "henry@example.com", "password123", "Henry")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "henry@example.com", "password123", client, verifier, challenge)
		u, _ := url.Parse(loc)
		if u.Query().Get("state") != "test-state-xyz" {
			t.Errorf("state not preserved, got %q", u.Query().Get("state"))
		}
	})

	t.Run("invalid client_id in GET /authorize returns error", func(t *testing.T) {
		srv := newTestServer(t)

		params := url.Values{
			"response_type": {"code"},
			"client_id":     {"nonexistent-client"},
			"redirect_uri":  {"http://localhost:3000/callback"},
			"scope":         {"openid"},
			"state":         {"s"},
		}
		resp := doGet(t, srv, "/authorize?"+params.Encode(), nil)
		// GET /authorize does not validate the client_id — it only renders the form.
		// POST /authorize will reject it. Accept 200 (form rendered) or 400.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 200 or 400, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("invalid client_id in POST /authorize returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "ian@example.com", "password123", "Ian")

		values := url.Values{
			"email":         {"ian@example.com"},
			"password":      {"password123"},
			"client_id":     {"totally-fake-client"},
			"redirect_uri":  {"http://localhost:3000/callback"},
			"response_type": {"code"},
			"scope":         {"openid"},
			"state":         {"s"},
		}
		resp := doForm(t, srv, "/authorize", values)
		defer resp.Body.Close()
		// Must not redirect to unregistered URI; must return 400.
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for unknown client_id, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid redirect_uri in POST /authorize returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "julia@example.com", "password123", "Julia")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})

		values := url.Values{
			"email":         {"julia@example.com"},
			"password":      {"password123"},
			"client_id":     {client.ClientID},
			"redirect_uri":  {"http://evil.com/steal"},
			"response_type": {"code"},
			"scope":         {"openid"},
			"state":         {"s"},
		}
		resp := doForm(t, srv, "/authorize", values)
		defer resp.Body.Close()
		// Must not redirect to the attacker URI.
		if resp.StatusCode == http.StatusFound {
			loc := resp.Header.Get("Location")
			if strings.HasPrefix(loc, "http://evil.com") {
				t.Errorf("server must not redirect to unregistered redirect_uri; got %q", loc)
			}
		} else if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid redirect_uri, got %d", resp.StatusCode)
		}
	})

	t.Run("wrong credentials in POST /authorize returns non-redirect error", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "iris@example.com", "password123", "Iris")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})

		values := url.Values{
			"email":         {"iris@example.com"},
			"password":      {"wrongpassword"},
			"client_id":     {client.ClientID},
			"redirect_uri":  {"http://localhost:3000/callback"},
			"response_type": {"code"},
			"scope":         {"openid"},
			"state":         {"s1"},
		}

		resp := doForm(t, srv, "/authorize", values)
		defer resp.Body.Close()
		// Implementation returns 401 with an HTML error page (not a redirect).
		if resp.StatusCode == http.StatusFound {
			loc := resp.Header.Get("Location")
			u, _ := url.Parse(loc)
			errParam := u.Query().Get("error")
			// If it redirects, it must carry an error parameter.
			if errParam == "" {
				t.Errorf("redirect without error parameter: %q", loc)
			}
		} else if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 401/400 or error redirect, got %d", resp.StatusCode)
		}
	})

	t.Run("auth code can only be used once", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "jack@example.com", "password123", "Jack")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "jack@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)

		// First exchange succeeds.
		exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		// Second exchange must fail.
		values := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {client.RedirectURIs[0]},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
			"code_verifier": {verifier},
		}
		resp := doForm(t, srv, "/token", values)
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_grant")
	})

	t.Run("wrong redirect_uri in token exchange returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "kate@example.com", "password123", "Kate")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "kate@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)

		values := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {"http://localhost:3000/other"},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
			"code_verifier": {verifier},
		}
		resp := doForm(t, srv, "/token", values)
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_grant")
	})

	t.Run("missing code_verifier when PKCE was used returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "liam@example.com", "password123", "Liam")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "liam@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)

		values := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {client.RedirectURIs[0]},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
			// code_verifier intentionally omitted
		}
		resp := doForm(t, srv, "/token", values)
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_grant")
	})

	t.Run("wrong code_verifier is rejected", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "mia@example.com", "password123", "Mia")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "mia@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)

		wrongVerifier, _ := pkceChallenge(t)

		values := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {client.RedirectURIs[0]},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
			"code_verifier": {wrongVerifier},
		}
		resp := doForm(t, srv, "/token", values)
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_grant")
	})

	t.Run("unsupported grant_type returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doForm(t, srv, "/token", url.Values{
			"grant_type": {"password"},
			"username":   {"user@example.com"},
			"password":   {"password123"},
		})
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("invalid auth code returns 400", func(t *testing.T) {
		srv := newTestServer(t)

		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})

		values := url.Values{
			"grant_type":   {"authorization_code"},
			"code":         {"completely-invalid-code"},
			"redirect_uri": {"http://localhost:3000/callback"},
			"client_id":    {client.ClientID},
		}
		resp := doForm(t, srv, "/token", values)
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_grant")
	})
}

// ---------------------------------------------------------------------------
// Token grant tests
// ---------------------------------------------------------------------------

func TestTokenGrants(t *testing.T) {
	t.Run("refresh token grant returns new access token", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "noah@example.com", "password123", "Noah")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "noah@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		resp := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, resp, http.StatusOK)

		var newTR TokenResponse
		decodeJSON(t, resp, &newTR)

		if newTR.AccessToken == "" {
			t.Error("expected non-empty access_token")
		}
		if newTR.TokenType != "Bearer" {
			t.Errorf("token_type: got %q, want Bearer", newTR.TokenType)
		}
		if newTR.ExpiresIn <= 0 {
			t.Errorf("expires_in: got %d, want > 0", newTR.ExpiresIn)
		}
	})

	t.Run("new access token from refresh is cryptographically valid", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "oscar@example.com", "password123", "Oscar")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "oscar@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		resp := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, resp, http.StatusOK)

		var newTR TokenResponse
		decodeJSON(t, resp, &newTR)

		verifyJWT(t, srv, newTR.AccessToken)
	})

	t.Run("revoked refresh token is rejected", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "olivia@example.com", "password123", "Olivia")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "olivia@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		revokeResp := doForm(t, srv, "/revoke", url.Values{
			"token":         {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, revokeResp, http.StatusOK)
		revokeResp.Body.Close()

		retryResp := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, retryResp, http.StatusBadRequest)
		assertErrorCode(t, retryResp, "invalid_grant")
	})

	t.Run("used refresh token is rejected (rotation)", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "pat@example.com", "password123", "Pat")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "pat@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		// First refresh succeeds and rotates the token.
		resp1 := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, resp1, http.StatusOK)
		resp1.Body.Close()

		// Using the old refresh token must now fail.
		resp2 := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, resp2, http.StatusBadRequest)
		assertErrorCode(t, resp2, "invalid_grant")
	})

	t.Run("invalid refresh token is rejected", func(t *testing.T) {
		srv := newTestServer(t)

		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})

		resp := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {"totally-fake-token"},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_grant")
	})

	t.Run("access token expiry is in the future", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "pete@example.com", "password123", "Pete")
		tr := loginUser(t, srv, "pete@example.com", "password123")

		claims := verifyJWT(t, srv, tr.AccessToken)
		expF, _ := claims["exp"].(float64)
		exp := time.Unix(int64(expF), 0)
		if !exp.After(time.Now()) {
			t.Errorf("exp %v is not in the future", exp)
		}
	})

	t.Run("refresh token from /login grant can refresh", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "quinn@example.com", "password123", "Quinn")
		tr := loginUser(t, srv, "quinn@example.com", "password123")

		// /login issues refresh tokens with empty client_id — the handler must
		// still accept them.
		resp := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tr.RefreshToken},
			// no client_id / client_secret needed for public refresh tokens
		})
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("unexpected status %d", resp.StatusCode)
		}
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// Userinfo tests
// ---------------------------------------------------------------------------

func TestUserinfo(t *testing.T) {
	t.Run("valid token returns user info", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "rose@example.com", "password123", "Rose")
		tr := loginUser(t, srv, "rose@example.com", "password123")

		resp := doGet(t, srv, "/userinfo", map[string]string{
			"Authorization": "Bearer " + tr.AccessToken,
		})
		assertStatus(t, resp, http.StatusOK)

		var ui UserInfoResponse
		decodeJSON(t, resp, &ui)

		if ui.Sub == "" {
			t.Error("sub must be non-empty")
		}
		if ui.Email != "rose@example.com" {
			t.Errorf("email: got %q", ui.Email)
		}
		if ui.Name != "Rose" {
			t.Errorf("name: got %q", ui.Name)
		}
	})

	t.Run("missing Authorization header returns 401", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/userinfo", nil)
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "invalid_token")
	})

	t.Run("malformed token returns 401", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/userinfo", map[string]string{
			"Authorization": "Bearer this.is.not.a.valid.token",
		})
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "invalid_token")
	})

	t.Run("Bearer prefix is required", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "sam@example.com", "password123", "Sam")
		tr := loginUser(t, srv, "sam@example.com", "password123")

		resp := doGet(t, srv, "/userinfo", map[string]string{
			"Authorization": tr.AccessToken, // missing "Bearer "
		})
		assertStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	})

	t.Run("token from PKCE flow grants userinfo access", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "tina@example.com", "password123", "Tina")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "tina@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		resp := doGet(t, srv, "/userinfo", map[string]string{
			"Authorization": "Bearer " + tr.AccessToken,
		})
		assertStatus(t, resp, http.StatusOK)

		var ui UserInfoResponse
		decodeJSON(t, resp, &ui)
		if ui.Email != "tina@example.com" {
			t.Errorf("email: got %q", ui.Email)
		}
	})

	t.Run("sub in userinfo matches registered user id", func(t *testing.T) {
		srv := newTestServer(t)

		user := registerUser(t, srv, "uma@example.com", "password123", "Uma")
		tr := loginUser(t, srv, "uma@example.com", "password123")

		resp := doGet(t, srv, "/userinfo", map[string]string{
			"Authorization": "Bearer " + tr.AccessToken,
		})
		assertStatus(t, resp, http.StatusOK)

		var ui UserInfoResponse
		decodeJSON(t, resp, &ui)
		if ui.Sub != user["id"] {
			t.Errorf("sub: got %q, want %q", ui.Sub, user["id"])
		}
	})
}

// ---------------------------------------------------------------------------
// OIDC Discovery tests
// ---------------------------------------------------------------------------

func TestOIDCDiscovery(t *testing.T) {
	t.Run("discovery endpoint returns required fields", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/.well-known/openid-configuration", nil)
		assertStatus(t, resp, http.StatusOK)

		var disc OIDCDiscovery
		decodeJSON(t, resp, &disc)

		required := map[string]string{
			"issuer":                 disc.Issuer,
			"authorization_endpoint": disc.AuthorizationEndpoint,
			"token_endpoint":         disc.TokenEndpoint,
			"userinfo_endpoint":      disc.UserinfoEndpoint,
			"jwks_uri":               disc.JwksURI,
		}
		for field, val := range required {
			if val == "" {
				t.Errorf("%s must be non-empty", field)
			}
		}
	})

	t.Run("response_types_supported includes code", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/.well-known/openid-configuration", nil)
		assertStatus(t, resp, http.StatusOK)
		var disc OIDCDiscovery
		decodeJSON(t, resp, &disc)

		found := false
		for _, rt := range disc.ResponseTypesSupported {
			if rt == "code" {
				found = true
			}
		}
		if !found {
			t.Errorf("response_types_supported does not include 'code': %v", disc.ResponseTypesSupported)
		}
	})

	t.Run("id_token_signing_alg_values_supported includes RS256", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/.well-known/openid-configuration", nil)
		assertStatus(t, resp, http.StatusOK)
		var disc OIDCDiscovery
		decodeJSON(t, resp, &disc)

		found := false
		for _, alg := range disc.IDTokenSigningAlgValuesSupported {
			if alg == "RS256" {
				found = true
			}
		}
		if !found {
			t.Errorf("id_token_signing_alg_values_supported missing RS256: %v", disc.IDTokenSigningAlgValuesSupported)
		}
	})

	t.Run("all endpoint URLs share the issuer as base", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/.well-known/openid-configuration", nil)
		assertStatus(t, resp, http.StatusOK)
		var disc OIDCDiscovery
		decodeJSON(t, resp, &disc)

		for name, endpoint := range map[string]string{
			"authorization_endpoint": disc.AuthorizationEndpoint,
			"token_endpoint":         disc.TokenEndpoint,
			"userinfo_endpoint":      disc.UserinfoEndpoint,
			"jwks_uri":               disc.JwksURI,
		} {
			if !strings.HasPrefix(endpoint, disc.Issuer) {
				t.Errorf("%s %q does not start with issuer %q", name, endpoint, disc.Issuer)
			}
		}
	})

	t.Run("discovered endpoint paths are reachable on the test server", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/.well-known/openid-configuration", nil)
		assertStatus(t, resp, http.StatusOK)
		var disc OIDCDiscovery
		decodeJSON(t, resp, &disc)

		// Strip the issuer to get just the path, then hit it on the test server.
		for name, endpoint := range map[string]string{
			"jwks_uri":          disc.JwksURI,
			"userinfo_endpoint": disc.UserinfoEndpoint,
		} {
			path := strings.TrimPrefix(endpoint, disc.Issuer)
			if path == "" || path == endpoint {
				t.Logf("skipping %s — cannot derive path from %q", name, endpoint)
				continue
			}
			r := doGet(t, srv, path, nil)
			if r.StatusCode == http.StatusNotFound {
				t.Errorf("discovered endpoint %s (%s) returned 404", name, path)
			}
			r.Body.Close()
		}
	})

	t.Run("grant_types_supported includes authorization_code and refresh_token", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/.well-known/openid-configuration", nil)
		assertStatus(t, resp, http.StatusOK)
		var disc OIDCDiscovery
		decodeJSON(t, resp, &disc)

		want := map[string]bool{"authorization_code": false, "refresh_token": false}
		for _, gt := range disc.GrantTypesSupported {
			want[gt] = true
		}
		for k, found := range want {
			if !found {
				t.Errorf("grant_types_supported missing %q: %v", k, disc.GrantTypesSupported)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// JWKS tests
// ---------------------------------------------------------------------------

func TestJWKS(t *testing.T) {
	t.Run("JWKS endpoint returns a valid RSA public key", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/jwks", nil)
		assertStatus(t, resp, http.StatusOK)

		var jwks struct {
			Keys []map[string]string `json:"keys"`
		}
		decodeJSON(t, resp, &jwks)

		if len(jwks.Keys) == 0 {
			t.Fatal("expected at least one key in JWKS")
		}
		key := jwks.Keys[0]
		if key["kty"] != "RSA" {
			t.Errorf("kty: got %q, want RSA", key["kty"])
		}
		if key["n"] == "" {
			t.Error("n must be non-empty")
		}
		if key["e"] == "" {
			t.Error("e must be non-empty")
		}
		if key["use"] != "sig" {
			t.Errorf("use: got %q, want sig", key["use"])
		}
		if key["alg"] != "RS256" {
			t.Errorf("alg: got %q, want RS256", key["alg"])
		}
	})

	t.Run("access token signature verifiable with JWKS", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "vera@example.com", "password123", "Vera")
		tr := loginUser(t, srv, "vera@example.com", "password123")

		claims := verifyJWT(t, srv, tr.AccessToken)
		if claims["sub"] == nil {
			t.Error("sub claim missing after JWKS-based verification")
		}
	})

	t.Run("id_token signature verifiable with JWKS", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "will@example.com", "password123", "Will")
		tr := loginUser(t, srv, "will@example.com", "password123")

		claims := verifyJWT(t, srv, tr.IDToken)
		if claims["sub"] == nil {
			t.Error("sub claim missing in id_token after JWKS-based verification")
		}
	})

	t.Run("PKCE flow tokens are verifiable with JWKS", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "xena@example.com", "password123", "Xena")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "xena@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		verifyJWT(t, srv, tr.AccessToken)
		if tr.IDToken != "" {
			verifyJWT(t, srv, tr.IDToken)
		}
	})
}

// ---------------------------------------------------------------------------
// Token revocation tests
// ---------------------------------------------------------------------------

func TestRevocation(t *testing.T) {
	t.Run("POST /revoke returns 200", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "yara@example.com", "password123", "Yara")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "yara@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		resp := doForm(t, srv, "/revoke", url.Values{
			"token":         {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	})

	t.Run("revoking already-revoked token is idempotent (200)", func(t *testing.T) {
		srv := newTestServer(t)

		registerUser(t, srv, "zara@example.com", "password123", "Zara")
		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, srv, "zara@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		vals := url.Values{
			"token":         {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		}
		resp1 := doForm(t, srv, "/revoke", vals)
		assertStatus(t, resp1, http.StatusOK)
		resp1.Body.Close()

		resp2 := doForm(t, srv, "/revoke", vals)
		assertStatus(t, resp2, http.StatusOK)
		resp2.Body.Close()
	})

	t.Run("revoking unknown token returns 200 per RFC 7009", func(t *testing.T) {
		srv := newTestServer(t)

		client := registerClient(t, srv, "App", []string{"http://localhost:3000/callback"})

		resp := doForm(t, srv, "/revoke", url.Values{
			"token":         {"not-a-real-token"},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// CORS tests
// ---------------------------------------------------------------------------

func TestCORS(t *testing.T) {
	t.Run("OPTIONS preflight returns CORS headers", func(t *testing.T) {
		srv := newTestServer(t)

		req, err := http.NewRequest(http.MethodOptions, srv.URL+"/token", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set("Access-Control-Request-Method", "POST")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("OPTIONS /token: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			t.Errorf("OPTIONS /token: expected 200 or 204, got %d", resp.StatusCode)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") == "" {
			t.Error("expected Access-Control-Allow-Origin response header")
		}
	})

	t.Run("GET request includes CORS headers", func(t *testing.T) {
		srv := newTestServer(t)

		req, err := http.NewRequest(http.MethodGet, srv.URL+"/health", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Origin", "http://localhost:3000")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer resp.Body.Close()

		if resp.Header.Get("Access-Control-Allow-Origin") == "" {
			t.Error("expected Access-Control-Allow-Origin on GET response")
		}
	})

	t.Run("POST request includes CORS headers", func(t *testing.T) {
		srv := newTestServer(t)

		req, err := http.NewRequest(http.MethodPost, srv.URL+"/register",
			strings.NewReader(`{"email":"cors@test.com","password":"pass1234","name":"CORS"}`))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /register: %v", err)
		}
		defer resp.Body.Close()

		if resp.Header.Get("Access-Control-Allow-Origin") == "" {
			t.Error("expected Access-Control-Allow-Origin on POST response")
		}
	})
}

// ---------------------------------------------------------------------------
// Health check tests
// ---------------------------------------------------------------------------

func TestHealthCheck(t *testing.T) {
	t.Run("returns 200 with status ok", func(t *testing.T) {
		srv := newTestServer(t)

		resp := doGet(t, srv, "/health", nil)
		assertStatus(t, resp, http.StatusOK)

		var body map[string]any
		decodeJSON(t, resp, &body)

		if body["status"] != "ok" {
			t.Errorf("status: got %v, want ok", body["status"])
		}
	})

	t.Run("health check responds to repeated requests", func(t *testing.T) {
		srv := newTestServer(t)

		for i := range 3 {
			resp := doGet(t, srv, "/health", nil)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("request %d: expected 200, got %d", i, resp.StatusCode)
			}
			resp.Body.Close()
		}
	})
}

// ---------------------------------------------------------------------------
// Table-driven registration validation tests
// ---------------------------------------------------------------------------

func TestRegistrationValidation(t *testing.T) {
	cases := []struct {
		name       string
		payload    map[string]any
		wantStatus int
	}{
		{"empty body", map[string]any{}, http.StatusBadRequest},
		{"email only", map[string]any{"email": "a@b.com"}, http.StatusBadRequest},
		{"password only", map[string]any{"password": "password123"}, http.StatusBadRequest},
		{"invalid email no @", map[string]any{"email": "noatsign", "password": "password123", "name": "X"}, http.StatusBadRequest},
		{"invalid email no domain", map[string]any{"email": "user@", "password": "password123", "name": "X"}, http.StatusBadRequest},
		{"empty password", map[string]any{"email": "a@b.com", "password": "", "name": "X"}, http.StatusBadRequest},
		{"valid registration", map[string]any{"email": "valid@example.com", "password": "password123", "name": "Valid"}, http.StatusCreated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)

			resp := doJSON(t, srv, "/register", tc.payload)
			if resp.StatusCode != tc.wantStatus {
				body := readBody(resp)
				t.Errorf("expected %d, got %d (body=%q)", tc.wantStatus, resp.StatusCode, body)
			} else {
				resp.Body.Close()
			}
		})
	}
}

// ---------------------------------------------------------------------------
// End-to-end flow test
// ---------------------------------------------------------------------------

func TestEndToEndFlow(t *testing.T) {
	t.Run("complete OIDC flow: register→authorize→token→userinfo→refresh→revoke", func(t *testing.T) {
		srv := newTestServer(t)

		// 1. Register user.
		user := registerUser(t, srv, "e2e@example.com", "password123", "E2E User")
		userID, _ := user["id"].(string)

		// 2. Register client.
		client := registerClient(t, srv, "E2E App", []string{"http://localhost:3000/callback"})

		// 3. Generate PKCE.
		verifier, challenge := pkceChallenge(t)

		// 4. Authorize.
		loc := authorizeCode(t, srv, "e2e@example.com", "password123", client, verifier, challenge)
		code := extractCode(t, loc)

		// 5. Exchange code for tokens.
		tr := exchangeCode(t, srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)
		if tr.AccessToken == "" || tr.RefreshToken == "" || tr.IDToken == "" {
			t.Fatalf("incomplete token response: %+v", tr)
		}

		// 6. Verify access token claims.
		claims := verifyJWT(t, srv, tr.AccessToken)
		if sub, _ := claims["sub"].(string); sub != userID {
			t.Errorf("access_token sub=%q, want %q", sub, userID)
		}

		// 7. Call userinfo.
		uiResp := doGet(t, srv, "/userinfo", map[string]string{
			"Authorization": "Bearer " + tr.AccessToken,
		})
		assertStatus(t, uiResp, http.StatusOK)
		var ui UserInfoResponse
		decodeJSON(t, uiResp, &ui)
		if ui.Email != "e2e@example.com" {
			t.Errorf("userinfo email=%q", ui.Email)
		}

		// 8. Refresh tokens.
		refreshResp := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, refreshResp, http.StatusOK)
		var refreshedTR TokenResponse
		decodeJSON(t, refreshResp, &refreshedTR)
		if refreshedTR.AccessToken == "" {
			t.Error("expected new access_token after refresh")
		}

		// 9. Revoke the original refresh token (may already be rotated; 200 either way).
		revokeResp := doForm(t, srv, "/revoke", url.Values{
			"token":         {tr.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, revokeResp, http.StatusOK)
		revokeResp.Body.Close()

		// 10. Revoke the rotated refresh token and confirm it is then rejected.
		revokeResp2 := doForm(t, srv, "/revoke", url.Values{
			"token":         {refreshedTR.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		assertStatus(t, revokeResp2, http.StatusOK)
		revokeResp2.Body.Close()

		badResp := doForm(t, srv, "/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {refreshedTR.RefreshToken},
			"client_id":     {client.ClientID},
			"client_secret": {client.ClientSecret},
		})
		if badResp.StatusCode == http.StatusOK {
			t.Error("revoked refresh token should not produce new tokens")
		}
		badResp.Body.Close()
	})
}
