package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type Handler struct {
	store    *Store
	tokens   *TokenService
	issuer   string
}

func NewHandler(store *Store, tokens *TokenService, issuer string) *Handler {
	return &Handler{store: store, tokens: tokens, issuer: issuer}
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err, desc string) {
	writeJSON(w, status, ErrorResponse{Error: err, ErrorDescription: desc})
}

var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// --- Handlers ---

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if !emailRegex.MatchString(req.Email) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid email format")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "password required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}

	user, err := h.store.CreateUser(req.Email, req.Password, req.Name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "invalid_request", "email already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create user")
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	user, err := h.store.AuthenticateUser(req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_grant", "invalid credentials")
		return
	}

	accessToken, err := h.tokens.CreateAccessToken(user, h.issuer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create access token")
		return
	}

	idToken, err := h.tokens.CreateIDToken(user, h.issuer, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create id token")
		return
	}

	refreshToken, err := h.store.CreateRefreshToken("", user.ID, "openid profile email")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create refresh token")
		return
	}

	writeJSON(w, http.StatusOK, TokenResponse{
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
<html><head><title>Login - mini-iam</title>
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
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
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

	// Validate client
	client, err := h.store.GetClient(clientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "unknown client_id")
		return
	}

	// Validate redirect_uri
	if !isValidRedirectURI(client, redirectURI) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid redirect_uri")
		return
	}

	// Authenticate user
	user, err := h.store.AuthenticateUser(email, password)
	if err != nil {
		// Re-render login form with error (minimal approach: redirect back)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`<!DOCTYPE html><html><body><p>Invalid credentials. <a href="javascript:history.back()">Try again</a></p></body></html>`))
		return
	}

	// Generate auth code
	code, err := h.store.CreateAuthCode(clientID, user.ID, redirectURI, scope, nonce, codeChallenge, codeChallengeMethod)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create authorization code")
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
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.tokenAuthorizationCode(w, r)
	case "refresh_token":
		h.tokenRefreshToken(w, r)
	default:
		writeError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (h *Handler) tokenAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	codeVerifier := r.FormValue("code_verifier")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	ac, err := h.store.ConsumeAuthCode(code)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// Verify client
	if clientID != "" && clientID != ac.ClientID {
		writeError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}

	// Verify redirect_uri
	if redirectURI != ac.RedirectURI {
		writeError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	// PKCE verification
	if ac.CodeChallenge != "" {
		if codeVerifier == "" {
			writeError(w, http.StatusBadRequest, "invalid_grant", "code_verifier required")
			return
		}
		if !VerifyPKCE(codeVerifier, ac.CodeChallenge, ac.CodeChallengeMethod) {
			writeError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
			return
		}
	} else if clientSecret != "" {
		// Confidential client: verify client secret
		client, err := h.store.GetClient(ac.ClientID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_grant", "unknown client")
			return
		}
		if !h.store.ValidateClientSecret(client, clientSecret) {
			writeError(w, http.StatusUnauthorized, "invalid_client", "invalid client credentials")
			return
		}
	}

	user, err := h.store.GetUserByID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "user not found")
		return
	}

	accessToken, err := h.tokens.CreateAccessToken(user, ac.ClientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create access token")
		return
	}

	idToken, err := h.tokens.CreateIDToken(user, ac.ClientID, ac.Nonce)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create id token")
		return
	}

	refreshToken, err := h.store.CreateRefreshToken(ac.ClientID, user.ID, ac.Scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create refresh token")
		return
	}

	writeJSON(w, http.StatusOK, TokenResponse{
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
		writeError(w, http.StatusBadRequest, "invalid_request", "refresh_token required")
		return
	}

	rt, err := h.store.ValidateRefreshToken(token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// Revoke old refresh token
	h.store.RevokeRefreshToken(token)

	user, err := h.store.GetUserByID(rt.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "user not found")
		return
	}

	audience := rt.ClientID
	if audience == "" {
		audience = h.issuer
	}

	accessToken, err := h.tokens.CreateAccessToken(user, audience)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create access token")
		return
	}

	idToken, err := h.tokens.CreateIDToken(user, audience, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create id token")
		return
	}

	newRefreshToken, err := h.store.CreateRefreshToken(rt.ClientID, user.ID, rt.Scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create refresh token")
		return
	}

	writeJSON(w, http.StatusOK, TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: newRefreshToken,
		IDToken:      idToken,
	})
}

func (h *Handler) UserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "invalid_token", "Bearer token required")
		return
	}
	tokenStr := strings.TrimPrefix(auth, "Bearer ")

	claims, err := h.tokens.ValidateAccessToken(tokenStr)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		return
	}

	sub, _ := claims["sub"].(string)
	user, err := h.store.GetUserByID(sub)
	if err != nil {
		writeError(w, http.StatusNotFound, "invalid_request", "user not found")
		return
	}

	writeJSON(w, http.StatusOK, UserInfoResponse{
		Sub:   user.ID,
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
	})
}

func (h *Handler) JWKS(w http.ResponseWriter, r *http.Request) {
	data, err := h.tokens.JWKSBytes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to generate JWKS")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (h *Handler) Discovery(w http.ResponseWriter, r *http.Request) {
	doc := OIDCDiscovery{
		Issuer:                           h.issuer,
		AuthorizationEndpoint:            h.issuer + "/authorize",
		TokenEndpoint:                    h.issuer + "/token",
		UserinfoEndpoint:                 h.issuer + "/userinfo",
		JwksURI:                          h.issuer + "/jwks",
		RevocationEndpoint:               h.issuer + "/revoke",
		RegistrationEndpoint:             h.issuer + "/clients",
		ScopesSupported:                  []string{"openid", "profile", "email"},
		ResponseTypesSupported:           []string{"code"},
		GrantTypesSupported:              []string{"authorization_code", "refresh_token"},
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: []string{"RS256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "none"},
		CodeChallengeMethodsSupported:    []string{"S256"},
	}
	writeJSON(w, http.StatusOK, doc)
}

func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	token := r.FormValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token required")
		return
	}

	// Per RFC 7009, revocation always returns 200 even if token doesn't exist
	h.store.RevokeRefreshToken(token)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	var req struct {
		Name         string   `json:"name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name required")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "at least one redirect_uri required")
		return
	}

	client, secret, err := h.store.CreateClient(req.Name, req.RedirectURIs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to create client")
		return
	}

	writeJSON(w, http.StatusCreated, ClientCreateResponse{
		ClientID:     client.ID,
		ClientSecret: secret,
		Name:         client.Name,
		RedirectURIs: client.RedirectURIs,
	})
}

// --- Admin ---

func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) (*User, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "invalid_token", "Bearer token required")
		return nil, false
	}
	tokenStr := strings.TrimPrefix(auth, "Bearer ")
	claims, err := h.tokens.ValidateAccessToken(tokenStr)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		return nil, false
	}
	role, _ := claims["role"].(string)
	if role != "admin" {
		writeError(w, http.StatusForbidden, "insufficient_scope", "admin role required")
		return nil, false
	}
	sub, _ := claims["sub"].(string)
	user, err := h.store.GetUserByID(sub)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "user not found")
		return nil, false
	}
	return user, true
}

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	users, err := h.store.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) AdminUserByID(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/admin/users/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "user id required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		user, err := h.store.GetUserByID(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeJSON(w, http.StatusOK, user)

	case http.MethodPut:
		var req struct {
			Name string `json:"name"`
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if req.Role != "" && req.Role != "user" && req.Role != "admin" {
			writeError(w, http.StatusBadRequest, "invalid_request", "role must be 'user' or 'admin'")
			return
		}
		user, err := h.store.UpdateUser(id, req.Name, req.Role)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeJSON(w, http.StatusOK, user)

	case http.MethodDelete:
		auth := r.Header.Get("Authorization")
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims, _ := h.tokens.ValidateAccessToken(tokenStr)
		adminID, _ := claims["sub"].(string)

		if id == adminID {
			writeError(w, http.StatusBadRequest, "invalid_request", "cannot delete yourself")
			return
		}
		if err := h.store.DeleteUser(id); err != nil {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminListClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	clients, err := h.store.ListClients()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "failed to list clients")
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (h *Handler) AdminDeleteClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/clients/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "client id required")
		return
	}
	if err := h.store.DeleteClient(id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "client not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func isValidRedirectURI(client *Client, uri string) bool {
	for _, allowed := range client.RedirectURIs {
		if allowed == uri {
			return true
		}
	}
	return false
}

