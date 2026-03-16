package iam

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/jenslaufer/launch-kit/tenant"
)

type Handler struct {
	Store  *Store
	Tokens *TokenService
	Issuer string
}

func NewHandler(store *Store, tokens *TokenService, issuer string) *Handler {
	return &Handler{Store: store, Tokens: tokens, Issuer: issuer}
}

// tenantStore returns a store scoped to the request's tenant.
func (h *Handler) tenantStore(r *http.Request) *Store {
	return h.Store.ForTenant(tenant.FromContext(r.Context()))
}

// --- Helpers ---

func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, err, desc string) {
	WriteJSON(w, status, ErrorResponse{Error: err, ErrorDescription: desc})
}

var EmailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// --- Handlers ---

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if !EmailRegex.MatchString(req.Email) {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid email format")
		return
	}
	if req.Password == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "password required")
		return
	}
	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}

	store := h.tenantStore(r)
	user, err := store.CreateUser(req.Email, req.Password, req.Name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			WriteError(w, http.StatusConflict, "invalid_request", "email already registered")
			return
		}
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create user")
		return
	}

	WriteJSON(w, http.StatusCreated, user)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	store := h.tenantStore(r)
	user, err := store.AuthenticateUser(req.Email, req.Password)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "invalid_grant", "invalid credentials")
		return
	}

	tenantID := tenant.FromContext(r.Context())
	accessToken, err := h.Tokens.CreateAccessToken(user, h.Issuer, tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create access token")
		return
	}

	idToken, err := h.Tokens.CreateIDToken(user, h.Issuer, "", tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create id token")
		return
	}

	refreshToken, err := store.CreateRefreshToken("", user.ID, "openid profile email")
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create refresh token")
		return
	}

	WriteJSON(w, http.StatusOK, TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: refreshToken,
		IDToken:      idToken,
	})
}

func (h *Handler) AuthorizeGET(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Login - launch-kit</title>
<style>
body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}
form{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);width:320px}
h2{margin-top:0;text-align:center}
label{display:block;margin-top:1rem;font-size:0.9rem;color:#333}
input[type=email],input[type=password]{width:100%%;padding:0.5rem;margin-top:0.25rem;border:1px solid #ccc;border-radius:4px;box-sizing:border-box}
button{width:100%%;padding:0.75rem;margin-top:1.5rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:1rem}
button:hover{background:#1d4ed8}
</style></head><body>
<form method="POST" action="/authorize">
<h2>Sign In</h2>
<input type="hidden" name="client_id" value="%s">
<input type="hidden" name="redirect_uri" value="%s">
<input type="hidden" name="state" value="%s">
<input type="hidden" name="scope" value="%s">
<input type="hidden" name="nonce" value="%s">
<input type="hidden" name="code_challenge" value="%s">
<input type="hidden" name="code_challenge_method" value="%s">
<input type="hidden" name="response_type" value="%s">
<label>Email</label><input type="email" name="email" required autofocus>
<label>Password</label><input type="password" name="password" required>
<button type="submit">Sign In</button>
</form></body></html>`,
		escapeHTML(q.Get("client_id")),
		escapeHTML(q.Get("redirect_uri")),
		escapeHTML(q.Get("state")),
		escapeHTML(q.Get("scope")),
		escapeHTML(q.Get("nonce")),
		escapeHTML(q.Get("code_challenge")),
		escapeHTML(q.Get("code_challenge_method")),
		escapeHTML(q.Get("response_type")),
	)

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func (h *Handler) AuthorizePOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	scope := r.FormValue("scope")
	nonce := r.FormValue("nonce")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")

	store := h.tenantStore(r)

	// Validate client
	client, err := store.GetClient(clientID)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "unknown client_id")
		return
	}

	// Validate redirect_uri
	if !isValidRedirectURI(client, redirectURI) {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid redirect_uri")
		return
	}

	// Authenticate user
	user, err := store.AuthenticateUser(email, password)
	if err != nil {
		// Re-render login form with error (minimal approach: redirect back)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`<!DOCTYPE html><html><body><p>Invalid credentials. <a href="javascript:history.back()">Try again</a></p></body></html>`))
		return
	}

	// Generate auth code
	code, err := store.CreateAuthCode(clientID, user.ID, redirectURI, scope, nonce, codeChallenge, codeChallengeMethod)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create authorization code")
		return
	}

	// Redirect
	redirect, _ := url.Parse(redirectURI)
	q := redirect.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	redirect.RawQuery = q.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.AuthorizeGET(w, r)
	case http.MethodPost:
		h.AuthorizePOST(w, r)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if err := r.ParseForm(); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.tokenAuthorizationCode(w, r)
	case "refresh_token":
		h.tokenRefreshToken(w, r)
	default:
		WriteError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (h *Handler) tokenAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	codeVerifier := r.FormValue("code_verifier")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	store := h.tenantStore(r)
	ac, err := store.ConsumeAuthCode(code)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// Verify client
	if clientID != "" && clientID != ac.ClientID {
		WriteError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}

	// Verify redirect_uri
	if redirectURI != ac.RedirectURI {
		WriteError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	// PKCE verification
	if ac.CodeChallenge != "" {
		if codeVerifier == "" {
			WriteError(w, http.StatusBadRequest, "invalid_grant", "code_verifier required")
			return
		}
		if !VerifyPKCE(codeVerifier, ac.CodeChallenge, ac.CodeChallengeMethod) {
			WriteError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
			return
		}
	} else if clientSecret != "" {
		// Confidential client: verify client secret
		client, err := store.GetClient(ac.ClientID)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_grant", "unknown client")
			return
		}
		if !store.ValidateClientSecret(client, clientSecret) {
			WriteError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
			return
		}
	}

	user, err := store.GetUserByID(ac.UserID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "user not found")
		return
	}

	tenantID := tenant.FromContext(r.Context())
	accessToken, err := h.Tokens.CreateAccessToken(user, ac.ClientID, tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create access token")
		return
	}

	idToken, err := h.Tokens.CreateIDToken(user, ac.ClientID, ac.Nonce, tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create id token")
		return
	}

	refreshToken, err := store.CreateRefreshToken(ac.ClientID, user.ID, ac.Scope)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create refresh token")
		return
	}

	WriteJSON(w, http.StatusOK, TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: refreshToken,
		IDToken:      idToken,
	})
}

func (h *Handler) tokenRefreshToken(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("refresh_token")
	if token == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "refresh_token required")
		return
	}

	store := h.tenantStore(r)
	rt, err := store.ValidateRefreshToken(token)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// Revoke old refresh token
	store.RevokeRefreshToken(token)

	user, err := store.GetUserByID(rt.UserID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "user not found")
		return
	}

	audience := rt.ClientID
	if audience == "" {
		audience = h.Issuer
	}

	tenantID := tenant.FromContext(r.Context())
	accessToken, err := h.Tokens.CreateAccessToken(user, audience, tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create access token")
		return
	}

	idToken, err := h.Tokens.CreateIDToken(user, audience, "", tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create id token")
		return
	}

	newRefreshToken, err := store.CreateRefreshToken(rt.ClientID, user.ID, rt.Scope)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create refresh token")
		return
	}

	WriteJSON(w, http.StatusOK, TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: newRefreshToken,
		IDToken:      idToken,
	})
}

func (h *Handler) UserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		WriteError(w, http.StatusUnauthorized, "invalid_token", "Bearer token required")
		return
	}
	tokenStr := strings.TrimPrefix(auth, "Bearer ")

	claims, err := h.Tokens.ValidateAccessToken(tokenStr)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		return
	}

	sub, _ := claims["sub"].(string)
	store := h.tenantStore(r)
	user, err := store.GetUserByID(sub)
	if err != nil {
		WriteError(w, http.StatusNotFound, "invalid_request", "user not found")
		return
	}

	WriteJSON(w, http.StatusOK, UserInfoResponse{
		Sub:   user.ID,
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
	})
}

func (h *Handler) JWKS(w http.ResponseWriter, r *http.Request) {
	data, err := h.Tokens.JWKSBytes()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to generate JWKS")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (h *Handler) Discovery(w http.ResponseWriter, r *http.Request) {
	doc := OIDCDiscovery{
		Issuer:                            h.Issuer,
		AuthorizationEndpoint:             h.Issuer + "/authorize",
		TokenEndpoint:                     h.Issuer + "/token",
		UserinfoEndpoint:                  h.Issuer + "/userinfo",
		JwksURI:                           h.Issuer + "/jwks",
		RevocationEndpoint:                h.Issuer + "/revoke",
		RegistrationEndpoint:              h.Issuer + "/clients",
		ScopesSupported:                   []string{"openid", "profile", "email"},
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"RS256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	}
	WriteJSON(w, http.StatusOK, doc)
}

func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if err := r.ParseForm(); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	token := r.FormValue("token")
	if token == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "token required")
		return
	}

	// Per RFC 7009, revocation always returns 200 even if token doesn't exist
	store := h.tenantStore(r)
	store.RevokeRefreshToken(token)
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	var req struct {
		Name         string   `json:"name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}
	if len(req.RedirectURIs) == 0 {
		WriteError(w, http.StatusBadRequest, "invalid_request", "at least one redirect_uri required")
		return
	}

	store := h.tenantStore(r)
	client, secret, err := store.CreateClient(req.Name, req.RedirectURIs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to create client")
		return
	}

	WriteJSON(w, http.StatusCreated, ClientCreateResponse{
		ClientID:     client.ID,
		ClientSecret: secret,
		Name:         client.Name,
		RedirectURIs: client.RedirectURIs,
	})
}

// --- Admin ---

// CheckAdmin validates the Bearer token and checks the admin role.
// Returns the admin User and true on success, writes an error and returns false on failure.
// Used by both IAM and marketing handlers.
func CheckAdmin(tokens *TokenService, store *Store, w http.ResponseWriter, r *http.Request) (*User, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		WriteError(w, http.StatusUnauthorized, "invalid_token", "Bearer token required")
		return nil, false
	}
	tokenStr := strings.TrimPrefix(auth, "Bearer ")
	claims, err := tokens.ValidateAccessToken(tokenStr)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		return nil, false
	}
	role, _ := claims["role"].(string)
	if role != "admin" {
		WriteError(w, http.StatusForbidden, "insufficient_scope", "admin role required")
		return nil, false
	}

	// Validate tenant matches
	tokenTenantID, _ := claims["tid"].(string)
	requestTenantID := tenant.FromContext(r.Context())
	if tokenTenantID != requestTenantID {
		WriteError(w, http.StatusForbidden, "invalid_token", "token tenant mismatch")
		return nil, false
	}

	sub, _ := claims["sub"].(string)
	scopedStore := store.ForTenant(requestTenantID)
	user, err := scopedStore.GetUserByID(sub)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "invalid_token", "user not found")
		return nil, false
	}
	return user, true
}

func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) (*User, bool) {
	return CheckAdmin(h.Tokens, h.Store, w, r)
}

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	store := h.tenantStore(r)
	users, err := store.ListUsers()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to list users")
		return
	}
	WriteJSON(w, http.StatusOK, users)
}

func (h *Handler) AdminUserByID(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/admin/users/")
	if id == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "user id required")
		return
	}

	store := h.tenantStore(r)

	switch r.Method {
	case http.MethodGet:
		user, err := store.GetUserByID(id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		WriteJSON(w, http.StatusOK, user)

	case http.MethodPut:
		var req struct {
			Name string `json:"name"`
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if req.Role != "" && req.Role != "user" && req.Role != "admin" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "role must be 'user' or 'admin'")
			return
		}
		user, err := store.UpdateUser(id, req.Name, req.Role)
		if err != nil {
			WriteError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		WriteJSON(w, http.StatusOK, user)

	case http.MethodDelete:
		auth := r.Header.Get("Authorization")
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims, _ := h.Tokens.ValidateAccessToken(tokenStr)
		adminID, _ := claims["sub"].(string)

		if id == adminID {
			WriteError(w, http.StatusBadRequest, "invalid_request", "cannot delete yourself")
			return
		}
		if err := store.DeleteUser(id); err != nil {
			WriteError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminListClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	store := h.tenantStore(r)
	clients, err := store.ListClients()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server_error", "failed to list clients")
		return
	}
	WriteJSON(w, http.StatusOK, clients)
}

func (h *Handler) AdminDeleteClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/clients/")
	if id == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "client id required")
		return
	}
	store := h.tenantStore(r)
	if err := store.DeleteClient(id); err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "client not found")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func isValidRedirectURI(client *Client, uri string) bool {
	for _, allowed := range client.RedirectURIs {
		if allowed == uri {
			return true
		}
	}
	return false
}

// Activate handles the invite activation flow (public, no auth).
// GET  /activate/{token} — renders an HTML password form.
// POST /activate/{token} — sets the password and creates the User account.
func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/activate/")
	if token == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "token required")
		return
	}

	store := h.tenantStore(r)

	switch r.Method {
	case http.MethodGet:
		email, activated, err := store.GetContactByInviteToken(token)
		if err != nil || activated {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`<!DOCTYPE html><html><head><title>launch-kit</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}form{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);width:320px;text-align:center}h2{margin-top:0}</style>
</head><body><form><h2>Invalid or expired invite link.</h2></form></body></html>`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Activate Account - launch-kit</title>
<style>
body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}
form{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);width:320px}
h2{margin-top:0;text-align:center}
label{display:block;margin-top:1rem;font-size:0.9rem;color:#333}
input[type=password]{width:100%%;padding:0.5rem;margin-top:0.25rem;border:1px solid #ccc;border-radius:4px;box-sizing:border-box}
button{width:100%%;padding:0.75rem;margin-top:1.5rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:1rem}
button:hover{background:#1d4ed8}
</style></head><body>
<form method="POST">
<h2>Activate Account</h2>
<p style="text-align:center;color:#666;font-size:0.9rem">%s</p>
<label>Password</label><input type="password" name="password" required minlength="8" autofocus>
<label>Confirm Password</label><input type="password" name="confirm" required minlength="8">
<button type="submit">Activate</button>
</form></body></html>`, escapeHTML(email))

	case http.MethodPost:
		var password string
		ct := r.Header.Get("Content-Type")
		isJSON := strings.Contains(ct, "application/json")
		if isJSON {
			var req struct {
				Password string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
				return
			}
			password = req.Password
		} else {
			if err := r.ParseForm(); err != nil {
				WriteError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
				return
			}
			password = r.FormValue("password")
			confirm := r.FormValue("confirm")
			if password != confirm {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`<!DOCTYPE html><html><head><title>launch-kit</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}div{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);width:320px;text-align:center}</style>
</head><body><div><h2>Passwords do not match</h2><p><a href="javascript:history.back()">Try again</a></p></div></body></html>`))
				return
			}
		}

		if len(password) < 8 {
			if isJSON {
				WriteError(w, http.StatusBadRequest, "invalid_request", "password must be at least 8 characters")
			} else {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`<!DOCTYPE html><html><head><title>launch-kit</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}div{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);width:320px;text-align:center}</style>
</head><body><div><h2>Password too short</h2><p>Password must be at least 8 characters.</p><p><a href="javascript:history.back()">Try again</a></p></div></body></html>`))
			}
			return
		}

		user, err := store.ActivateContact(token, password)
		if err != nil {
			if isJSON {
				WriteError(w, http.StatusNotFound, "not_found", err.Error())
			} else {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`<!DOCTYPE html><html><head><title>launch-kit</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}div{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);width:320px;text-align:center}</style>
</head><body><div><h2>Invalid or expired invite link.</h2></div></body></html>`))
			}
			return
		}

		if isJSON {
			WriteJSON(w, http.StatusOK, map[string]string{"status": "activated", "user_id": user.ID})
		} else {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Account Activated - launch-kit</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}div{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);width:320px;text-align:center}</style>
</head><body><div>
<h2>Account Activated</h2>
<p>Your account has been created. You can now sign in.</p>
</div></body></html>`))
		}

	default:
		WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}
