package iam

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "modernc.org/sqlite"
)

// newTestDBWithKid creates a test DB with the kid column on the keys table.
func newTestDBWithKid(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	PRAGMA foreign_keys = ON;
	CREATE TABLE users (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, password_hash TEXT NOT NULL,
		name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user', created_at DATETIME NOT NULL,
		reset_token TEXT, reset_token_expires_at DATETIME,
		UNIQUE(tenant_id, email)
	);
	CREATE TABLE clients (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', secret_hash TEXT NOT NULL, name TEXT NOT NULL,
		redirect_uris TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE auth_codes (
		code TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL, user_id TEXT NOT NULL,
		redirect_uri TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '', nonce TEXT NOT NULL DEFAULT '',
		code_challenge TEXT NOT NULL DEFAULT '', code_challenge_method TEXT NOT NULL DEFAULT '',
		expires_at DATETIME NOT NULL, used INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE refresh_tokens (
		token TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL, user_id TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL, revoked INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE keys (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', kid TEXT NOT NULL DEFAULT '',
		private_key_pem TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE contacts (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
		user_id TEXT REFERENCES users(id), unsubscribed INTEGER NOT NULL DEFAULT 0,
		unsubscribe_token TEXT UNIQUE NOT NULL, invite_token TEXT UNIQUE, invite_token_expires_at DATETIME,
		consent_source TEXT NOT NULL, consent_at DATETIME NOT NULL, created_at DATETIME NOT NULL,
		UNIQUE(tenant_id, email)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

// --- Encryption at rest ---

func TestKeyEncryptionAtRest(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")

	encKey := make([]byte, 32)
	if _, err := rand.Read(encKey); err != nil {
		t.Fatal(err)
	}
	store.SetEncryptionKey(encKey)

	if _, err := store.LoadOrCreateRSAKey(); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := db.QueryRow("SELECT private_key_pem FROM keys WHERE tenant_id = ?", "test-tenant").Scan(&stored); err != nil {
		t.Fatal(err)
	}

	// Encrypted data must not be a PEM block.
	block, _ := pem.Decode([]byte(stored))
	if block != nil {
		t.Error("stored value is PEM: expected encrypted ciphertext")
	}

	// Must be valid base64 (our chosen encoding for ciphertext).
	if _, err := base64.StdEncoding.DecodeString(stored); err != nil {
		t.Errorf("stored value is not base64: %v", err)
	}
}

func TestKeyEncryptionRoundTrip(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")

	encKey := make([]byte, 32)
	if _, err := rand.Read(encKey); err != nil {
		t.Fatal(err)
	}
	store.SetEncryptionKey(encKey)

	key1, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}

	// Second call must decrypt from DB and return the same key.
	key2, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}

	if key1.D.Cmp(key2.D) != 0 {
		t.Error("decrypted key differs from original")
	}
}

func TestKeyEncryptionMigration(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")

	// Store a plaintext PEM key directly (simulates a pre-migration row).
	rawKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rawKey),
	})
	if _, err := db.Exec(
		"INSERT INTO keys (id, tenant_id, kid, private_key_pem, created_at) VALUES ('migkey', 'test-tenant', 'old-kid', ?, ?)",
		string(pemBytes), time.Now().UTC(),
	); err != nil {
		t.Fatal(err)
	}

	// Now attach an encryption key and load — should transparently re-encrypt.
	encKey := make([]byte, 32)
	if _, err := rand.Read(encKey); err != nil {
		t.Fatal(err)
	}
	store.SetEncryptionKey(encKey)

	loaded, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.D.Cmp(rawKey.D) != 0 {
		t.Error("migrated key differs from original plaintext key")
	}

	// The DB row must now contain ciphertext, not PEM.
	var stored string
	if err := db.QueryRow("SELECT private_key_pem FROM keys WHERE tenant_id = ?", "test-tenant").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(stored))
	if block != nil {
		t.Error("after migration the value is still PEM; expected encrypted ciphertext")
	}
}

func TestKeyNoEncryptionWithoutKey(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")
	// No SetEncryptionKey call — backward-compat mode.

	if _, err := store.LoadOrCreateRSAKey(); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := db.QueryRow("SELECT private_key_pem FROM keys WHERE tenant_id = ?", "test-tenant").Scan(&stored); err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode([]byte(stored))
	if block == nil {
		t.Error("without encryption key the stored value must be plaintext PEM")
	}
}

// --- Key size ---

func TestNewKeySize3072(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")
	store.KeySize = 3072

	key, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	if key.N.BitLen() != 3072 {
		t.Errorf("key bit length = %d, want 3072", key.N.BitLen())
	}
}

// --- Key rotation ---

func TestRotateKeyGeneratesNewKid(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")

	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}
	first, err := store.LoadActiveKeys("test-tenant")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("no keys after first rotation")
	}
	firstKid := first[len(first)-1].Kid

	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadActiveKeys("test-tenant")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) < 2 {
		t.Fatalf("expected at least 2 active keys, got %d", len(second))
	}
	newestKid := second[len(second)-1].Kid

	if newestKid == firstKid {
		t.Errorf("kid did not change after rotation: both are %q", firstKid)
	}
}

func TestJWKSReturnsMultipleKeysDuringGracePeriod(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")

	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}
	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}

	keys, err := store.LoadActiveKeys("test-tenant")
	if err != nil {
		t.Fatal(err)
	}

	ts := NewTokenServiceMultiKey(keys, "http://test-issuer")
	jwks := ts.JWKS()

	keysSlice, ok := jwks["keys"].([]map[string]interface{})
	if !ok {
		// Try the alternate type that json.Marshal roundtrip produces.
		raw, _ := json.Marshal(jwks)
		var parsed struct {
			Keys []map[string]interface{} `json:"keys"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatal(err)
		}
		keysSlice = parsed.Keys
	}

	if len(keysSlice) < 2 {
		t.Errorf("JWKS contains %d key(s), expected >= 2 during grace period", len(keysSlice))
	}
}

func TestNewTokensSignedWithNewestKey(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")

	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}
	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}

	keys, err := store.LoadActiveKeys("test-tenant")
	if err != nil {
		t.Fatal(err)
	}

	ts := NewTokenServiceMultiKey(keys, "http://test-issuer")
	user := &User{ID: "u1", Email: "u@example.com", Name: "U", Role: "user"}
	tokenStr, err := ts.CreateAccessToken(user, "aud", "test-tenant")
	if err != nil {
		t.Fatal(err)
	}

	// Parse without verification to inspect the header.
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		t.Fatal("token does not have 3 parts")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]interface{}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatal(err)
	}

	newestKid := keys[len(keys)-1].Kid
	if header["kid"] != newestKid {
		t.Errorf("token kid = %q, want newest kid %q", header["kid"], newestKid)
	}
}

func TestOldTokenVerifiesDuringGracePeriod(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")

	// Create initial key and sign a token with it.
	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}
	firstKeys, err := store.LoadActiveKeys("test-tenant")
	if err != nil {
		t.Fatal(err)
	}
	tsFirst := NewTokenServiceMultiKey(firstKeys, "http://test-issuer")
	user := &User{ID: "u1", Email: "u@example.com", Name: "U", Role: "user"}
	oldToken, err := tsFirst.CreateAccessToken(user, "aud", "test-tenant")
	if err != nil {
		t.Fatal(err)
	}

	// Rotate: the first key is still within the grace period.
	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}
	allKeys, err := store.LoadActiveKeys("test-tenant")
	if err != nil {
		t.Fatal(err)
	}
	tsMulti := NewTokenServiceMultiKey(allKeys, "http://test-issuer")

	if _, err := tsMulti.ValidateAccessToken(oldToken, "aud"); err != nil {
		t.Errorf("old token should still validate during grace period: %v", err)
	}
}

func TestExpiredKeyRemovedFromJWKS(t *testing.T) {
	db := newTestDBWithKid(t)
	store := NewStore(db).ForTenant("test-tenant")
	store.GracePeriod = 1 * time.Millisecond // very short grace period for testing

	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}

	// Wait for grace period to elapse.
	time.Sleep(5 * time.Millisecond)

	if err := store.RotateKey("test-tenant"); err != nil {
		t.Fatal(err)
	}

	keys, err := store.LoadActiveKeys("test-tenant")
	if err != nil {
		t.Fatal(err)
	}

	ts := NewTokenServiceMultiKey(keys, "http://test-issuer")
	raw, _ := json.Marshal(ts.JWKS())
	var parsed struct {
		Keys []map[string]interface{} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}

	// Only the new key (created after grace expiry) must appear.
	if len(parsed.Keys) != 1 {
		t.Errorf("JWKS has %d key(s), want exactly 1 after grace period expired", len(parsed.Keys))
	}
}

// --- Admin endpoint ---

func newHandlerEnvWithKid(t *testing.T) *testEnv {
	t.Helper()
	db := newTestDBWithKid(t)

	store := NewStore(db)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tokens := NewTokenService(key, "http://test-issuer")
	registry := NewStaticTokenRegistry(tokens)
	h := NewHandler(store, registry, "http://test-issuer")

	mux := http.NewServeMux()
	mux.HandleFunc("/login", h.Login)
	mux.HandleFunc("/admin/keys/rotate", h.AdminRotateKey)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })

	return &testEnv{srv: srv, store: store, tokens: tokens}
}

func TestRotateKeyEndpoint(t *testing.T) {
	env := newHandlerEnvWithKid(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/keys/rotate", tok, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	kid, _ := body["kid"].(string)
	if kid == "" {
		t.Error("response missing non-empty kid")
	}
}

func TestRotateKeyEndpointRequiresAdmin(t *testing.T) {
	env := newHandlerEnvWithKid(t)

	// No token at all.
	resp := doReq(t, env, "POST", "/admin/keys/rotate", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf("no-token status = %d, want 401 or 403", resp.StatusCode)
	}
}

func TestRotateKeyEndpointNonAdminForbidden(t *testing.T) {
	env := newHandlerEnvWithKid(t)

	// Create a regular user and get their token.
	env.store.CreateUser("plain@example.com", "pass12345", "Plain")
	resp := postJSON(t, env, "/login", `{"email":"plain@example.com","password":"pass12345"}`)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()

	resp2 := doReq(t, env, "POST", "/admin/keys/rotate", tok.AccessToken, "")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden && resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("non-admin status = %d, want 403 or 401", resp2.StatusCode)
	}
}

// --- JWT kid helper used in multi-key tests ---

func tokenKid(t *testing.T, tokenStr string) string {
	t.Helper()
	// jwt.ParseUnverified is the correct approach for inspecting headers without verification.
	p := jwt.NewParser()
	tok, _, err := p.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	kid, _ := tok.Header["kid"].(string)
	return kid
}
