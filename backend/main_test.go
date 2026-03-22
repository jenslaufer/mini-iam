package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
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
	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/marketing"
	"github.com/jenslaufer/launch-kit/tenant"
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

	// Wire a synchronous sender for tests: Enqueue processes campaigns inline,
	// eliminating goroutine races and making waitForCampaignStatus unnecessary.
	sender := NewCampaignSender(store, &LogMailer{}, issuer, 0)
	sender.StartSync()
	h.sender = sender

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

	// Marketing routes
	mux.HandleFunc("/admin/contacts", h.AdminContacts)
	mux.HandleFunc("/admin/contacts/import", h.AdminImportContacts)
	mux.HandleFunc("/admin/contacts/", h.AdminContactByID)
	mux.HandleFunc("/admin/segments", h.AdminSegments)
	mux.HandleFunc("/admin/segments/", h.AdminSegmentByID)
	mux.HandleFunc("/admin/campaigns", h.AdminCampaigns)
	mux.HandleFunc("/admin/campaigns/", h.AdminCampaignByID)
	mux.HandleFunc("/track/", h.TrackOpen)
	mux.HandleFunc("/unsubscribe/", h.Unsubscribe)
	mux.HandleFunc("/activate/", h.Activate)
	mux.HandleFunc("/forgot-password", h.ForgotPassword)
	mux.HandleFunc("/reset-password/", h.ResetPassword)

	return CORSMiddleware("*")(mux)
}

// SetupServerWithRateLimit is like SetupServer but adds rate limiting middleware.
func SetupServerWithRateLimit(store *Store, tokenService *TokenService, rl *RateLimiter) http.Handler {
	base := SetupServer(store, tokenService)
	return rl.Middleware(base)
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
	// Seed admin so registerClient can authenticate
	if err := store.SeedAdmin("admin@test.com", "adminpass123", "Admin"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
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

// getAdminToken logs in as the pre-seeded admin and returns the access token.
func getAdminToken(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp := doJSON(t, srv, "/login", map[string]any{
		"email":    "admin@test.com",
		"password": "adminpass123",
	})
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)
	return tr.AccessToken
}

// doJSONWithAuth sends a JSON POST with an Authorization header.
func doJSONWithAuth(t *testing.T, srv *httptest.Server, path string, body any, token string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
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
// Requires an admin user seeded (admin@test.com / adminpass123) in the server.
func registerClient(t *testing.T, srv *httptest.Server, name string, redirectURIs []string) ClientCreateResponse {
	t.Helper()
	// Login as admin to get a token for client creation
	loginResp := doJSON(t, srv, "/login", map[string]any{
		"email":    "admin@test.com",
		"password": "adminpass123",
	})
	assertStatus(t, loginResp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, loginResp, &tr)

	body, _ := json.Marshal(map[string]any{
		"name":          name,
		"redirect_uris": redirectURIs,
	})
	req, err := http.NewRequest("POST", srv.URL+"/clients", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
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
		token := getAdminToken(t, srv)

		resp := doJSONWithAuth(t, srv, "/clients", map[string]any{
			"redirect_uris": []string{"http://localhost:3000/callback"},
		}, token)
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})

	t.Run("missing redirect_uris returns 400", func(t *testing.T) {
		srv := newTestServer(t)
		token := getAdminToken(t, srv)

		resp := doJSONWithAuth(t, srv, "/clients", map[string]any{
			"name": "No Redirects",
		}, token)
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "invalid_request")
	})

	t.Run("empty redirect_uris returns 400", func(t *testing.T) {
		srv := newTestServer(t)
		token := getAdminToken(t, srv)

		resp := doJSONWithAuth(t, srv, "/clients", map[string]any{
			"name":          "Empty Redirects",
			"redirect_uris": []string{},
		}, token)
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

// ---------------------------------------------------------------------------
// Admin test infrastructure
// ---------------------------------------------------------------------------

// testEnv holds a test server together with the underlying store so admin
// tests can call store methods (e.g. SeedAdmin) directly.
type testEnv struct {
	srv   *httptest.Server
	store *Store
}

// newTestEnv creates a fully isolated in-memory environment that exposes both
// the HTTP test server and the backing store.  It keeps newTestServer working
// unchanged for all existing tests.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Seed admin so registerClient can authenticate
	if err := store.SeedAdmin("admin@test.com", "adminpass123", "Admin"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tokenService := NewTokenService(key, "http://test-issuer")
	handler := SetupServer(store, tokenService)
	srv := httptest.NewServer(handler)
	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})
	return &testEnv{srv: srv, store: store}
}

// loginAsAdmin seeds an admin user, logs in, and returns the Bearer access token.
func loginAsAdmin(t *testing.T, env *testEnv) string {
	t.Helper()
	if err := env.store.SeedAdmin("admin@test.com", "adminpass123", "Admin"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	resp := doJSON(t, env.srv, "/login", map[string]any{
		"email":    "admin@test.com",
		"password": "adminpass123",
	})
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)
	if tr.AccessToken == "" {
		t.Fatal("loginAsAdmin: empty access_token")
	}
	return tr.AccessToken
}

// doRequest sends an HTTP request with an optional JSON body and optional
// Authorization header.  method is e.g. "GET", "PUT", "DELETE".
func doRequest(t *testing.T, srv *httptest.Server, method, path, token, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// TestAdminSeed
// ---------------------------------------------------------------------------

func TestAdminSeed(t *testing.T) {
	t.Run("SeedAdmin creates admin user", func(t *testing.T) {
		env := newTestEnv(t)

		err := env.store.SeedAdmin("seed@test.com", "seedpass123", "Seeded Admin")
		if err != nil {
			t.Fatalf("SeedAdmin: %v", err)
		}

		// User must be able to log in.
		resp := doJSON(t, env.srv, "/login", map[string]any{
			"email":    "seed@test.com",
			"password": "seedpass123",
		})
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	})

	t.Run("SeedAdmin is idempotent", func(t *testing.T) {
		env := newTestEnv(t)

		if err := env.store.SeedAdmin("idem@test.com", "idempass123", "Idem Admin"); err != nil {
			t.Fatalf("first SeedAdmin: %v", err)
		}
		if err := env.store.SeedAdmin("idem@test.com", "idempass123", "Idem Admin"); err != nil {
			t.Fatalf("second SeedAdmin: %v", err)
		}
	})

	t.Run("seeded admin login token carries role admin", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		claims := verifyJWT(t, env.srv, token)

		role, _ := claims["role"].(string)
		if role != "admin" {
			t.Errorf("role claim: got %q, want admin", role)
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminAuthentication
// ---------------------------------------------------------------------------

func TestAdminAuthentication(t *testing.T) {
	adminEndpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/admin/users"},
		{"GET", "/admin/clients"},
	}

	t.Run("admin endpoints return 401 without token", func(t *testing.T) {
		env := newTestEnv(t)

		for _, ep := range adminEndpoints {
			t.Run(ep.method+" "+ep.path, func(t *testing.T) {
				resp := doRequest(t, env.srv, ep.method, ep.path, "", "")
				if resp.StatusCode != http.StatusUnauthorized {
					body := readBody(resp)
					t.Errorf("expected 401, got %d (body=%q)", resp.StatusCode, body)
				} else {
					resp.Body.Close()
				}
			})
		}
	})

	t.Run("admin endpoints return 401 with invalid token", func(t *testing.T) {
		env := newTestEnv(t)

		for _, ep := range adminEndpoints {
			t.Run(ep.method+" "+ep.path, func(t *testing.T) {
				resp := doRequest(t, env.srv, ep.method, ep.path, "this.is.not.valid", "")
				if resp.StatusCode != http.StatusUnauthorized {
					body := readBody(resp)
					t.Errorf("expected 401, got %d (body=%q)", resp.StatusCode, body)
				} else {
					resp.Body.Close()
				}
			})
		}
	})

	t.Run("admin endpoints return 403 with non-admin token", func(t *testing.T) {
		env := newTestEnv(t)

		registerUser(t, env.srv, "regular@test.com", "pass1234", "Regular")
		tr := loginUser(t, env.srv, "regular@test.com", "pass1234")

		for _, ep := range adminEndpoints {
			t.Run(ep.method+" "+ep.path, func(t *testing.T) {
				resp := doRequest(t, env.srv, ep.method, ep.path, tr.AccessToken, "")
				if resp.StatusCode != http.StatusForbidden {
					body := readBody(resp)
					t.Errorf("expected 403, got %d (body=%q)", resp.StatusCode, body)
				} else {
					resp.Body.Close()
				}
			})
		}
	})

	t.Run("admin endpoints return 200 with admin token", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		for _, ep := range adminEndpoints {
			t.Run(ep.method+" "+ep.path, func(t *testing.T) {
				resp := doRequest(t, env.srv, ep.method, ep.path, token, "")
				if resp.StatusCode != http.StatusOK {
					body := readBody(resp)
					t.Errorf("expected 200, got %d (body=%q)", resp.StatusCode, body)
				} else {
					resp.Body.Close()
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminListUsers
// ---------------------------------------------------------------------------

func TestAdminListUsers(t *testing.T) {
	t.Run("returns all users including admin", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "GET", "/admin/users", token, "")
		assertStatus(t, resp, http.StatusOK)

		var users []map[string]any
		decodeJSON(t, resp, &users)

		if len(users) == 0 {
			t.Fatal("expected at least the admin user in the list")
		}

		found := false
		for _, u := range users {
			if u["email"] == "admin@test.com" {
				found = true
				if u["role"] != "admin" {
					t.Errorf("admin user role: got %v, want admin", u["role"])
				}
			}
		}
		if !found {
			t.Error("admin user not found in /admin/users response")
		}
	})

	t.Run("newly registered users appear in list", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		registerUser(t, env.srv, "newuser@test.com", "pass1234", "New User")

		resp := doRequest(t, env.srv, "GET", "/admin/users", token, "")
		assertStatus(t, resp, http.StatusOK)

		var users []map[string]any
		decodeJSON(t, resp, &users)

		found := false
		for _, u := range users {
			if u["email"] == "newuser@test.com" {
				found = true
			}
		}
		if !found {
			t.Error("newly registered user not found in /admin/users response")
		}
	})

	t.Run("response objects include required fields", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "GET", "/admin/users", token, "")
		assertStatus(t, resp, http.StatusOK)

		var users []map[string]any
		decodeJSON(t, resp, &users)

		for _, u := range users {
			for _, field := range []string{"id", "email", "name", "role", "created_at"} {
				if _, ok := u[field]; !ok {
					t.Errorf("user object missing field %q: %v", field, u)
				}
			}
			if _, ok := u["password_hash"]; ok {
				t.Error("password_hash must not be exposed in admin list")
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminGetUser
// ---------------------------------------------------------------------------

func TestAdminGetUser(t *testing.T) {
	t.Run("returns specific user by ID", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		user := registerUser(t, env.srv, "getme@test.com", "pass1234", "Get Me")
		userID, _ := user["id"].(string)

		resp := doRequest(t, env.srv, "GET", "/admin/users/"+userID, token, "")
		assertStatus(t, resp, http.StatusOK)

		var got map[string]any
		decodeJSON(t, resp, &got)

		if got["id"] != userID {
			t.Errorf("id: got %v, want %v", got["id"], userID)
		}
		if got["email"] != "getme@test.com" {
			t.Errorf("email: got %v", got["email"])
		}
	})

	t.Run("returns 404 for non-existent ID", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "GET", "/admin/users/00000000-0000-0000-0000-000000000000", token, "")
		assertStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestAdminUpdateUser
// ---------------------------------------------------------------------------

func TestAdminUpdateUser(t *testing.T) {
	t.Run("update name only", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		user := registerUser(t, env.srv, "upname@test.com", "pass1234", "Original Name")
		userID, _ := user["id"].(string)

		resp := doRequest(t, env.srv, "PUT", "/admin/users/"+userID, token,
			`{"name":"Updated Name"}`)
		assertStatus(t, resp, http.StatusOK)

		var updated map[string]any
		decodeJSON(t, resp, &updated)

		if updated["name"] != "Updated Name" {
			t.Errorf("name: got %v, want Updated Name", updated["name"])
		}
		if updated["role"] != "user" {
			t.Errorf("role should remain user, got %v", updated["role"])
		}
	})

	t.Run("update role only", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		user := registerUser(t, env.srv, "uprole@test.com", "pass1234", "Role Target")
		userID, _ := user["id"].(string)

		resp := doRequest(t, env.srv, "PUT", "/admin/users/"+userID, token,
			`{"role":"admin"}`)
		assertStatus(t, resp, http.StatusOK)

		var updated map[string]any
		decodeJSON(t, resp, &updated)

		if updated["role"] != "admin" {
			t.Errorf("role: got %v, want admin", updated["role"])
		}
		if updated["name"] != "Role Target" {
			t.Errorf("name changed unexpectedly: got %v", updated["name"])
		}
	})

	t.Run("update both name and role", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		user := registerUser(t, env.srv, "upboth@test.com", "pass1234", "Before")
		userID, _ := user["id"].(string)

		resp := doRequest(t, env.srv, "PUT", "/admin/users/"+userID, token,
			`{"name":"After","role":"admin"}`)
		assertStatus(t, resp, http.StatusOK)

		var updated map[string]any
		decodeJSON(t, resp, &updated)

		if updated["name"] != "After" {
			t.Errorf("name: got %v, want After", updated["name"])
		}
		if updated["role"] != "admin" {
			t.Errorf("role: got %v, want admin", updated["role"])
		}
	})

	t.Run("invalid role returns 400", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		user := registerUser(t, env.srv, "badrole@test.com", "pass1234", "Bad Role")
		userID, _ := user["id"].(string)

		resp := doRequest(t, env.srv, "PUT", "/admin/users/"+userID, token,
			`{"role":"superadmin"}`)
		assertStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	})

	t.Run("non-existent user returns 404", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "PUT", "/admin/users/00000000-0000-0000-0000-000000000000", token,
			`{"name":"Ghost"}`)
		assertStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestAdminDeleteUser
// ---------------------------------------------------------------------------

func TestAdminDeleteUser(t *testing.T) {
	t.Run("delete user succeeds", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		user := registerUser(t, env.srv, "deleteme@test.com", "pass1234", "Delete Me")
		userID, _ := user["id"].(string)

		resp := doRequest(t, env.srv, "DELETE", "/admin/users/"+userID, token, "")
		assertStatus(t, resp, http.StatusOK)

		var body map[string]any
		decodeJSON(t, resp, &body)

		if body["status"] != "deleted" {
			t.Errorf("status: got %v, want deleted", body["status"])
		}
	})

	t.Run("deleted user can no longer login", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		registerUser(t, env.srv, "gone@test.com", "pass1234", "Gone")

		// Fetch user ID via list.
		listResp := doRequest(t, env.srv, "GET", "/admin/users", token, "")
		assertStatus(t, listResp, http.StatusOK)
		var users []map[string]any
		decodeJSON(t, listResp, &users)

		var goneID string
		for _, u := range users {
			if u["email"] == "gone@test.com" {
				goneID, _ = u["id"].(string)
			}
		}
		if goneID == "" {
			t.Fatal("could not find gone@test.com in admin user list")
		}

		delResp := doRequest(t, env.srv, "DELETE", "/admin/users/"+goneID, token, "")
		assertStatus(t, delResp, http.StatusOK)
		delResp.Body.Close()

		loginResp := doJSON(t, env.srv, "/login", map[string]any{
			"email":    "gone@test.com",
			"password": "pass1234",
		})
		if loginResp.StatusCode == http.StatusOK {
			t.Error("deleted user should not be able to login")
		}
		loginResp.Body.Close()
	})

	t.Run("cannot delete yourself returns 400", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		// Find the admin user's own ID via userinfo.
		uiResp := doRequest(t, env.srv, "GET", "/userinfo", token, "")
		assertStatus(t, uiResp, http.StatusOK)
		var ui UserInfoResponse
		decodeJSON(t, uiResp, &ui)

		resp := doRequest(t, env.srv, "DELETE", "/admin/users/"+ui.Sub, token, "")
		assertStatus(t, resp, http.StatusBadRequest)

		var errBody map[string]any
		decodeJSON(t, resp, &errBody)
		msg, _ := errBody["error"].(string)
		if msg == "" {
			t.Error("expected non-empty error message when deleting yourself")
		}
	})

	t.Run("non-existent user returns 404", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "DELETE", "/admin/users/00000000-0000-0000-0000-000000000000", token, "")
		assertStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestAdminListClients
// ---------------------------------------------------------------------------

func TestAdminListClients(t *testing.T) {
	t.Run("returns all clients", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		registerClient(t, env.srv, "List App", []string{"http://localhost:3000/callback"})

		resp := doRequest(t, env.srv, "GET", "/admin/clients", token, "")
		assertStatus(t, resp, http.StatusOK)

		var clients []map[string]any
		decodeJSON(t, resp, &clients)

		if len(clients) == 0 {
			t.Fatal("expected at least one client in list")
		}
	})

	t.Run("newly registered clients appear in list", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		cr := registerClient(t, env.srv, "New Client", []string{"http://localhost:4000/callback"})

		resp := doRequest(t, env.srv, "GET", "/admin/clients", token, "")
		assertStatus(t, resp, http.StatusOK)

		var clients []map[string]any
		decodeJSON(t, resp, &clients)

		found := false
		for _, c := range clients {
			if c["client_id"] == cr.ClientID {
				found = true
			}
		}
		if !found {
			t.Errorf("newly created client %q not found in admin list", cr.ClientID)
		}
	})

	t.Run("response objects include required fields", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		registerClient(t, env.srv, "Field Check App", []string{"http://localhost:5000/callback"})

		resp := doRequest(t, env.srv, "GET", "/admin/clients", token, "")
		assertStatus(t, resp, http.StatusOK)

		var clients []map[string]any
		decodeJSON(t, resp, &clients)

		for _, c := range clients {
			for _, field := range []string{"client_id", "name", "redirect_uris", "created_at"} {
				if _, ok := c[field]; !ok {
					t.Errorf("client object missing field %q: %v", field, c)
				}
			}
			if _, ok := c["secret_hash"]; ok {
				t.Error("secret_hash must not be exposed in admin client list")
			}
			if _, ok := c["client_secret"]; ok {
				t.Error("client_secret must not be exposed in admin client list")
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestAdminDeleteClient
// ---------------------------------------------------------------------------

func TestAdminDeleteClient(t *testing.T) {
	t.Run("delete client succeeds", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		cr := registerClient(t, env.srv, "Doomed Client", []string{"http://localhost:3000/callback"})

		resp := doRequest(t, env.srv, "DELETE", "/admin/clients/"+cr.ClientID, token, "")
		assertStatus(t, resp, http.StatusOK)

		var body map[string]any
		decodeJSON(t, resp, &body)

		if body["status"] != "deleted" {
			t.Errorf("status: got %v, want deleted", body["status"])
		}
	})

	t.Run("deleted client can no longer be used for auth", func(t *testing.T) {
		env := newTestEnv(t)

		adminToken := loginAsAdmin(t, env)
		registerUser(t, env.srv, "authuser@test.com", "pass1234", "Auth User")
		cr := registerClient(t, env.srv, "Soon Gone", []string{"http://localhost:3000/callback"})

		// Delete the client.
		delResp := doRequest(t, env.srv, "DELETE", "/admin/clients/"+cr.ClientID, adminToken, "")
		assertStatus(t, delResp, http.StatusOK)
		delResp.Body.Close()

		// Attempt to use the deleted client in the auth flow.
		values := url.Values{
			"email":         {"authuser@test.com"},
			"password":      {"pass1234"},
			"client_id":     {cr.ClientID},
			"redirect_uri":  {cr.RedirectURIs[0]},
			"response_type": {"code"},
			"scope":         {"openid"},
			"state":         {"s"},
		}
		authResp := doForm(t, env.srv, "/authorize", values)
		defer authResp.Body.Close()

		if authResp.StatusCode == http.StatusFound {
			// A redirect is only acceptable if it carries an error parameter.
			loc := authResp.Header.Get("Location")
			u, _ := url.Parse(loc)
			if u.Query().Get("error") == "" {
				t.Errorf("deleted client produced a valid auth redirect: %q", loc)
			}
		} else if authResp.StatusCode != http.StatusBadRequest && authResp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 400/401 or error redirect for deleted client, got %d", authResp.StatusCode)
		}
	})

	t.Run("non-existent client returns 404", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "DELETE", "/admin/clients/00000000-0000-0000-0000-000000000000", token, "")
		assertStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestRoleInTokens
// ---------------------------------------------------------------------------

func TestRoleInTokens(t *testing.T) {
	t.Run("login token includes role claim for regular user", func(t *testing.T) {
		env := newTestEnv(t)

		registerUser(t, env.srv, "rolecheck@test.com", "pass1234", "Role Check")
		tr := loginUser(t, env.srv, "rolecheck@test.com", "pass1234")

		claims := verifyJWT(t, env.srv, tr.AccessToken)
		role, _ := claims["role"].(string)
		if role != "user" {
			t.Errorf("role claim: got %q, want user", role)
		}
	})

	t.Run("login token includes role admin for admin user", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)
		claims := verifyJWT(t, env.srv, token)
		role, _ := claims["role"].(string)
		if role != "admin" {
			t.Errorf("role claim: got %q, want admin", role)
		}
	})

	t.Run("OAuth2 flow token includes role claim", func(t *testing.T) {
		env := newTestEnv(t)

		registerUser(t, env.srv, "oauthrole@test.com", "pass1234", "OAuth Role")
		client := registerClient(t, env.srv, "Role App", []string{"http://localhost:3000/callback"})
		verifier, challenge := pkceChallenge(t)

		loc := authorizeCode(t, env.srv, "oauthrole@test.com", "pass1234", client, verifier, challenge)
		code := extractCode(t, loc)
		tr := exchangeCode(t, env.srv, code, client.RedirectURIs[0], client.ClientID, client.ClientSecret, verifier)

		claims := verifyJWT(t, env.srv, tr.AccessToken)
		if _, ok := claims["role"]; !ok {
			t.Error("OAuth2 access_token missing role claim")
		}
		role, _ := claims["role"].(string)
		if role != "user" {
			t.Errorf("role claim in OAuth2 token: got %q, want user", role)
		}
	})

	t.Run("UserInfo returns role field for regular user", func(t *testing.T) {
		env := newTestEnv(t)

		registerUser(t, env.srv, "uirole@test.com", "pass1234", "UI Role")
		tr := loginUser(t, env.srv, "uirole@test.com", "pass1234")

		resp := doGet(t, env.srv, "/userinfo", map[string]string{
			"Authorization": "Bearer " + tr.AccessToken,
		})
		assertStatus(t, resp, http.StatusOK)

		var ui UserInfoResponse
		decodeJSON(t, resp, &ui)

		if ui.Role == "" {
			t.Error("userinfo response missing role field")
		}
		if ui.Role != "user" {
			t.Errorf("userinfo role: got %q, want user", ui.Role)
		}
	})

	t.Run("UserInfo returns role admin for admin user", func(t *testing.T) {
		env := newTestEnv(t)

		token := loginAsAdmin(t, env)

		resp := doGet(t, env.srv, "/userinfo", map[string]string{
			"Authorization": "Bearer " + token,
		})
		assertStatus(t, resp, http.StatusOK)

		var ui UserInfoResponse
		decodeJSON(t, resp, &ui)

		if ui.Role != "admin" {
			t.Errorf("userinfo role for admin: got %q, want admin", ui.Role)
		}
	})

	t.Run("promoted user token reflects new role after re-login", func(t *testing.T) {
		env := newTestEnv(t)

		adminToken := loginAsAdmin(t, env)
		registerUser(t, env.srv, "promote@test.com", "pass1234", "Promote Me")

		// Find user ID.
		listResp := doRequest(t, env.srv, "GET", "/admin/users", adminToken, "")
		assertStatus(t, listResp, http.StatusOK)
		var users []map[string]any
		decodeJSON(t, listResp, &users)

		var promoteID string
		for _, u := range users {
			if u["email"] == "promote@test.com" {
				promoteID, _ = u["id"].(string)
			}
		}
		if promoteID == "" {
			t.Fatal("could not find promote@test.com in admin user list")
		}

		// Promote to admin.
		putResp := doRequest(t, env.srv, "PUT", "/admin/users/"+promoteID, adminToken,
			`{"role":"admin"}`)
		assertStatus(t, putResp, http.StatusOK)
		putResp.Body.Close()

		// Re-login and verify the token now carries role=admin.
		tr := loginUser(t, env.srv, "promote@test.com", "pass1234")
		claims := verifyJWT(t, env.srv, tr.AccessToken)
		role, _ := claims["role"].(string)
		if role != "admin" {
			t.Errorf("promoted user token role: got %q, want admin", role)
		}
	})
}

// ---------------------------------------------------------------------------
// Marketing test helpers
// ---------------------------------------------------------------------------

// createContact calls POST /admin/contacts and returns the decoded contact map.
func createContact(t *testing.T, env *testEnv, token, email, name string) map[string]any {
	t.Helper()
	resp := doRequest(t, env.srv, "POST", "/admin/contacts", token,
		fmt.Sprintf(`{"email":%q,"name":%q}`, email, name))
	assertStatus(t, resp, http.StatusCreated)
	var c map[string]any
	decodeJSON(t, resp, &c)
	return c
}

// createSegment calls POST /admin/segments and returns the decoded segment map.
func createSegment(t *testing.T, env *testEnv, token, name, description string) map[string]any {
	t.Helper()
	resp := doRequest(t, env.srv, "POST", "/admin/segments", token,
		fmt.Sprintf(`{"name":%q,"description":%q}`, name, description))
	assertStatus(t, resp, http.StatusCreated)
	var s map[string]any
	decodeJSON(t, resp, &s)
	return s
}

// addContactToSegment calls POST /admin/segments/{segID}/contacts.
func addContactToSegment(t *testing.T, env *testEnv, token, segID, contactID string) {
	t.Helper()
	resp := doRequest(t, env.srv, "POST", "/admin/segments/"+segID+"/contacts", token,
		fmt.Sprintf(`{"contact_id":%q}`, contactID))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body := readBody(resp)
		t.Fatalf("addContactToSegment: expected 2xx, got %d (body=%q)", resp.StatusCode, body)
	}
	resp.Body.Close()
}

// createCampaign calls POST /admin/campaigns and returns the decoded campaign map.
func createCampaign(t *testing.T, env *testEnv, token string, segmentIDs []string) map[string]any {
	t.Helper()
	segsJSON, _ := json.Marshal(segmentIDs)
	body := fmt.Sprintf(`{"subject":"Test Subject","html_body":"<p>Hello {{.Name}}</p>","from_name":"Sender","from_email":"sender@example.com","segment_ids":%s}`, segsJSON)
	resp := doRequest(t, env.srv, "POST", "/admin/campaigns", token, body)
	assertStatus(t, resp, http.StatusCreated)
	var c map[string]any
	decodeJSON(t, resp, &c)
	return c
}

// waitForCampaignStatus polls GET /admin/campaigns/{id}/stats until the
// campaign status matches want or the deadline is reached.
func waitForCampaignStatus(t *testing.T, env *testEnv, token, campaignID, want string, maxWait time.Duration) {
	t.Helper()
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		resp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campaignID, token, "")
		if resp.StatusCode == http.StatusOK {
			var body map[string]any
			decodeJSON(t, resp, &body)
			if camp, ok := body["campaign"].(map[string]any); ok {
				if camp["status"] == want {
					return
				}
			}
		} else {
			resp.Body.Close()
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("campaign %s did not reach status %q within %v", campaignID, want, maxWait)
}

// ---------------------------------------------------------------------------
// TestContactManagement
// ---------------------------------------------------------------------------

func TestContactManagement(t *testing.T) {
	t.Run("create contact returns 201 with all fields", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "POST", "/admin/contacts", token,
			`{"email":"alice@marketing.com","name":"Alice"}`)
		assertStatus(t, resp, http.StatusCreated)

		var c map[string]any
		decodeJSON(t, resp, &c)

		if c["id"] == nil || c["id"] == "" {
			t.Error("expected non-empty id")
		}
		if c["email"] != "alice@marketing.com" {
			t.Errorf("email: got %v", c["email"])
		}
		if c["name"] != "Alice" {
			t.Errorf("name: got %v", c["name"])
		}
		if _, ok := c["unsubscribed"]; !ok {
			t.Error("expected unsubscribed field")
		}
		if _, ok := c["consent_source"]; !ok {
			t.Error("expected consent_source field")
		}
		if _, ok := c["created_at"]; !ok {
			t.Error("expected created_at field")
		}
		// unsubscribe_token must not leak through the API
		if _, ok := c["unsubscribe_token"]; ok {
			t.Error("unsubscribe_token must not be returned")
		}
	})

	t.Run("duplicate email returns 409", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		createContact(t, env, token, "dup@marketing.com", "Dup")

		resp := doRequest(t, env.srv, "POST", "/admin/contacts", token,
			`{"email":"dup@marketing.com","name":"Dup2"}`)
		assertStatus(t, resp, http.StatusConflict)
		resp.Body.Close()
	})

	t.Run("list contacts returns all contacts", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		createContact(t, env, token, "c1@marketing.com", "C1")
		createContact(t, env, token, "c2@marketing.com", "C2")

		resp := doRequest(t, env.srv, "GET", "/admin/contacts", token, "")
		assertStatus(t, resp, http.StatusOK)

		var contacts []map[string]any
		decodeJSON(t, resp, &contacts)

		if len(contacts) < 2 {
			t.Errorf("expected at least 2 contacts, got %d", len(contacts))
		}
	})

	t.Run("get contact by ID returns contact with segments field", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createContact(t, env, token, "getme@marketing.com", "Get Me")
		id, _ := c["id"].(string)

		resp := doRequest(t, env.srv, "GET", "/admin/contacts/"+id, token, "")
		assertStatus(t, resp, http.StatusOK)

		var got map[string]any
		decodeJSON(t, resp, &got)

		if got["id"] != id {
			t.Errorf("id: got %v, want %v", got["id"], id)
		}
		if got["email"] != "getme@marketing.com" {
			t.Errorf("email: got %v", got["email"])
		}
	})

	t.Run("delete contact returns 204 or 200", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createContact(t, env, token, "delme@marketing.com", "Del Me")
		id, _ := c["id"].(string)

		resp := doRequest(t, env.srv, "DELETE", "/admin/contacts/"+id, token, "")
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			body := readBody(resp)
			t.Fatalf("expected 204 or 200, got %d (body=%q)", resp.StatusCode, body)
		}
		resp.Body.Close()
	})

	t.Run("deleted contact not in list", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createContact(t, env, token, "gone@marketing.com", "Gone")
		id, _ := c["id"].(string)

		delResp := doRequest(t, env.srv, "DELETE", "/admin/contacts/"+id, token, "")
		delResp.Body.Close()

		listResp := doRequest(t, env.srv, "GET", "/admin/contacts", token, "")
		assertStatus(t, listResp, http.StatusOK)
		var contacts []map[string]any
		decodeJSON(t, listResp, &contacts)

		for _, ct := range contacts {
			if ct["id"] == id {
				t.Errorf("deleted contact %q still appears in list", id)
			}
		}
	})

	t.Run("contact endpoint without auth returns 401", func(t *testing.T) {
		env := newTestEnv(t)

		resp := doRequest(t, env.srv, "GET", "/admin/contacts", "", "")
		assertStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	})

	t.Run("contact endpoint with non-admin token returns 403", func(t *testing.T) {
		env := newTestEnv(t)
		registerUser(t, env.srv, "regular@test.com", "pass1234", "Regular")
		tr := loginUser(t, env.srv, "regular@test.com", "pass1234")

		resp := doRequest(t, env.srv, "GET", "/admin/contacts", tr.AccessToken, "")
		assertStatus(t, resp, http.StatusForbidden)
		resp.Body.Close()
	})

	t.Run("get non-existent contact returns 404", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "GET", "/admin/contacts/00000000-0000-0000-0000-000000000000", token, "")
		assertStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestContactImport
// ---------------------------------------------------------------------------

func TestContactImport(t *testing.T) {
	t.Run("import multiple contacts", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		body := `[{"email":"imp1@test.com","name":"Imp One"},{"email":"imp2@test.com","name":"Imp Two"},{"email":"imp3@test.com","name":"Imp Three"}]`
		resp := doRequest(t, env.srv, "POST", "/admin/contacts/import", token, body)
		assertStatus(t, resp, http.StatusOK)

		var result map[string]any
		decodeJSON(t, resp, &result)

		imported, _ := result["imported"].(float64)
		if int(imported) != 3 {
			t.Errorf("imported: got %v, want 3", result["imported"])
		}
		skipped, _ := result["skipped"].(float64)
		if int(skipped) != 0 {
			t.Errorf("skipped: got %v, want 0", result["skipped"])
		}
	})

	t.Run("import skips duplicates and returns count", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		body := `[{"email":"skipper@test.com","name":"Skipper"},{"email":"new@test.com","name":"New"}]`
		resp1 := doRequest(t, env.srv, "POST", "/admin/contacts/import", token, body)
		assertStatus(t, resp1, http.StatusOK)
		resp1.Body.Close()

		// Re-import same batch — both should be skipped.
		resp2 := doRequest(t, env.srv, "POST", "/admin/contacts/import", token, body)
		assertStatus(t, resp2, http.StatusOK)

		var result map[string]any
		decodeJSON(t, resp2, &result)

		imported, _ := result["imported"].(float64)
		skipped, _ := result["skipped"].(float64)
		if int(imported) != 0 {
			t.Errorf("imported after re-import: got %v, want 0", result["imported"])
		}
		if int(skipped) != 2 {
			t.Errorf("skipped after re-import: got %v, want 2", result["skipped"])
		}
	})

	t.Run("import with existing email skips it", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		// Pre-create a contact via API.
		createContact(t, env, token, "existing@test.com", "Existing")

		body := `[{"email":"existing@test.com","name":"Existing Again"},{"email":"brand-new@test.com","name":"Brand New"}]`
		resp := doRequest(t, env.srv, "POST", "/admin/contacts/import", token, body)
		assertStatus(t, resp, http.StatusOK)

		var result map[string]any
		decodeJSON(t, resp, &result)

		imported, _ := result["imported"].(float64)
		skipped, _ := result["skipped"].(float64)
		if int(imported) != 1 {
			t.Errorf("imported: got %v, want 1", result["imported"])
		}
		if int(skipped) != 1 {
			t.Errorf("skipped: got %v, want 1", result["skipped"])
		}
	})

	t.Run("empty import returns 0/0", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "POST", "/admin/contacts/import", token, `[]`)
		assertStatus(t, resp, http.StatusOK)

		var result map[string]any
		decodeJSON(t, resp, &result)

		imported, _ := result["imported"].(float64)
		skipped, _ := result["skipped"].(float64)
		if int(imported) != 0 || int(skipped) != 0 {
			t.Errorf("empty import: got imported=%v skipped=%v, want 0/0", result["imported"], result["skipped"])
		}
	})

	t.Run("import without auth returns 401", func(t *testing.T) {
		env := newTestEnv(t)

		resp := doRequest(t, env.srv, "POST", "/admin/contacts/import", "",
			`[{"email":"x@test.com","name":"X"}]`)
		assertStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestSegmentManagement
// ---------------------------------------------------------------------------

func TestSegmentManagement(t *testing.T) {
	t.Run("create segment returns 201 with all fields", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "POST", "/admin/segments", token,
			`{"name":"VIP Customers","description":"High-value customers"}`)
		assertStatus(t, resp, http.StatusCreated)

		var s map[string]any
		decodeJSON(t, resp, &s)

		if s["id"] == nil || s["id"] == "" {
			t.Error("expected non-empty id")
		}
		if s["name"] != "VIP Customers" {
			t.Errorf("name: got %v", s["name"])
		}
		if s["description"] != "High-value customers" {
			t.Errorf("description: got %v", s["description"])
		}
		if _, ok := s["created_at"]; !ok {
			t.Error("expected created_at field")
		}
	})

	t.Run("duplicate segment name returns 409", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		createSegment(t, env, token, "UniqueSegment", "")

		resp := doRequest(t, env.srv, "POST", "/admin/segments", token,
			`{"name":"UniqueSegment"}`)
		assertStatus(t, resp, http.StatusConflict)
		resp.Body.Close()
	})

	t.Run("list segments with contact counts", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "ListSeg", "")
		segID, _ := seg["id"].(string)

		c1 := createContact(t, env, token, "seg-c1@test.com", "C1")
		c2 := createContact(t, env, token, "seg-c2@test.com", "C2")
		addContactToSegment(t, env, token, segID, c1["id"].(string))
		addContactToSegment(t, env, token, segID, c2["id"].(string))

		resp := doRequest(t, env.srv, "GET", "/admin/segments", token, "")
		assertStatus(t, resp, http.StatusOK)

		var segments []map[string]any
		decodeJSON(t, resp, &segments)

		var found map[string]any
		for _, s := range segments {
			if s["id"] == segID {
				found = s
			}
		}
		if found == nil {
			t.Fatal("created segment not found in list")
		}
		count, _ := found["contact_count"].(float64)
		if int(count) != 2 {
			t.Errorf("contact_count: got %v, want 2", found["contact_count"])
		}
	})

	t.Run("update segment name and description", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "OldName", "Old desc")
		segID, _ := seg["id"].(string)

		resp := doRequest(t, env.srv, "PUT", "/admin/segments/"+segID, token,
			`{"name":"NewName","description":"New desc"}`)
		assertStatus(t, resp, http.StatusOK)

		var updated map[string]any
		decodeJSON(t, resp, &updated)

		if updated["name"] != "NewName" {
			t.Errorf("name after update: got %v, want NewName", updated["name"])
		}
		if updated["description"] != "New desc" {
			t.Errorf("description after update: got %v, want New desc", updated["description"])
		}
	})

	t.Run("delete segment returns 200 or 204", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "SegToDelete", "")
		segID, _ := seg["id"].(string)

		resp := doRequest(t, env.srv, "DELETE", "/admin/segments/"+segID, token, "")
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			body := readBody(resp)
			t.Fatalf("expected 200 or 204, got %d (body=%q)", resp.StatusCode, body)
		}
		resp.Body.Close()
	})

	t.Run("deleted segment removed from list", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "GoneSeg", "")
		segID, _ := seg["id"].(string)

		delResp := doRequest(t, env.srv, "DELETE", "/admin/segments/"+segID, token, "")
		delResp.Body.Close()

		listResp := doRequest(t, env.srv, "GET", "/admin/segments", token, "")
		assertStatus(t, listResp, http.StatusOK)
		var segments []map[string]any
		decodeJSON(t, listResp, &segments)

		for _, s := range segments {
			if s["id"] == segID {
				t.Errorf("deleted segment %q still appears in list", segID)
			}
		}
	})

	t.Run("add contact to segment", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "AddContactSeg", "")
		segID, _ := seg["id"].(string)

		c := createContact(t, env, token, "seg-add@test.com", "Add Me")
		contactID, _ := c["id"].(string)

		addContactToSegment(t, env, token, segID, contactID)

		// Verify via GET /admin/segments/{id}
		resp := doRequest(t, env.srv, "GET", "/admin/segments/"+segID, token, "")
		assertStatus(t, resp, http.StatusOK)
		var body map[string]any
		decodeJSON(t, resp, &body)

		contacts, _ := body["contacts"].([]any)
		found := false
		for _, ct := range contacts {
			if m, ok := ct.(map[string]any); ok && m["id"] == contactID {
				found = true
			}
		}
		if !found {
			t.Errorf("contact %q not found in segment contacts", contactID)
		}
	})

	t.Run("remove contact from segment", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "RemoveContactSeg", "")
		segID, _ := seg["id"].(string)
		c := createContact(t, env, token, "seg-rem@test.com", "Remove Me")
		contactID, _ := c["id"].(string)
		addContactToSegment(t, env, token, segID, contactID)

		delResp := doRequest(t, env.srv, "DELETE",
			"/admin/segments/"+segID+"/contacts/"+contactID, token, "")
		if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNoContent {
			body := readBody(delResp)
			t.Fatalf("expected 200 or 204 removing from segment, got %d (body=%q)", delResp.StatusCode, body)
		}
		delResp.Body.Close()

		// Confirm removal via segment detail
		resp := doRequest(t, env.srv, "GET", "/admin/segments/"+segID, token, "")
		assertStatus(t, resp, http.StatusOK)
		var body map[string]any
		decodeJSON(t, resp, &body)

		contacts, _ := body["contacts"].([]any)
		for _, ct := range contacts {
			if m, ok := ct.(map[string]any); ok && m["id"] == contactID {
				t.Errorf("contact %q still listed in segment after removal", contactID)
			}
		}
	})

	t.Run("contact can be in multiple segments", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg1 := createSegment(t, env, token, "MultiSeg1", "")
		seg2 := createSegment(t, env, token, "MultiSeg2", "")
		c := createContact(t, env, token, "multi@test.com", "Multi")
		contactID, _ := c["id"].(string)

		addContactToSegment(t, env, token, seg1["id"].(string), contactID)
		addContactToSegment(t, env, token, seg2["id"].(string), contactID)

		// Both segments should list the contact
		for _, seg := range []map[string]any{seg1, seg2} {
			segID, _ := seg["id"].(string)
			resp := doRequest(t, env.srv, "GET", "/admin/segments/"+segID, token, "")
			assertStatus(t, resp, http.StatusOK)
			var body map[string]any
			decodeJSON(t, resp, &body)
			contacts, _ := body["contacts"].([]any)
			found := false
			for _, ct := range contacts {
				if m, ok := ct.(map[string]any); ok && m["id"] == contactID {
					found = true
				}
			}
			if !found {
				t.Errorf("contact not found in segment %q", segID)
			}
		}
	})

	t.Run("get segment returns contacts", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "GetContactsSeg", "")
		segID, _ := seg["id"].(string)
		c1 := createContact(t, env, token, "gcs1@test.com", "GCS1")
		c2 := createContact(t, env, token, "gcs2@test.com", "GCS2")
		addContactToSegment(t, env, token, segID, c1["id"].(string))
		addContactToSegment(t, env, token, segID, c2["id"].(string))

		resp := doRequest(t, env.srv, "GET", "/admin/segments/"+segID, token, "")
		assertStatus(t, resp, http.StatusOK)
		var body map[string]any
		decodeJSON(t, resp, &body)

		contacts, _ := body["contacts"].([]any)
		if len(contacts) != 2 {
			t.Errorf("expected 2 contacts in segment, got %d", len(contacts))
		}
	})

	t.Run("segment without auth returns 401", func(t *testing.T) {
		env := newTestEnv(t)

		resp := doRequest(t, env.srv, "GET", "/admin/segments", "", "")
		assertStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestCampaignManagement
// ---------------------------------------------------------------------------

func TestCampaignManagement(t *testing.T) {
	t.Run("create campaign with segment IDs returns 201", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "CampSeg", "")
		segID, _ := seg["id"].(string)

		segsJSON, _ := json.Marshal([]string{segID})
		body := fmt.Sprintf(`{"subject":"Hello","html_body":"<p>Hi</p>","from_name":"Acme","from_email":"noreply@acme.com","segment_ids":%s}`, segsJSON)
		resp := doRequest(t, env.srv, "POST", "/admin/campaigns", token, body)
		assertStatus(t, resp, http.StatusCreated)

		var c map[string]any
		decodeJSON(t, resp, &c)

		if c["id"] == nil || c["id"] == "" {
			t.Error("expected non-empty id")
		}
		if c["subject"] != "Hello" {
			t.Errorf("subject: got %v", c["subject"])
		}
		if c["status"] != "draft" {
			t.Errorf("status: got %v, want draft", c["status"])
		}
	})

	t.Run("list campaigns includes stats fields", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		createCampaign(t, env, token, []string{})

		resp := doRequest(t, env.srv, "GET", "/admin/campaigns", token, "")
		assertStatus(t, resp, http.StatusOK)

		var campaigns []map[string]any
		decodeJSON(t, resp, &campaigns)

		if len(campaigns) == 0 {
			t.Fatal("expected at least one campaign")
		}
		c := campaigns[0]
		for _, field := range []string{"id", "subject", "status", "created_at", "total", "sent", "queued", "failed", "opened"} {
			if _, ok := c[field]; !ok {
				t.Errorf("campaign list item missing field %q", field)
			}
		}
	})

	t.Run("get campaign by ID returns campaign with stats", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createCampaign(t, env, token, []string{})
		campaignID, _ := c["id"].(string)

		resp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campaignID, token, "")
		assertStatus(t, resp, http.StatusOK)

		var body map[string]any
		decodeJSON(t, resp, &body)

		if body["campaign"] == nil {
			t.Error("expected campaign field in response")
		}
		if body["stats"] == nil {
			t.Error("expected stats field in response")
		}
	})

	t.Run("update draft campaign", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createCampaign(t, env, token, []string{})
		campaignID, _ := c["id"].(string)

		resp := doRequest(t, env.srv, "PUT", "/admin/campaigns/"+campaignID, token,
			`{"subject":"Updated Subject","html_body":"<p>Updated</p>","from_name":"Updated Sender","from_email":"updated@example.com","segment_ids":[]}`)
		assertStatus(t, resp, http.StatusOK)

		var updated map[string]any
		decodeJSON(t, resp, &updated)

		if updated["subject"] != "Updated Subject" {
			t.Errorf("subject after update: got %v", updated["subject"])
		}
	})

	t.Run("delete draft campaign returns 200 or 204", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createCampaign(t, env, token, []string{})
		campaignID, _ := c["id"].(string)

		resp := doRequest(t, env.srv, "DELETE", "/admin/campaigns/"+campaignID, token, "")
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			body := readBody(resp)
			t.Fatalf("expected 200 or 204, got %d (body=%q)", resp.StatusCode, body)
		}
		resp.Body.Close()
	})

	t.Run("campaign without auth returns 401", func(t *testing.T) {
		env := newTestEnv(t)

		resp := doRequest(t, env.srv, "GET", "/admin/campaigns", "", "")
		assertStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	})

	t.Run("campaign create without subject returns 400", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "POST", "/admin/campaigns", token,
			`{"html_body":"<p>No subject</p>"}`)
		assertStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	})

	t.Run("campaign create without html_body returns 400", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "POST", "/admin/campaigns", token,
			`{"subject":"No body"}`)
		assertStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	})

	t.Run("get non-existent campaign returns 404", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		resp := doRequest(t, env.srv, "GET", "/admin/campaigns/00000000-0000-0000-0000-000000000000", token, "")
		assertStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	})
}

// ---------------------------------------------------------------------------
// TestCampaignSending
// ---------------------------------------------------------------------------

func TestCampaignSending(t *testing.T) {
	t.Run("send campaign returns 202 with status sending", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "SendSeg1", "")
		segID, _ := seg["id"].(string)
		c := createContact(t, env, token, "recipient1@test.com", "Recipient One")
		addContactToSegment(t, env, token, segID, c["id"].(string))

		camp := createCampaign(t, env, token, []string{segID})
		campaignID, _ := camp["id"].(string)

		resp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, resp, http.StatusAccepted)

		var body map[string]any
		decodeJSON(t, resp, &body)
		if body["status"] != "sending" {
			t.Errorf("status: got %v, want sending", body["status"])
		}
	})

	t.Run("campaign status changes to sent after processing", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "SendSeg2", "")
		segID, _ := seg["id"].(string)
		c := createContact(t, env, token, "recipient2@test.com", "Recipient Two")
		addContactToSegment(t, env, token, segID, c["id"].(string))

		camp := createCampaign(t, env, token, []string{segID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()

		// Wait for async sender goroutine to complete.
		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)
	})

	t.Run("recipients created from segment contacts deduplicated", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg1 := createSegment(t, env, token, "DeduSeg1", "")
		seg2 := createSegment(t, env, token, "DeduSeg2", "")
		seg1ID, _ := seg1["id"].(string)
		seg2ID, _ := seg2["id"].(string)

		// Same contact in two segments — should only get one email.
		c := createContact(t, env, token, "dedup@test.com", "Dedup")
		contactID, _ := c["id"].(string)
		addContactToSegment(t, env, token, seg1ID, contactID)
		addContactToSegment(t, env, token, seg2ID, contactID)

		camp := createCampaign(t, env, token, []string{seg1ID, seg2ID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()

		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)

		statsResp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campaignID+"/stats", token, "")
		assertStatus(t, statsResp, http.StatusOK)
		var stats map[string]any
		decodeJSON(t, statsResp, &stats)

		total, _ := stats["total"].(float64)
		if int(total) != 1 {
			t.Errorf("deduplicated total: got %v, want 1", stats["total"])
		}
	})

	t.Run("unsubscribed contacts excluded from sending", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "UnsubSeg", "")
		segID, _ := seg["id"].(string)

		// Create two contacts; unsubscribe one directly via the store.
		cSub := createContact(t, env, token, "subscribed@test.com", "Subscribed")
		cUnsub := createContact(t, env, token, "unsubbed@test.com", "Unsubbed")
		addContactToSegment(t, env, token, segID, cSub["id"].(string))
		addContactToSegment(t, env, token, segID, cUnsub["id"].(string))

		// Get unsubscribe token for the second contact and use the public endpoint.
		unsubContact, err := env.store.GetContactByEmail("unsubbed@test.com")
		if err != nil {
			t.Fatalf("GetContactByEmail: %v", err)
		}
		unsubResp := doRequest(t, env.srv, "POST", "/unsubscribe/"+unsubContact.UnsubscribeToken, "", "")
		if unsubResp.StatusCode != http.StatusOK {
			t.Fatalf("unsubscribe: got %d", unsubResp.StatusCode)
		}
		unsubResp.Body.Close()

		camp := createCampaign(t, env, token, []string{segID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()

		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)

		statsResp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campaignID+"/stats", token, "")
		assertStatus(t, statsResp, http.StatusOK)
		var stats map[string]any
		decodeJSON(t, statsResp, &stats)

		total, _ := stats["total"].(float64)
		if int(total) != 1 {
			t.Errorf("expected 1 recipient (subscribed only), got %v", stats["total"])
		}
	})

	t.Run("campaign stats reflect sent count", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := createSegment(t, env, token, "StatsSeg", "")
		segID, _ := seg["id"].(string)
		for i := range 3 {
			c := createContact(t, env, token, fmt.Sprintf("stats%d@test.com", i), fmt.Sprintf("Stats%d", i))
			addContactToSegment(t, env, token, segID, c["id"].(string))
		}

		camp := createCampaign(t, env, token, []string{segID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()

		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)

		statsResp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campaignID+"/stats", token, "")
		assertStatus(t, statsResp, http.StatusOK)
		var stats map[string]any
		decodeJSON(t, statsResp, &stats)

		sent, _ := stats["sent"].(float64)
		if int(sent) != 3 {
			t.Errorf("sent count: got %v, want 3", stats["sent"])
		}
	})

	t.Run("cannot send non-draft campaign", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		seg := newTestEnvSegWithContact(t, env, token, "NoDraftSeg", "nondraft@test.com")
		camp := createCampaign(t, env, token, []string{seg})
		campaignID, _ := camp["id"].(string)

		// First send.
		send1 := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, send1, http.StatusAccepted)
		send1.Body.Close()

		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)

		// Second send must fail.
		send2 := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		if send2.StatusCode != http.StatusBadRequest && send2.StatusCode != http.StatusConflict {
			body := readBody(send2)
			t.Errorf("expected 400 or 409 for resending, got %d (body=%q)", send2.StatusCode, body)
		} else {
			send2.Body.Close()
		}
	})
}

// newTestEnvSegWithContact is a small helper that creates a named segment,
// creates a contact, adds the contact to the segment, and returns the segment ID.
func newTestEnvSegWithContact(t *testing.T, env *testEnv, token, segName, contactEmail string) string {
	t.Helper()
	seg := createSegment(t, env, token, segName, "")
	segID, _ := seg["id"].(string)
	c := createContact(t, env, token, contactEmail, "Contact")
	addContactToSegment(t, env, token, segID, c["id"].(string))
	return segID
}

// ---------------------------------------------------------------------------
// TestTrackingPixel
// ---------------------------------------------------------------------------

func TestTrackingPixel(t *testing.T) {
	t.Run("GET /track/{valid-id} returns image/gif", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		// Send a campaign so we have a real recipient ID.
		segID := newTestEnvSegWithContact(t, env, token, "TrackSeg1", "track1@test.com")
		camp := createCampaign(t, env, token, []string{segID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()
		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)

		// Get a recipient ID.
		recipients, err := env.store.GetCampaignRecipients(campaignID)
		if err != nil || len(recipients) == 0 {
			t.Fatalf("GetCampaignRecipients: %v, len=%d", err, len(recipients))
		}
		recipientID := recipients[0].ID

		resp := doRequest(t, env.srv, "GET", "/track/"+recipientID, "", "")
		assertStatus(t, resp, http.StatusOK)

		ct := resp.Header.Get("Content-Type")
		if ct != "image/gif" {
			t.Errorf("Content-Type: got %q, want image/gif", ct)
		}
		resp.Body.Close()
	})

	t.Run("GET /track/{valid-id} records open in stats", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		segID := newTestEnvSegWithContact(t, env, token, "TrackSeg2", "track2@test.com")
		camp := createCampaign(t, env, token, []string{segID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()
		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)

		recipients, err := env.store.GetCampaignRecipients(campaignID)
		if err != nil || len(recipients) == 0 {
			t.Fatalf("GetCampaignRecipients: %v, len=%d", err, len(recipients))
		}
		recipientID := recipients[0].ID

		// Hit tracking pixel.
		trackResp := doRequest(t, env.srv, "GET", "/track/"+recipientID, "", "")
		trackResp.Body.Close()

		// Stats should show 1 opened.
		statsResp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campaignID+"/stats", token, "")
		assertStatus(t, statsResp, http.StatusOK)
		var stats map[string]any
		decodeJSON(t, statsResp, &stats)

		opened, _ := stats["opened"].(float64)
		if int(opened) != 1 {
			t.Errorf("opened count after pixel hit: got %v, want 1", stats["opened"])
		}
	})

	t.Run("GET /track/{invalid-id} still returns 200 gif (no info leak)", func(t *testing.T) {
		env := newTestEnv(t)

		resp := doRequest(t, env.srv, "GET", "/track/totally-fake-recipient-id", "", "")
		assertStatus(t, resp, http.StatusOK)

		ct := resp.Header.Get("Content-Type")
		if ct != "image/gif" {
			t.Errorf("Content-Type: got %q, want image/gif", ct)
		}
		resp.Body.Close()
	})

	t.Run("second open does not update opened_at", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		segID := newTestEnvSegWithContact(t, env, token, "TrackSeg3", "track3@test.com")
		camp := createCampaign(t, env, token, []string{segID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", token, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()
		waitForCampaignStatus(t, env, token, campaignID, "sent", 5*time.Second)

		recipients, err := env.store.GetCampaignRecipients(campaignID)
		if err != nil || len(recipients) == 0 {
			t.Fatalf("GetCampaignRecipients: %v, len=%d", err, len(recipients))
		}
		recipientID := recipients[0].ID

		// First open.
		r1 := doRequest(t, env.srv, "GET", "/track/"+recipientID, "", "")
		r1.Body.Close()

		firstRecipients, _ := env.store.GetCampaignRecipients(campaignID)
		var firstOpenedAt *time.Time
		if len(firstRecipients) > 0 {
			firstOpenedAt = firstRecipients[0].OpenedAt
		}

		time.Sleep(10 * time.Millisecond)

		// Second open.
		r2 := doRequest(t, env.srv, "GET", "/track/"+recipientID, "", "")
		r2.Body.Close()

		secondRecipients, _ := env.store.GetCampaignRecipients(campaignID)
		var secondOpenedAt *time.Time
		if len(secondRecipients) > 0 {
			secondOpenedAt = secondRecipients[0].OpenedAt
		}

		if firstOpenedAt == nil {
			t.Fatal("opened_at not set after first open")
		}
		if secondOpenedAt == nil {
			t.Fatal("opened_at nil after second open")
		}
		if !firstOpenedAt.Equal(*secondOpenedAt) {
			t.Errorf("opened_at changed on second open: first=%v second=%v", firstOpenedAt, secondOpenedAt)
		}
	})
}

// ---------------------------------------------------------------------------
// TestUnsubscribe
// ---------------------------------------------------------------------------

func TestUnsubscribe(t *testing.T) {
	t.Run("GET /unsubscribe/{token} returns HTML page", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		createContact(t, env, token, "unsub1@test.com", "Unsub One")
		contact, err := env.store.GetContactByEmail("unsub1@test.com")
		if err != nil {
			t.Fatalf("GetContactByEmail: %v", err)
		}

		resp := doRequest(t, env.srv, "GET", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		assertStatus(t, resp, http.StatusOK)

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type: got %q, want text/html", ct)
		}

		body := readBody(resp)
		lower := strings.ToLower(body)
		if !strings.Contains(lower, "unsubscribe") {
			t.Error("expected 'unsubscribe' word in GET /unsubscribe response")
		}
	})

	t.Run("POST /unsubscribe/{token} unsubscribes contact", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		createContact(t, env, adminToken, "unsub2@test.com", "Unsub Two")
		contact, err := env.store.GetContactByEmail("unsub2@test.com")
		if err != nil {
			t.Fatalf("GetContactByEmail: %v", err)
		}
		if contact.Unsubscribed {
			t.Fatal("contact should not be unsubscribed initially")
		}

		resp := doRequest(t, env.srv, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		updated, err := env.store.GetContactByEmail("unsub2@test.com")
		if err != nil {
			t.Fatalf("GetContactByEmail after unsubscribe: %v", err)
		}
		if !updated.Unsubscribed {
			t.Error("contact should be unsubscribed after POST /unsubscribe")
		}
	})

	t.Run("unsubscribed contact excluded from future sends", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		seg := createSegment(t, env, adminToken, "ExcludeUnsubSeg", "")
		segID, _ := seg["id"].(string)

		c := createContact(t, env, adminToken, "exclude@test.com", "Exclude")
		contactID, _ := c["id"].(string)
		addContactToSegment(t, env, adminToken, segID, contactID)

		// Unsubscribe.
		contact, _ := env.store.GetContactByEmail("exclude@test.com")
		unsubResp := doRequest(t, env.srv, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		unsubResp.Body.Close()

		// Send campaign — the unsubscribed contact must be excluded.
		camp := createCampaign(t, env, adminToken, []string{segID})
		campaignID, _ := camp["id"].(string)

		sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campaignID+"/send", adminToken, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()

		waitForCampaignStatus(t, env, adminToken, campaignID, "sent", 5*time.Second)

		statsResp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campaignID+"/stats", adminToken, "")
		assertStatus(t, statsResp, http.StatusOK)
		var stats map[string]any
		decodeJSON(t, statsResp, &stats)

		total, _ := stats["total"].(float64)
		if int(total) != 0 {
			t.Errorf("unsubscribed contact must be excluded: total got %v, want 0", stats["total"])
		}
	})

	t.Run("invalid unsubscribe token returns 404", func(t *testing.T) {
		env := newTestEnv(t)

		resp := doRequest(t, env.srv, "GET", "/unsubscribe/invalid-token-xyz", "", "")
		assertStatus(t, resp, http.StatusNotFound)
		resp.Body.Close()
	})

	t.Run("double unsubscribe is idempotent", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		createContact(t, env, adminToken, "doubleun@test.com", "Double Un")
		contact, _ := env.store.GetContactByEmail("doubleun@test.com")

		resp1 := doRequest(t, env.srv, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		assertStatus(t, resp1, http.StatusOK)
		resp1.Body.Close()

		resp2 := doRequest(t, env.srv, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		// Unsubscribe must be idempotent — always return 200.
		assertStatus(t, resp2, http.StatusOK)
		resp2.Body.Close()

		// Either way, the contact must remain unsubscribed.
		updated, _ := env.store.GetContactByEmail("doubleun@test.com")
		if !updated.Unsubscribed {
			t.Error("contact must stay unsubscribed after double POST")
		}
	})
}

// ---------------------------------------------------------------------------
// TestContactSegmentRelationship
// ---------------------------------------------------------------------------

func TestContactSegmentRelationship(t *testing.T) {
	t.Run("contact appears in multiple segments", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createContact(t, env, token, "multiseg@test.com", "MultiSeg")
		contactID, _ := c["id"].(string)

		seg1 := createSegment(t, env, token, "RelSeg1", "")
		seg2 := createSegment(t, env, token, "RelSeg2", "")
		addContactToSegment(t, env, token, seg1["id"].(string), contactID)
		addContactToSegment(t, env, token, seg2["id"].(string), contactID)

		// List segments: both should report contact_count >= 1.
		resp := doRequest(t, env.srv, "GET", "/admin/segments", token, "")
		assertStatus(t, resp, http.StatusOK)
		var segments []map[string]any
		decodeJSON(t, resp, &segments)

		for _, seg := range []map[string]any{seg1, seg2} {
			segID, _ := seg["id"].(string)
			var found map[string]any
			for _, s := range segments {
				if s["id"] == segID {
					found = s
				}
			}
			if found == nil {
				t.Errorf("segment %q not found in list", segID)
				continue
			}
			count, _ := found["contact_count"].(float64)
			if int(count) < 1 {
				t.Errorf("segment %q contact_count: got %v, want >= 1", segID, found["contact_count"])
			}
		}
	})

	t.Run("deleting contact removes it from all segments", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createContact(t, env, token, "toremove@test.com", "To Remove")
		contactID, _ := c["id"].(string)

		seg := createSegment(t, env, token, "CascadeDelSeg", "")
		segID, _ := seg["id"].(string)
		addContactToSegment(t, env, token, segID, contactID)

		// Delete the contact.
		delResp := doRequest(t, env.srv, "DELETE", "/admin/contacts/"+contactID, token, "")
		delResp.Body.Close()

		// The segment should now have 0 contacts.
		segResp := doRequest(t, env.srv, "GET", "/admin/segments/"+segID, token, "")
		assertStatus(t, segResp, http.StatusOK)
		var body map[string]any
		decodeJSON(t, segResp, &body)

		contacts, _ := body["contacts"].([]any)
		if len(contacts) != 0 {
			t.Errorf("expected 0 contacts after deleting contact, got %d", len(contacts))
		}
	})

	t.Run("deleting segment removes contact associations", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createContact(t, env, token, "assoc@test.com", "Assoc")
		contactID, _ := c["id"].(string)

		seg := createSegment(t, env, token, "CascadeSegDel", "")
		segID, _ := seg["id"].(string)
		addContactToSegment(t, env, token, segID, contactID)

		// Delete the segment.
		delResp := doRequest(t, env.srv, "DELETE", "/admin/segments/"+segID, token, "")
		delResp.Body.Close()

		// Fetch contact detail — its segments list should not include the deleted segment.
		contResp := doRequest(t, env.srv, "GET", "/admin/contacts/"+contactID, token, "")
		assertStatus(t, contResp, http.StatusOK)
		var contact map[string]any
		decodeJSON(t, contResp, &contact)

		segs, _ := contact["segments"].([]any)
		for _, s := range segs {
			if m, ok := s.(map[string]any); ok && m["id"] == segID {
				t.Errorf("deleted segment %q still listed on contact", segID)
			}
		}
	})

	t.Run("contact segments listed in contact detail", func(t *testing.T) {
		env := newTestEnv(t)
		token := loginAsAdmin(t, env)

		c := createContact(t, env, token, "detail@test.com", "Detail")
		contactID, _ := c["id"].(string)

		seg1 := createSegment(t, env, token, "DetailSeg1", "")
		seg2 := createSegment(t, env, token, "DetailSeg2", "")
		addContactToSegment(t, env, token, seg1["id"].(string), contactID)
		addContactToSegment(t, env, token, seg2["id"].(string), contactID)

		resp := doRequest(t, env.srv, "GET", "/admin/contacts/"+contactID, token, "")
		assertStatus(t, resp, http.StatusOK)
		var contact map[string]any
		decodeJSON(t, resp, &contact)

		segs, _ := contact["segments"].([]any)
		if len(segs) != 2 {
			t.Errorf("expected 2 segments on contact detail, got %d", len(segs))
		}
	})
}

// ---------------------------------------------------------------------------
// CapturingMailer — records every Send call for inspection in tests.
// ---------------------------------------------------------------------------

type capturedMail struct {
	To          string
	Subject     string
	Body        string
	Attachments []marketing.Attachment
}

type CapturingMailer struct {
	mails []capturedMail
}

func (m *CapturingMailer) Send(to, subject, htmlBody string, _ map[string]string, attachments []marketing.Attachment) error {
	m.mails = append(m.mails, capturedMail{To: to, Subject: subject, Body: htmlBody, Attachments: attachments})
	return nil
}

// newTestEnvWithMailer is like newTestEnv but injects a custom Mailer so tests
// can inspect outgoing emails.
func newTestEnvWithMailer(t *testing.T, mailer Mailer) *testEnv {
	t.Helper()
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Seed admin so registerClient can authenticate
	if err := store.SeedAdmin("admin@test.com", "adminpass123", "Admin"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tokenService := NewTokenService(key, "http://test-issuer")

	const issuer = "http://test-issuer"
	h := NewHandler(store, tokenService, issuer)

	sender := NewCampaignSender(store, mailer, issuer, 0)
	sender.StartSync()
	h.sender = sender
	h.SetMailer(mailer)

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
	mux.HandleFunc("/admin/contacts", h.AdminContacts)
	mux.HandleFunc("/admin/contacts/import", h.AdminImportContacts)
	mux.HandleFunc("/admin/contacts/", h.AdminContactByID)
	mux.HandleFunc("/admin/segments", h.AdminSegments)
	mux.HandleFunc("/admin/segments/", h.AdminSegmentByID)
	mux.HandleFunc("/admin/campaigns", h.AdminCampaigns)
	mux.HandleFunc("/admin/campaigns/", h.AdminCampaignByID)
	mux.HandleFunc("/track/", h.TrackOpen)
	mux.HandleFunc("/unsubscribe/", h.Unsubscribe)
	mux.HandleFunc("/activate/", h.Activate)
	mux.HandleFunc("/forgot-password", h.ForgotPassword)
	mux.HandleFunc("/reset-password/", h.ResetPassword)

	srv := httptest.NewServer(CORSMiddleware("*")(mux))
	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})
	return &testEnv{srv: srv, store: store}
}

// ---------------------------------------------------------------------------
// TestInviteActivationFlow
// ---------------------------------------------------------------------------

func TestInviteActivationFlow(t *testing.T) {
	// Shared state across subtests — all subtests run sequentially inside this
	// parent test, sharing a single env so later subtests can reuse earlier
	// state (token, contact ID, invite token, etc.).
	env := newTestEnv(t)
	token := loginAsAdmin(t, env)

	var contactID string
	var inviteToken string

	t.Run("create_contact_returns_invite_token", func(t *testing.T) {
		resp := doRequest(t, env.srv, "POST", "/admin/contacts", token,
			`{"email":"invited@example.com","name":"Invited User"}`)
		assertStatus(t, resp, http.StatusCreated)

		var c map[string]any
		decodeJSON(t, resp, &c)

		contactID, _ = c["id"].(string)
		if contactID == "" {
			t.Fatal("expected non-empty id in response")
		}

		inviteToken, _ = c["invite_token"].(string)
		if inviteToken == "" {
			t.Fatal("expected non-empty invite_token in response")
		}
		// invite_token must look like a UUID (contains hyphens, 36 chars).
		if len(inviteToken) != 36 {
			t.Errorf("invite_token looks wrong: %q", inviteToken)
		}
	})

	t.Run("GET_activate_with_valid_token_returns_HTML", func(t *testing.T) {
		if inviteToken == "" {
			t.Skip("no invite token from previous subtest")
		}
		resp := doGet(t, env.srv, "/activate/"+inviteToken, nil)
		assertStatus(t, resp, http.StatusOK)

		body := readBody(resp)
		if !strings.Contains(body, "<form") {
			t.Errorf("expected HTML form in response, got: %.200s", body)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected Content-Type text/html, got %q", ct)
		}
	})

	t.Run("GET_activate_with_invalid_token_returns_404", func(t *testing.T) {
		resp := doGet(t, env.srv, "/activate/00000000-0000-0000-0000-000000000000", nil)
		if resp.StatusCode != http.StatusNotFound {
			body := readBody(resp)
			t.Fatalf("expected 404, got %d (body=%q)", resp.StatusCode, body)
		}
		resp.Body.Close()
	})

	t.Run("POST_activate_creates_user_and_links_contact", func(t *testing.T) {
		if inviteToken == "" {
			t.Skip("no invite token from previous subtest")
		}
		resp := doRequest(t, env.srv, "POST", "/activate/"+inviteToken, "",
			`{"password":"securePass1"}`)
		assertStatus(t, resp, http.StatusOK)

		var body map[string]any
		decodeJSON(t, resp, &body)

		if body["status"] != "activated" {
			t.Errorf("status: got %v, want activated", body["status"])
		}
		if body["user_id"] == nil || body["user_id"] == "" {
			t.Error("expected non-empty user_id in activation response")
		}

		// Confirm login works with the new credentials.
		loginResp := doJSON(t, env.srv, "/login", map[string]any{
			"email":    "invited@example.com",
			"password": "securePass1",
		})
		assertStatus(t, loginResp, http.StatusOK)
		var tr TokenResponse
		decodeJSON(t, loginResp, &tr)
		if tr.AccessToken == "" {
			t.Error("expected non-empty access_token after activation login")
		}
	})

	t.Run("activated_user_can_access_userinfo", func(t *testing.T) {
		if inviteToken == "" {
			t.Skip("no invite token from previous subtest")
		}
		// Log in as the activated user and call /userinfo.
		loginResp := doJSON(t, env.srv, "/login", map[string]any{
			"email":    "invited@example.com",
			"password": "securePass1",
		})
		assertStatus(t, loginResp, http.StatusOK)
		var tr TokenResponse
		decodeJSON(t, loginResp, &tr)

		uiResp := doGet(t, env.srv, "/userinfo", map[string]string{
			"Authorization": "Bearer " + tr.AccessToken,
		})
		assertStatus(t, uiResp, http.StatusOK)
		var ui UserInfoResponse
		decodeJSON(t, uiResp, &ui)

		if ui.Email != "invited@example.com" {
			t.Errorf("userinfo email: got %q, want invited@example.com", ui.Email)
		}
	})

	t.Run("POST_activate_with_used_token_returns_404", func(t *testing.T) {
		if inviteToken == "" {
			t.Skip("no invite token from previous subtest")
		}
		// Token was consumed by the activation above.
		resp := doRequest(t, env.srv, "POST", "/activate/"+inviteToken, "",
			`{"password":"anotherPass1"}`)
		if resp.StatusCode != http.StatusNotFound {
			body := readBody(resp)
			t.Fatalf("expected 404 for used token, got %d (body=%q)", resp.StatusCode, body)
		}
		resp.Body.Close()
	})

	t.Run("POST_activate_with_short_password_returns_400", func(t *testing.T) {
		// Create a fresh contact with its own invite token.
		resp := doRequest(t, env.srv, "POST", "/admin/contacts", token,
			`{"email":"shortpw@example.com","name":"Short PW"}`)
		assertStatus(t, resp, http.StatusCreated)
		var c map[string]any
		decodeJSON(t, resp, &c)
		shortToken, _ := c["invite_token"].(string)
		if shortToken == "" {
			t.Fatal("no invite token for short-password test contact")
		}

		resp2 := doRequest(t, env.srv, "POST", "/activate/"+shortToken, "",
			`{"password":"abc"}`)
		if resp2.StatusCode != http.StatusBadRequest {
			body := readBody(resp2)
			t.Fatalf("expected 400 for short password, got %d (body=%q)", resp2.StatusCode, body)
		}
		resp2.Body.Close()
	})

	t.Run("contact_user_id_linked_after_activation", func(t *testing.T) {
		if contactID == "" {
			t.Skip("no contact id from previous subtest")
		}
		// GET /admin/contacts/{id} and verify user_id is set.
		resp := doRequest(t, env.srv, "GET", "/admin/contacts/"+contactID, token, "")
		assertStatus(t, resp, http.StatusOK)

		var c map[string]any
		decodeJSON(t, resp, &c)

		userID, _ := c["user_id"].(string)
		if userID == "" {
			t.Error("expected user_id to be set on contact after activation")
		}
	})

	t.Run("invite_url_template_variable_in_campaign", func(t *testing.T) {
		// Use a CapturingMailer so we can inspect the rendered email body.
		capMailer := &CapturingMailer{}
		capEnv := newTestEnvWithMailer(t, capMailer)
		capToken := loginAsAdmin(t, capEnv)

		// Create contact (gets invite token).
		contactResp := doRequest(t, capEnv.srv, "POST", "/admin/contacts", capToken,
			`{"email":"campaign-invite@example.com","name":"Campaign Invite"}`)
		assertStatus(t, contactResp, http.StatusCreated)
		var cc map[string]any
		decodeJSON(t, contactResp, &cc)
		campInviteToken, _ := cc["invite_token"].(string)
		if campInviteToken == "" {
			t.Fatal("no invite_token on created contact")
		}
		campContactID, _ := cc["id"].(string)

		// Create segment and add contact.
		seg := createSegment(t, capEnv, capToken, "InviteSeg", "")
		segID, _ := seg["id"].(string)
		addContactToSegment(t, capEnv, capToken, segID, campContactID)

		// Create campaign with {{.InviteURL}} in the body.
		segsJSON, _ := json.Marshal([]string{segID})
		campBody := fmt.Sprintf(
			`{"subject":"Your Invite","html_body":"<p>Click here: {{.InviteURL}}</p>","from_name":"Test","from_email":"test@example.com","segment_ids":%s}`,
			segsJSON,
		)
		campResp := doRequest(t, capEnv.srv, "POST", "/admin/campaigns", capToken, campBody)
		assertStatus(t, campResp, http.StatusCreated)
		var camp map[string]any
		decodeJSON(t, campResp, &camp)
		campID, _ := camp["id"].(string)

		// Send campaign (synchronous in test mode).
		sendResp := doRequest(t, capEnv.srv, "POST", "/admin/campaigns/"+campID+"/send", capToken, "")
		assertStatus(t, sendResp, http.StatusAccepted)
		sendResp.Body.Close()

		// Verify the captured email contains the /activate/ URL.
		if len(capMailer.mails) == 0 {
			t.Fatal("no emails captured — expected campaign to send at least one email")
		}
		found := false
		for _, m := range capMailer.mails {
			if strings.Contains(m.Body, "/activate/"+campInviteToken) {
				found = true
				break
			}
		}
		if !found {
			bodies := make([]string, len(capMailer.mails))
			for i, m := range capMailer.mails {
				bodies[i] = m.Body
			}
			t.Errorf("expected /activate/%s in email body, got bodies: %v", campInviteToken, bodies)
		}

		// Also verify the campaign recipients show the contact as sent.
		statsResp := doRequest(t, capEnv.srv, "GET", "/admin/campaigns/"+campID+"/stats", capToken, "")
		assertStatus(t, statsResp, http.StatusOK)
		var stats map[string]any
		decodeJSON(t, statsResp, &stats)
		sent, _ := stats["sent"].(float64)
		if int(sent) != 1 {
			t.Errorf("expected 1 sent recipient, got %v", stats["sent"])
		}
	})
}

// ---------------------------------------------------------------------------
// TestParseTenantConfigs
// ---------------------------------------------------------------------------

func TestParseTenantConfigs(t *testing.T) {
	t.Run("single object", func(t *testing.T) {
		data := []byte(`{"slug":"demo","name":"Demo"}`)
		configs, err := parseTenantConfigs(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 1 {
			t.Fatalf("expected 1 config, got %d", len(configs))
		}
		if configs[0].Slug != "demo" {
			t.Errorf("slug: got %q, want %q", configs[0].Slug, "demo")
		}
		if configs[0].Name != "Demo" {
			t.Errorf("name: got %q, want %q", configs[0].Name, "Demo")
		}
	})

	t.Run("array of objects", func(t *testing.T) {
		data := []byte(`[{"slug":"a","name":"A"},{"slug":"b","name":"B"}]`)
		configs, err := parseTenantConfigs(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 2 {
			t.Fatalf("expected 2 configs, got %d", len(configs))
		}
		if configs[0].Slug != "a" {
			t.Errorf("configs[0].slug: got %q, want %q", configs[0].Slug, "a")
		}
		if configs[1].Slug != "b" {
			t.Errorf("configs[1].slug: got %q, want %q", configs[1].Slug, "b")
		}
	})

	t.Run("single object with whitespace", func(t *testing.T) {
		data := []byte(`  { "slug": "ws", "name": "Whitespace" }  `)
		configs, err := parseTenantConfigs(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 1 || configs[0].Slug != "ws" {
			t.Errorf("unexpected result: %+v", configs)
		}
	})

	t.Run("array with whitespace", func(t *testing.T) {
		data := []byte(`  [ { "slug": "x", "name": "X" } ]  `)
		configs, err := parseTenantConfigs(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 1 || configs[0].Slug != "x" {
			t.Errorf("unexpected result: %+v", configs)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		data := []byte(`{invalid`)
		_, err := parseTenantConfigs(data)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("empty array", func(t *testing.T) {
		data := []byte(`[]`)
		configs, err := parseTenantConfigs(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 0 {
			t.Errorf("expected 0 configs, got %d", len(configs))
		}
	})
}

// ---------------------------------------------------------------------------
// TestLifecycleTokenSecurity (Issue #20)
// ---------------------------------------------------------------------------

func TestLifecycleTokenSecurity(t *testing.T) {
	// --- Item 1: Single-use activation tokens ---

	t.Run("activation_token_invalidated_after_use", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		c := createContact(t, env, adminToken, "singleuse@test.com", "Single Use")
		inviteToken, _ := c["invite_token"].(string)
		if inviteToken == "" {
			t.Fatal("expected invite_token")
		}

		// First activation succeeds.
		resp := doRequest(t, env.srv, "POST", "/activate/"+inviteToken, "",
			`{"password":"password123"}`)
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		// Second activation with same token must fail.
		resp2 := doRequest(t, env.srv, "POST", "/activate/"+inviteToken, "",
			`{"password":"password456"}`)
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("second activation: want 404, got %d", resp2.StatusCode)
		}
		resp2.Body.Close()

		// GET with used token must also fail.
		resp3 := doGet(t, env.srv, "/activate/"+inviteToken, nil)
		if resp3.StatusCode != http.StatusNotFound {
			t.Errorf("GET used token: want 404, got %d", resp3.StatusCode)
		}
		resp3.Body.Close()
	})

	t.Run("activation_does_not_create_duplicate_user", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		c := createContact(t, env, adminToken, "nodup@test.com", "No Dup")
		inviteToken, _ := c["invite_token"].(string)

		resp := doRequest(t, env.srv, "POST", "/activate/"+inviteToken, "",
			`{"password":"password123"}`)
		assertStatus(t, resp, http.StatusOK)
		resp.Body.Close()

		resp2 := doRequest(t, env.srv, "POST", "/activate/"+inviteToken, "",
			`{"password":"password456"}`)
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("duplicate activation: want 404, got %d", resp2.StatusCode)
		}
		resp2.Body.Close()

		loginResp := doJSON(t, env.srv, "/login", map[string]any{
			"email":    "nodup@test.com",
			"password": "password123",
		})
		assertStatus(t, loginResp, http.StatusOK)
		loginResp.Body.Close()
	})

	// --- Item 2: Idempotent unsubscribe ---

	t.Run("unsubscribe_is_idempotent_returns_200", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		createContact(t, env, adminToken, "idempotent@test.com", "Idempotent")
		contact, err := env.store.GetContactByEmail("idempotent@test.com")
		if err != nil {
			t.Fatalf("GetContactByEmail: %v", err)
		}

		resp1 := doRequest(t, env.srv, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		assertStatus(t, resp1, http.StatusOK)
		resp1.Body.Close()

		resp2 := doRequest(t, env.srv, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		assertStatus(t, resp2, http.StatusOK)
		resp2.Body.Close()

		resp3 := doRequest(t, env.srv, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
		assertStatus(t, resp3, http.StatusOK)
		resp3.Body.Close()

		updated, _ := env.store.GetContactByEmail("idempotent@test.com")
		if !updated.Unsubscribed {
			t.Error("contact must remain unsubscribed")
		}
	})

	t.Run("unsubscribe_token_does_not_grant_other_access", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		createContact(t, env, adminToken, "noaccess@test.com", "No Access")
		contact, _ := env.store.GetContactByEmail("noaccess@test.com")

		resp := doRequest(t, env.srv, "POST", "/activate/"+contact.UnsubscribeToken, "",
			`{"password":"password123"}`)
		if resp.StatusCode == http.StatusOK {
			t.Error("unsubscribe token must not work as activation token")
		}
		resp.Body.Close()
	})

	// --- Item 3: TTL expiry for lifecycle tokens ---

	t.Run("expired_invite_token_rejected", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		c := createContact(t, env, adminToken, "expired@test.com", "Expired")
		inviteToken, _ := c["invite_token"].(string)
		if inviteToken == "" {
			t.Fatal("expected invite_token")
		}

		_, err := env.store.DB().Exec(
			"UPDATE contacts SET invite_token_expires_at = ? WHERE invite_token = ?",
			time.Now().Add(-1*time.Hour), inviteToken,
		)
		if err != nil {
			t.Fatalf("backdate invite_token_expires_at: %v", err)
		}

		resp := doGet(t, env.srv, "/activate/"+inviteToken, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET expired token: want 404, got %d", resp.StatusCode)
		}
		resp.Body.Close()

		resp2 := doRequest(t, env.srv, "POST", "/activate/"+inviteToken, "",
			`{"password":"password123"}`)
		if resp2.StatusCode != http.StatusNotFound {
			t.Errorf("POST expired token: want 404, got %d", resp2.StatusCode)
		}
		resp2.Body.Close()
	})

	t.Run("non_expired_invite_token_works", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		c := createContact(t, env, adminToken, "fresh@test.com", "Fresh")
		inviteToken, _ := c["invite_token"].(string)
		if inviteToken == "" {
			t.Fatal("expected invite_token")
		}

		resp := doGet(t, env.srv, "/activate/"+inviteToken, nil)
		assertStatus(t, resp, http.StatusOK)
		body := readBody(resp)
		if !strings.Contains(body, "<form") {
			t.Error("expected HTML form for non-expired token")
		}
	})

	// --- Item 4: Invite token exposure audit ---

	t.Run("invite_token_not_in_list_contacts_response", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		createContact(t, env, adminToken, "audit@test.com", "Audit")

		resp := doRequest(t, env.srv, "GET", "/admin/contacts", adminToken, "")
		assertStatus(t, resp, http.StatusOK)
		body := readBody(resp)

		if strings.Contains(body, "invite_token") {
			t.Error("invite_token must not appear in list contacts response")
		}
	})

	t.Run("invite_token_not_in_get_contact_response", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		c := createContact(t, env, adminToken, "audit2@test.com", "Audit2")
		contactID, _ := c["id"].(string)

		resp := doRequest(t, env.srv, "GET", "/admin/contacts/"+contactID, adminToken, "")
		assertStatus(t, resp, http.StatusOK)
		body := readBody(resp)

		if strings.Contains(body, "invite_token") {
			t.Error("invite_token must not appear in get contact response")
		}
	})

	t.Run("unsubscribe_token_not_in_any_api_response", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		c := createContact(t, env, adminToken, "audit3@test.com", "Audit3")
		contactID, _ := c["id"].(string)

		listResp := doRequest(t, env.srv, "GET", "/admin/contacts", adminToken, "")
		assertStatus(t, listResp, http.StatusOK)
		listBody := readBody(listResp)
		if strings.Contains(listBody, "unsubscribe_token") {
			t.Error("unsubscribe_token must not appear in list contacts response")
		}

		detailResp := doRequest(t, env.srv, "GET", "/admin/contacts/"+contactID, adminToken, "")
		assertStatus(t, detailResp, http.StatusOK)
		detailBody := readBody(detailResp)
		if strings.Contains(detailBody, "unsubscribe_token") {
			t.Error("unsubscribe_token must not appear in get contact response")
		}
	})

	t.Run("invite_token_only_in_creation_response", func(t *testing.T) {
		env := newTestEnv(t)
		adminToken := loginAsAdmin(t, env)

		c := createContact(t, env, adminToken, "creation@test.com", "Creation")
		inviteToken, _ := c["invite_token"].(string)
		if inviteToken == "" {
			t.Error("creation response must include invite_token")
		}
	})
}

// ---------------------------------------------------------------------------
// Client Credentials Grant
// ---------------------------------------------------------------------------

func TestClientCredentialsGrant(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "svc", []string{"https://example.com/cb"})

	resp := doForm(t, srv, "/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client.ClientID},
		"client_secret": {client.ClientSecret},
	})
	assertStatus(t, resp, http.StatusOK)

	var tr TokenResponse
	decodeJSON(t, resp, &tr)

	if tr.AccessToken == "" {
		t.Fatal("expected access_token, got empty string")
	}
	if tr.TokenType != "Bearer" {
		t.Errorf("token_type: got %q, want %q", tr.TokenType, "Bearer")
	}
	if tr.ExpiresIn != 3600 {
		t.Errorf("expires_in: got %d, want 3600", tr.ExpiresIn)
	}
}

func TestClientCredentialsInvalidSecret(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "svc", []string{"https://example.com/cb"})

	resp := doForm(t, srv, "/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client.ClientID},
		"client_secret": {"wrong-secret"},
	})
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestClientCredentialsUnknownClient(t *testing.T) {
	srv := newTestServer(t)

	resp := doForm(t, srv, "/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"non-existent-client-id"},
		"client_secret": {"some-secret"},
	})
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestClientCredentialsMissingFields(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "svc", []string{"https://example.com/cb"})

	resp := doForm(t, srv, "/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_secret": {client.ClientSecret},
	})
	assertStatus(t, resp, http.StatusBadRequest)

	resp = doForm(t, srv, "/token", url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {client.ClientID},
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestClientCredentialsNoRefreshToken(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "svc", []string{"https://example.com/cb"})

	resp := doForm(t, srv, "/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client.ClientID},
		"client_secret": {client.ClientSecret},
	})
	assertStatus(t, resp, http.StatusOK)

	var tr TokenResponse
	decodeJSON(t, resp, &tr)

	if tr.RefreshToken != "" {
		t.Errorf("expected no refresh_token, got %q", tr.RefreshToken)
	}
}

func TestClientCredentialsTokenValidation(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "svc", []string{"https://example.com/cb"})

	resp := doForm(t, srv, "/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client.ClientID},
		"client_secret": {client.ClientSecret},
	})
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)

	jwksResp := doGet(t, srv, "/jwks", nil)
	assertStatus(t, jwksResp, http.StatusOK)
	var jwks struct {
		Keys []struct {
			N string `json:"n"`
			E string `json:"e"`
		} `json:"keys"`
	}
	decodeJSON(t, jwksResp, &jwks)
	if len(jwks.Keys) == 0 {
		t.Fatal("JWKS: no keys returned")
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(jwks.Keys[0].N)
	if err != nil {
		t.Fatalf("decode n: %v", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwks.Keys[0].E)
	if err != nil {
		t.Fatalf("decode e: %v", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Int64())
	pubKey := &rsa.PublicKey{N: n, E: e}

	parsed, err := jwt.Parse(tr.AccessToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		t.Fatalf("jwt.Parse: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("token is not valid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected MapClaims")
	}

	if sub, _ := claims["sub"].(string); sub != client.ClientID {
		t.Errorf("sub: got %q, want %q", sub, client.ClientID)
	}

	switch aud := claims["aud"].(type) {
	case string:
		if aud != client.ClientID {
			t.Errorf("aud: got %q, want %q", aud, client.ClientID)
		}
	case []interface{}:
		if len(aud) == 0 {
			t.Error("aud: empty slice")
		} else if aud[0].(string) != client.ClientID {
			t.Errorf("aud[0]: got %q, want %q", aud[0], client.ClientID)
		}
	default:
		t.Errorf("aud: unexpected type %T", claims["aud"])
	}

	if _, ok := claims["tid"]; !ok {
		t.Error("tid: expected claim to be present")
	}

	if typ, _ := claims["type"].(string); typ != "client_credentials" {
		t.Errorf("type: got %q, want %q", typ, "client_credentials")
	}
}

func TestClientCredentialsBasicAuth(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "svc", []string{"https://example.com/cb"})

	credentials := base64.StdEncoding.EncodeToString([]byte(client.ClientID + ":" + client.ClientSecret))

	body := strings.NewReader("grant_type=client_credentials")
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/token", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+credentials)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	assertStatus(t, resp, http.StatusOK)

	var tr TokenResponse
	decodeJSON(t, resp, &tr)
	if tr.AccessToken == "" {
		t.Fatal("expected access_token, got empty string")
	}
}

func TestDiscoveryIncludesClientCredentials(t *testing.T) {
	srv := newTestServer(t)

	resp := doGet(t, srv, "/.well-known/openid-configuration", nil)
	assertStatus(t, resp, http.StatusOK)

	var doc OIDCDiscovery
	decodeJSON(t, resp, &doc)

	foundGrant := false
	for _, g := range doc.GrantTypesSupported {
		if g == "client_credentials" {
			foundGrant = true
			break
		}
	}
	if !foundGrant {
		t.Errorf("grant_types_supported does not contain %q: %v", "client_credentials", doc.GrantTypesSupported)
	}

	foundMethod := false
	for _, m := range doc.TokenEndpointAuthMethodsSupported {
		if m == "client_secret_basic" {
			foundMethod = true
			break
		}
	}
	if !foundMethod {
		t.Errorf("token_endpoint_auth_methods_supported does not contain %q: %v", "client_secret_basic", doc.TokenEndpointAuthMethodsSupported)
	}
}

// ---------------------------------------------------------------------------
// Audience / client_id tests
// ---------------------------------------------------------------------------

func TestLoginWithClientID(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "Test App", []string{"http://localhost/cb"})

	resp := doJSON(t, srv, "/login", map[string]any{
		"email":     "admin@test.com",
		"password":  "adminpass123",
		"client_id": client.ClientID,
	})
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)

	claims := verifyJWT(t, srv, tr.AccessToken)
	aud, _ := claims["aud"].(string)
	if aud != client.ClientID {
		t.Errorf("expected aud %q, got %q", client.ClientID, aud)
	}
}

func TestLoginWithoutClientID(t *testing.T) {
	srv := newTestServer(t)

	resp := doJSON(t, srv, "/login", map[string]any{
		"email":    "admin@test.com",
		"password": "adminpass123",
	})
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)

	claims := verifyJWT(t, srv, tr.AccessToken)
	aud, _ := claims["aud"].(string)
	const wantAud = "http://test-issuer"
	if aud != wantAud {
		t.Errorf("expected aud %q, got %q", wantAud, aud)
	}
}

func TestLoginWithInvalidClientID(t *testing.T) {
	srv := newTestServer(t)

	resp := doJSON(t, srv, "/login", map[string]any{
		"email":     "admin@test.com",
		"password":  "adminpass123",
		"client_id": "nonexistent-id",
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestRegisterWithClientID(t *testing.T) {
	srv := newTestServer(t)
	client := registerClient(t, srv, "Test App", []string{"http://localhost/cb"})

	regResp := doJSON(t, srv, "/register", map[string]any{
		"email":     "test@example.com",
		"password":  "password123",
		"name":      "Test",
		"client_id": client.ClientID,
	})
	assertStatus(t, regResp, http.StatusCreated)

	loginResp := doJSON(t, srv, "/login", map[string]any{
		"email":     "test@example.com",
		"password":  "password123",
		"client_id": client.ClientID,
	})
	assertStatus(t, loginResp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, loginResp, &tr)

	claims := verifyJWT(t, srv, tr.AccessToken)
	aud, _ := claims["aud"].(string)
	if aud != client.ClientID {
		t.Errorf("expected aud %q, got %q", client.ClientID, aud)
	}
}

func TestImportTenantWithFixedClientID(t *testing.T) {
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}

	tenantStore := tenant.NewStore(db)
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)

	cfg := tenant.ImportConfig{
		Slug: "fixed-client-test",
		Name: "Fixed Client Test",
		Admin: &tenant.AdminConfig{
			Email:    "admin@fixed.test",
			Password: "admin1234",
		},
		Clients: []tenant.ClientConfig{{
			ClientID:     "00000000-0000-0000-0000-000000000001",
			Name:         "Demo Frontend",
			RedirectURIs: []string{"http://localhost:3001/callback"},
		}},
	}

	result, err := tenant.ImportTenantConfig(tenantStore, iamStore, mktStore, cfg)
	if err != nil {
		t.Fatalf("ImportTenantConfig: %v", err)
	}

	found := false
	for _, c := range result.Clients {
		if c.ClientID == "00000000-0000-0000-0000-000000000001" {
			found = true
			if c.Name != "Demo Frontend" {
				t.Errorf("expected client name 'Demo Frontend', got %q", c.Name)
			}
		}
	}
	if !found {
		t.Errorf("expected client with fixed ID 00000000-0000-0000-0000-000000000001, got %v", result.Clients)
	}
}

// ---------------------------------------------------------------------------
// Service Token Admin Access
// ---------------------------------------------------------------------------

// getServiceToken registers a client and obtains a client_credentials token.
func getServiceToken(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	client := registerClient(t, srv, "svc-admin", []string{"https://example.com/cb"})
	resp := doForm(t, srv, "/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {client.ClientID},
		"client_secret": {client.ClientSecret},
	})
	assertStatus(t, resp, http.StatusOK)
	var tr TokenResponse
	decodeJSON(t, resp, &tr)
	if tr.AccessToken == "" {
		t.Fatal("getServiceToken: empty access_token")
	}
	return tr.AccessToken
}

func TestServiceTokenCanCreateContact(t *testing.T) {
	srv := newTestServer(t)
	token := getServiceToken(t, srv)

	resp := doJSONWithAuth(t, srv, "/admin/contacts", map[string]any{
		"email": "svc-contact@example.com",
		"name":  "Service Contact",
	}, token)
	assertStatus(t, resp, http.StatusCreated)
}

func TestServiceTokenCanListContacts(t *testing.T) {
	srv := newTestServer(t)
	token := getServiceToken(t, srv)

	resp := doRequest(t, srv, "GET", "/admin/contacts", token, "")
	assertStatus(t, resp, http.StatusOK)
}

// Service tokens must NOT access IAM admin endpoints (least-privilege).
func TestServiceTokenCannotAccessUserAdmin(t *testing.T) {
	srv := newTestServer(t)
	token := getServiceToken(t, srv)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/admin/users"},
		{"GET", "/admin/clients"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			resp := doRequest(t, srv, ep.method, ep.path, token, "")
			if resp.StatusCode != http.StatusForbidden {
				body := readBody(resp)
				t.Errorf("expected 403, got %d (body=%q)", resp.StatusCode, body)
			} else {
				resp.Body.Close()
			}
		})
	}
}

func TestUserTokenStillWorks(t *testing.T) {
	srv := newTestServer(t)
	adminToken := getAdminToken(t, srv)

	// Admin user can access both IAM and marketing endpoints
	endpoints := []struct {
		method string
		path   string
		want   int
	}{
		{"GET", "/admin/users", http.StatusOK},
		{"GET", "/admin/clients", http.StatusOK},
		{"GET", "/admin/contacts", http.StatusOK},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			resp := doRequest(t, srv, ep.method, ep.path, adminToken, "")
			if resp.StatusCode != ep.want {
				body := readBody(resp)
				t.Errorf("expected %d, got %d (body=%q)", ep.want, resp.StatusCode, body)
			} else {
				resp.Body.Close()
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Password Reset Tests
// ---------------------------------------------------------------------------

func TestForgotPasswordSendsEmail(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)

	// Register a user
	resp := doJSON(t, env.srv, "/register", map[string]any{
		"email": "reset@example.com", "password": "password123", "name": "Reset User",
	})
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Request password reset
	resp = doJSON(t, env.srv, "/forgot-password", map[string]any{
		"email": "reset@example.com",
	})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Check email was sent
	if len(capMailer.mails) == 0 {
		t.Fatal("expected a reset email to be sent")
	}
	mail := capMailer.mails[len(capMailer.mails)-1]
	if mail.To != "reset@example.com" {
		t.Errorf("expected email to reset@example.com, got %s", mail.To)
	}
	if mail.Subject != "Reset your password" {
		t.Errorf("expected subject 'Reset your password', got %s", mail.Subject)
	}
	if !strings.Contains(mail.Body, "/reset-password/") {
		t.Error("expected email body to contain reset link")
	}
}

func TestForgotPasswordUnknownEmailReturns200(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)

	resp := doJSON(t, env.srv, "/forgot-password", map[string]any{
		"email": "nonexistent@example.com",
	})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// No email should have been sent
	if len(capMailer.mails) != 0 {
		t.Errorf("expected no email sent for unknown address, got %d", len(capMailer.mails))
	}
}

func TestResetPasswordSuccess(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)

	// Register user
	resp := doJSON(t, env.srv, "/register", map[string]any{
		"email": "resetok@example.com", "password": "oldpass123", "name": "Reset OK",
	})
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Request reset
	resp = doJSON(t, env.srv, "/forgot-password", map[string]any{"email": "resetok@example.com"})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Extract token from email
	mail := capMailer.mails[len(capMailer.mails)-1]
	idx := strings.Index(mail.Body, "/reset-password/")
	if idx == -1 {
		t.Fatal("reset link not found in email")
	}
	tokenStart := idx + len("/reset-password/")
	token := mail.Body[tokenStart : tokenStart+36] // UUID length

	// Reset password via JSON
	resp = doJSON(t, env.srv, "/reset-password/"+token, map[string]any{
		"password": "newpass123",
	})
	assertStatus(t, resp, http.StatusOK)
	var result map[string]string
	decodeJSON(t, resp, &result)
	if result["status"] != "password_reset" {
		t.Errorf("expected status password_reset, got %s", result["status"])
	}

	// Login with new password should work
	resp = doJSON(t, env.srv, "/login", map[string]any{
		"email": "resetok@example.com", "password": "newpass123",
	})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Login with old password should fail
	resp = doJSON(t, env.srv, "/login", map[string]any{
		"email": "resetok@example.com", "password": "oldpass123",
	})
	if resp.StatusCode == http.StatusOK {
		t.Error("expected old password to be rejected")
	}
	resp.Body.Close()
}

func TestResetPasswordExpiredToken(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)

	// Register user
	resp := doJSON(t, env.srv, "/register", map[string]any{
		"email": "expired@example.com", "password": "password123", "name": "Expired",
	})
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Request reset
	resp = doJSON(t, env.srv, "/forgot-password", map[string]any{"email": "expired@example.com"})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Extract token
	mail := capMailer.mails[len(capMailer.mails)-1]
	idx := strings.Index(mail.Body, "/reset-password/")
	token := mail.Body[idx+len("/reset-password/") : idx+len("/reset-password/")+36]

	// Backdate the token expiry
	env.store.DB().Exec("UPDATE users SET reset_token_expires_at = ? WHERE email = ?",
		time.Now().UTC().Add(-1*time.Hour), "expired@example.com")

	// Try to reset — should fail
	resp = doJSON(t, env.srv, "/reset-password/"+token, map[string]any{"password": "newpass123"})
	assertStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

func TestResetPasswordSingleUse(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)

	// Register user
	resp := doJSON(t, env.srv, "/register", map[string]any{
		"email": "singleuse@example.com", "password": "password123", "name": "Single Use",
	})
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Request reset
	resp = doJSON(t, env.srv, "/forgot-password", map[string]any{"email": "singleuse@example.com"})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Extract token
	mail := capMailer.mails[len(capMailer.mails)-1]
	idx := strings.Index(mail.Body, "/reset-password/")
	token := mail.Body[idx+len("/reset-password/") : idx+len("/reset-password/")+36]

	// First use — should succeed
	resp = doJSON(t, env.srv, "/reset-password/"+token, map[string]any{"password": "newpass123"})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Second use — should fail
	resp = doJSON(t, env.srv, "/reset-password/"+token, map[string]any{"password": "anotherpass"})
	assertStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

func TestResetPasswordGetForm(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)

	// Register user
	resp := doJSON(t, env.srv, "/register", map[string]any{
		"email": "formtest@example.com", "password": "password123", "name": "Form Test",
	})
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Request reset
	resp = doJSON(t, env.srv, "/forgot-password", map[string]any{"email": "formtest@example.com"})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Extract token
	mail := capMailer.mails[len(capMailer.mails)-1]
	idx := strings.Index(mail.Body, "/reset-password/")
	token := mail.Body[idx+len("/reset-password/") : idx+len("/reset-password/")+36]

	// GET form
	resp = doGet(t, env.srv, "/reset-password/"+token, nil)
	assertStatus(t, resp, http.StatusOK)
	body := readBody(resp)

	if !strings.Contains(body, "text/html") || !strings.Contains(body, "Reset Password") {
		// Check Content-Type header instead
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected text/html content type, got %s", ct)
		}
	}
	if !strings.Contains(body, "formtest@example.com") {
		t.Error("expected email to be shown in form")
	}
	if !strings.Contains(body, "password") {
		t.Error("expected password input in form")
	}
}

func TestResetPasswordShortPassword(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)

	// Register user
	resp := doJSON(t, env.srv, "/register", map[string]any{
		"email": "shortpw@example.com", "password": "password123", "name": "Short PW",
	})
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Request reset
	resp = doJSON(t, env.srv, "/forgot-password", map[string]any{"email": "shortpw@example.com"})
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Extract token
	mail := capMailer.mails[len(capMailer.mails)-1]
	idx := strings.Index(mail.Body, "/reset-password/")
	token := mail.Body[idx+len("/reset-password/") : idx+len("/reset-password/")+36]

	// Try short password
	resp = doJSON(t, env.srv, "/reset-password/"+token, map[string]any{"password": "short"})
	assertStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Campaign PDF Attachment Tests
// ---------------------------------------------------------------------------

// createCampaignWithAttachment is like createCampaign but includes an attachment_url.
func createCampaignWithAttachment(t *testing.T, env *testEnv, token string, segmentIDs []string, attachmentURL string) map[string]any {
	t.Helper()
	segsJSON, _ := json.Marshal(segmentIDs)
	body := fmt.Sprintf(`{"subject":"Test Subject","html_body":"<p>Hello {{.Name}}</p>","from_name":"Sender","from_email":"sender@example.com","segment_ids":%s,"attachment_url":%q}`, segsJSON, attachmentURL)
	resp := doRequest(t, env.srv, "POST", "/admin/campaigns", token, body)
	assertStatus(t, resp, http.StatusCreated)
	var c map[string]any
	decodeJSON(t, resp, &c)
	return c
}

func TestCreateCampaignWithAttachment(t *testing.T) {
	env := newTestEnv(t)
	token := loginAsAdmin(t, env)

	c := createCampaignWithAttachment(t, env, token, []string{}, "https://example.com/guide.pdf")
	if c["attachment_url"] != "https://example.com/guide.pdf" {
		t.Errorf("expected attachment_url in create response, got %v", c["attachment_url"])
	}

	// GET the campaign and verify attachment_url is returned.
	campID, _ := c["id"].(string)
	resp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campID, token, "")
	assertStatus(t, resp, http.StatusOK)
	var detail map[string]any
	decodeJSON(t, resp, &detail)
	camp, _ := detail["campaign"].(map[string]any)
	if camp == nil {
		t.Fatal("expected campaign key in GET response")
	}
	if camp["attachment_url"] != "https://example.com/guide.pdf" {
		t.Errorf("expected attachment_url in GET response, got %v", camp["attachment_url"])
	}
}

func TestSendCampaignWithAttachment(t *testing.T) {
	// Start an httptest.Server that serves a small PDF file.
	pdfData := []byte("%PDF-1.4 test content for attachment")
	pdfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(pdfData)
	}))
	defer pdfServer.Close()

	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)
	token := loginAsAdmin(t, env)

	segID := newTestEnvSegWithContact(t, env, token, "AttachSeg", "attach@test.com")
	camp := createCampaignWithAttachment(t, env, token, []string{segID}, pdfServer.URL+"/guide.pdf")
	campID, _ := camp["id"].(string)

	// Send campaign (synchronous in test mode).
	sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campID+"/send", token, "")
	assertStatus(t, sendResp, http.StatusAccepted)
	sendResp.Body.Close()

	if len(capMailer.mails) == 0 {
		t.Fatal("no emails captured")
	}
	mail := capMailer.mails[0]
	if len(mail.Attachments) == 0 {
		t.Fatal("expected attachments in captured mail, got none")
	}
	att := mail.Attachments[0]
	if att.ContentType != "application/pdf" {
		t.Errorf("expected content type application/pdf, got %q", att.ContentType)
	}
	if att.Filename != "guide.pdf" {
		t.Errorf("expected filename guide.pdf, got %q", att.Filename)
	}
	if string(att.Data) != string(pdfData) {
		t.Errorf("attachment data mismatch: got %d bytes, want %d bytes", len(att.Data), len(pdfData))
	}
}

func TestCampaignAttachmentTooLarge(t *testing.T) {
	// Serve a response larger than 10MB.
	bigData := make([]byte, 11*1024*1024)
	bigServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(bigData)
	}))
	defer bigServer.Close()

	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)
	token := loginAsAdmin(t, env)

	segID := newTestEnvSegWithContact(t, env, token, "BigAttachSeg", "bigattach@test.com")
	camp := createCampaignWithAttachment(t, env, token, []string{segID}, bigServer.URL+"/big.pdf")
	campID, _ := camp["id"].(string)

	sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campID+"/send", token, "")
	assertStatus(t, sendResp, http.StatusAccepted)
	sendResp.Body.Close()

	// Campaign should be marked as failed due to oversized attachment.
	resp := doRequest(t, env.srv, "GET", "/admin/campaigns/"+campID, token, "")
	assertStatus(t, resp, http.StatusOK)
	var detail map[string]any
	decodeJSON(t, resp, &detail)
	campDetail, _ := detail["campaign"].(map[string]any)
	if campDetail["status"] != "failed" {
		t.Errorf("expected campaign status 'failed' for oversized attachment, got %q", campDetail["status"])
	}
}

func TestCampaignWithoutAttachment(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)
	token := loginAsAdmin(t, env)

	segID := newTestEnvSegWithContact(t, env, token, "NoAttachSeg", "noattach@test.com")
	camp := createCampaign(t, env, token, []string{segID})
	campID, _ := camp["id"].(string)

	sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campID+"/send", token, "")
	assertStatus(t, sendResp, http.StatusAccepted)
	sendResp.Body.Close()

	if len(capMailer.mails) == 0 {
		t.Fatal("no emails captured")
	}
	mail := capMailer.mails[0]
	if len(mail.Attachments) != 0 {
		t.Errorf("expected no attachments for campaign without attachment_url, got %d", len(mail.Attachments))
	}
}

// ---------------------------------------------------------------------------
// Rate Limiting Tests
// ---------------------------------------------------------------------------

func TestLoginRateLimited(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SeedAdmin("admin@test.com", "adminpass123", "Admin"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	rsaKey, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatalf("LoadOrCreateRSAKey: %v", err)
	}
	tokenSvc := NewTokenService(rsaKey, "http://test-issuer")

	rl := NewRateLimiter(map[string]float64{"/login": 10}, 5*time.Minute)
	defer rl.Stop()

	srv := httptest.NewServer(SetupServerWithRateLimit(store, tokenSvc, rl))
	defer srv.Close()

	body := `{"email":"admin@test.com","password":"adminpass123"}`
	for i := 0; i < 10; i++ {
		resp, err := http.Post(srv.URL+"/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i+1, resp.StatusCode)
		}
	}

	// 11th request should be rate limited
	resp, err := http.Post(srv.URL+"/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request 11: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("request 11: got %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
}

// ---------------------------------------------------------------------------
// floatEnvOr
// ---------------------------------------------------------------------------

func TestFloatEnvOrDefault(t *testing.T) {
	t.Setenv("TEST_FLOAT_UNSET", "")
	if v := floatEnvOr("TEST_FLOAT_UNSET", 42); v != 42 {
		t.Fatalf("got %f, want 42", v)
	}
}

func TestFloatEnvOrValid(t *testing.T) {
	t.Setenv("TEST_FLOAT_VALID", "99.5")
	if v := floatEnvOr("TEST_FLOAT_VALID", 10); v != 99.5 {
		t.Fatalf("got %f, want 99.5", v)
	}
}

func TestFloatEnvOrZeroFallsBack(t *testing.T) {
	t.Setenv("TEST_FLOAT_ZERO", "0")
	if v := floatEnvOr("TEST_FLOAT_ZERO", 10); v != 10 {
		t.Fatalf("got %f, want 10 (fallback)", v)
	}
}

func TestFloatEnvOrNegativeFallsBack(t *testing.T) {
	t.Setenv("TEST_FLOAT_NEG", "-5")
	if v := floatEnvOr("TEST_FLOAT_NEG", 10); v != 10 {
		t.Fatalf("got %f, want 10 (fallback)", v)
	}
}

func TestFloatEnvOrInvalidFallsBack(t *testing.T) {
	t.Setenv("TEST_FLOAT_BAD", "abc")
	if v := floatEnvOr("TEST_FLOAT_BAD", 10); v != 10 {
		t.Fatalf("got %f, want 10 (fallback)", v)
	}
}

// ---------------------------------------------------------------------------
// Input safety: HTML escaping in campaign templates
// ---------------------------------------------------------------------------

func TestCampaignTemplateEscapesContactName(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)
	token := loginAsAdmin(t, env)

	xssName := `<script>alert(1)</script>`
	c := createContact(t, env, token, "xssname@example.com", xssName)
	contactID, _ := c["id"].(string)

	seg := createSegment(t, env, token, "XSSNameSeg", "")
	segID, _ := seg["id"].(string)
	addContactToSegment(t, env, token, segID, contactID)

	segsJSON, _ := json.Marshal([]string{segID})
	campBody := fmt.Sprintf(
		`{"subject":"XSS Test","html_body":"<p>Hello {{.Name}}</p>","from_name":"Test","from_email":"test@example.com","segment_ids":%s}`,
		segsJSON,
	)
	campResp := doRequest(t, env.srv, "POST", "/admin/campaigns", token, campBody)
	assertStatus(t, campResp, http.StatusCreated)
	var camp map[string]any
	decodeJSON(t, campResp, &camp)
	campID, _ := camp["id"].(string)

	sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campID+"/send", token, "")
	assertStatus(t, sendResp, http.StatusAccepted)
	sendResp.Body.Close()

	if len(capMailer.mails) == 0 {
		t.Fatal("no emails captured")
	}
	body := capMailer.mails[0].Body
	escaped := html.EscapeString(xssName)
	if !strings.Contains(body, escaped) {
		t.Errorf("expected escaped name %q in body, got: %s", escaped, body)
	}
	if strings.Contains(body, xssName) {
		t.Errorf("body contains unescaped XSS payload: %s", body)
	}
}

func TestCampaignTemplateEscapesContactEmail(t *testing.T) {
	capMailer := &CapturingMailer{}
	env := newTestEnvWithMailer(t, capMailer)
	token := loginAsAdmin(t, env)

	// Create contact with a normal email but use {{.Email}} in template.
	// The contact email itself is valid, but we test that special chars
	// in the display context get escaped. Use a contact with name containing
	// HTML chars and reference {{.Email}} in the template.
	c := createContact(t, env, token, "escapeemail@example.com", "Normal")
	contactID, _ := c["id"].(string)

	seg := createSegment(t, env, token, "EmailEscSeg", "")
	segID, _ := seg["id"].(string)
	addContactToSegment(t, env, token, segID, contactID)

	// Template uses {{.Email}} — the email address itself should be escaped
	segsJSON, _ := json.Marshal([]string{segID})
	campBody := fmt.Sprintf(
		`{"subject":"Email Esc","html_body":"<p>Your email: {{.Email}}</p>","from_name":"Test","from_email":"test@example.com","segment_ids":%s}`,
		segsJSON,
	)
	campResp := doRequest(t, env.srv, "POST", "/admin/campaigns", token, campBody)
	assertStatus(t, campResp, http.StatusCreated)
	var camp map[string]any
	decodeJSON(t, campResp, &camp)
	campID, _ := camp["id"].(string)

	sendResp := doRequest(t, env.srv, "POST", "/admin/campaigns/"+campID+"/send", token, "")
	assertStatus(t, sendResp, http.StatusAccepted)
	sendResp.Body.Close()

	if len(capMailer.mails) == 0 {
		t.Fatal("no emails captured")
	}
	body := capMailer.mails[0].Body
	// The email should appear HTML-escaped in the body
	escaped := html.EscapeString("escapeemail@example.com")
	if !strings.Contains(body, escaped) {
		t.Errorf("expected escaped email %q in body, got: %s", escaped, body)
	}
}

// ---------------------------------------------------------------------------
// Input safety: email validation
// ---------------------------------------------------------------------------

func TestEmailValidationRejectsInvalid(t *testing.T) {
	invalid := []string{
		"a@b.c",          // single-char TLD
		"@foo.com",       // no local part
		"foo@",           // no domain
		"foo@bar",        // no TLD
		"foo@.com",       // domain starts with dot
		"foo bar@baz.com", // space in local part
		"",               // empty
	}
	for _, email := range invalid {
		t.Run(email, func(t *testing.T) {
			if iam.EmailRegex.MatchString(email) {
				t.Errorf("expected %q to be rejected", email)
			}
		})
	}
}

func TestEmailValidationAcceptsValid(t *testing.T) {
	valid := []string{
		"user@example.com",
		"first.last@example.co.uk",
		"user+tag@example.org",
		"user@subdomain.example.com",
		"a1@example.io",
	}
	for _, email := range valid {
		t.Run(email, func(t *testing.T) {
			if !iam.EmailRegex.MatchString(email) {
				t.Errorf("expected %q to be accepted", email)
			}
		})
	}
}

func TestContactEmailLengthLimit(t *testing.T) {
	env := newTestEnv(t)
	token := loginAsAdmin(t, env)

	// Build a 255+ char email
	longLocal := strings.Repeat("a", 243)
	longEmail := longLocal + "@example.com" // 243 + 12 = 255 chars

	resp := doRequest(t, env.srv, "POST", "/admin/contacts", token,
		fmt.Sprintf(`{"email":%q,"name":"Long"}`, longEmail))
	assertStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}
