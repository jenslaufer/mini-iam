package iam

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "modernc.org/sqlite"
)


// ============================================================
// STUBS — remove once production code implements KeyManager.
// These define the expected API surface for key management.
// ============================================================

// KeyManager handles RSA key lifecycle: encryption at rest,
// minimum key size enforcement, rotation with JWKS kid support.
// STUB: all methods return errNotImplemented.
type KeyManager struct {
	store         *Store
	encryptionKey []byte        // AES-GCM key from env; nil = no encryption
	gracePeriod   time.Duration // how long old keys remain in JWKS
}

var errNotImplemented = fmt.Errorf("KeyManager: not implemented")

func NewKeyManager(store *Store, encryptionKey []byte, gracePeriod time.Duration) *KeyManager {
	return &KeyManager{store: store, encryptionKey: encryptionKey, gracePeriod: gracePeriod}
}

// LoadOrCreateKey returns the current signing key, its kid, and an error.
// Keys are encrypted at rest when encryptionKey is set.
// Generated keys must be >= 3072 bits.
func (km *KeyManager) LoadOrCreateKey(tenantID string) (*rsa.PrivateKey, string, error) {
	return nil, "", errNotImplemented
}

// RotateKey generates a new key pair for the tenant, assigns a new kid,
// and keeps the old key available for the grace period.
func (km *KeyManager) RotateKey(tenantID string) error {
	return errNotImplemented
}

// JWKS returns the JSON Web Key Set for the tenant, including keys
// within the grace period.
func (km *KeyManager) JWKS(tenantID string) (map[string]interface{}, error) {
	return nil, errNotImplemented
}

// RemoveExpiredKeys removes keys past their grace period.
func (km *KeyManager) RemoveExpiredKeys(tenantID string) error {
	return errNotImplemented
}

// EncryptPEM encrypts a PEM-encoded private key using AES-GCM.
func EncryptPEM(plainPEM []byte, aesKey []byte) ([]byte, error) {
	return nil, errNotImplemented
}

// DecryptPEM decrypts an AES-GCM encrypted PEM block.
func DecryptPEM(ciphertext []byte, aesKey []byte) ([]byte, error) {
	return nil, errNotImplemented
}

// ============================================================
// Helpers
// ============================================================

func newKeyTestDB(t *testing.T) *Store {
	t.Helper()
	db := newTestDB(t)
	// Extend keys table for rotation support (kid, expires_at).
	// The production migration should add these columns.
	db.Exec(`ALTER TABLE keys ADD COLUMN kid TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE keys ADD COLUMN expires_at DATETIME`)
	return NewStore(db)
}

func testAESKey() []byte {
	// 32-byte key for AES-256-GCM (deterministic for tests).
	return []byte("test-encryption-key-32bytes!!")[:32]
}

// ============================================================
// Key Encryption at Rest (H-2)
// ============================================================

func TestKeyEncryptionAtRest(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, testAESKey(), 24*time.Hour)

	key, _, err := km.LoadOrCreateKey("tenant-enc")
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}

	// Read raw DB value — must NOT be plaintext PEM.
	var raw string
	err = store.DB().QueryRow(
		"SELECT private_key_pem FROM keys WHERE tenant_id = ?", "tenant-enc",
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query raw key: %v", err)
	}

	if strings.Contains(raw, "BEGIN RSA PRIVATE KEY") {
		t.Error("raw DB value contains plaintext PEM — key is NOT encrypted at rest")
	}

	block, _ := pem.Decode([]byte(raw))
	if block != nil {
		t.Error("raw DB value is valid PEM — key is stored unencrypted")
	}
}

func TestKeyDecryptionRoundTrip(t *testing.T) {
	store := newKeyTestDB(t)
	aesKey := testAESKey()
	km := NewKeyManager(store, aesKey, 24*time.Hour)

	// Create and store.
	key1, kid1, err := km.LoadOrCreateKey("tenant-rt")
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	// Load again — must return identical key material.
	key2, kid2, err := km.LoadOrCreateKey("tenant-rt")
	if err != nil {
		t.Fatalf("reload key: %v", err)
	}

	if key1.D.Cmp(key2.D) != 0 {
		t.Error("round-tripped key has different private exponent")
	}
	if kid1 != kid2 {
		t.Errorf("kid changed on reload: %q vs %q", kid1, kid2)
	}
}

func TestKeyEncryptionWithoutEnvKey(t *testing.T) {
	store := newKeyTestDB(t)
	// nil encryption key = backward-compatible unencrypted storage.
	km := NewKeyManager(store, nil, 24*time.Hour)

	_, _, err := km.LoadOrCreateKey("tenant-plain")
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}

	var raw string
	err = store.DB().QueryRow(
		"SELECT private_key_pem FROM keys WHERE tenant_id = ?", "tenant-plain",
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if !strings.Contains(raw, "BEGIN RSA PRIVATE KEY") {
		t.Error("without encryption key, PEM should be stored as plaintext")
	}
}

func TestKeyEncryptionWrongKey(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, testAESKey(), 24*time.Hour)

	_, _, err := km.LoadOrCreateKey("tenant-wk")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Attempt to load with a different AES key.
	wrongKey := []byte("wrong-key-wrong-key-wrong-key!!")[:32]
	km2 := NewKeyManager(store, wrongKey, 24*time.Hour)

	_, _, err = km2.LoadOrCreateKey("tenant-wk")
	if err == nil {
		t.Error("expected error when decrypting with wrong key, got nil")
	}
}

func TestKeyEncryptionMigration(t *testing.T) {
	store := newKeyTestDB(t)

	// Simulate legacy: store an unencrypted key directly.
	legacyKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	legacyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(legacyKey),
	})
	_, err = store.DB().Exec(
		"INSERT INTO keys (id, tenant_id, kid, private_key_pem, created_at) VALUES (?, ?, ?, ?, ?)",
		"legacy-id", "tenant-mig", "old-kid", string(legacyPEM), time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("insert legacy key: %v", err)
	}

	// Load with encryption enabled — should transparently re-encrypt.
	km := NewKeyManager(store, testAESKey(), 24*time.Hour)
	loaded, _, err := km.LoadOrCreateKey("tenant-mig")
	if err != nil {
		t.Fatalf("load legacy key: %v", err)
	}
	if loaded.D.Cmp(legacyKey.D) != 0 {
		t.Error("loaded key doesn't match original legacy key")
	}

	// Verify DB now contains encrypted data.
	var raw string
	store.DB().QueryRow(
		"SELECT private_key_pem FROM keys WHERE tenant_id = ?", "tenant-mig",
	).Scan(&raw)
	if strings.Contains(raw, "BEGIN RSA PRIVATE KEY") {
		t.Error("after migration load, key should be encrypted in DB")
	}
}

// ============================================================
// Key Size (H-3)
// ============================================================

func TestNewKeyMinimumSize3072(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	key, _, err := km.LoadOrCreateKey("tenant-size")
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}

	bits := key.N.BitLen()
	if bits < 3072 {
		t.Errorf("key size = %d bits, want >= 3072", bits)
	}
}

func TestRejectSmallKeySize(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	// Insert a 2048-bit key directly.
	smallKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	smallPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(smallKey),
	})
	_, err = store.DB().Exec(
		"INSERT INTO keys (id, tenant_id, kid, private_key_pem, created_at) VALUES (?, ?, ?, ?, ?)",
		"small-id", "tenant-small", "small-kid", string(smallPEM), time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = km.LoadOrCreateKey("tenant-small")
	if err == nil {
		t.Error("expected error loading <3072-bit key, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "3072") {
		t.Errorf("error should mention 3072-bit requirement, got: %v", err)
	}
}

func TestKeyBitSizeExact(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	key, _, err := km.LoadOrCreateKey("tenant-exact")
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}

	bits := key.N.BitLen()
	if bits != 3072 {
		t.Errorf("key size = %d bits, want exactly 3072", bits)
	}
}

// ============================================================
// Key Rotation (H-4)
// ============================================================

func TestRotateKeyGeneratesNewKid(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	_, oldKid, err := km.LoadOrCreateKey("tenant-rot")
	if err != nil {
		t.Fatalf("initial key: %v", err)
	}

	if err := km.RotateKey("tenant-rot"); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	_, newKid, err := km.LoadOrCreateKey("tenant-rot")
	if err != nil {
		t.Fatalf("after rotation: %v", err)
	}

	if oldKid == newKid {
		t.Errorf("kid unchanged after rotation: %q", oldKid)
	}
}

func TestJWKSContainsMultipleKeysDuringGracePeriod(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	_, _, err := km.LoadOrCreateKey("tenant-jwks")
	if err != nil {
		t.Fatal(err)
	}

	if err := km.RotateKey("tenant-jwks"); err != nil {
		t.Fatal(err)
	}

	jwks, err := km.JWKS("tenant-jwks")
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}

	keys, ok := jwks["keys"].([]map[string]interface{})
	if !ok {
		// Try type assertion with []interface{} as fallback.
		rawKeys, ok2 := jwks["keys"].([]interface{})
		if !ok2 {
			t.Fatalf("JWKS keys field has unexpected type: %T", jwks["keys"])
		}
		keys = make([]map[string]interface{}, len(rawKeys))
		for i, k := range rawKeys {
			keys[i] = k.(map[string]interface{})
		}
	}

	if len(keys) < 2 {
		t.Errorf("JWKS has %d key(s) during grace period, want >= 2", len(keys))
	}

	// Verify kids are distinct.
	kids := map[string]bool{}
	for _, k := range keys {
		kid, _ := k["kid"].(string)
		if kid == "" {
			t.Error("JWKS key missing kid")
		}
		kids[kid] = true
	}
	if len(kids) < 2 {
		t.Error("JWKS keys should have distinct kids")
	}
}

func TestNewTokensSignedWithNewestKey(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	_, _, err := km.LoadOrCreateKey("tenant-new")
	if err != nil {
		t.Fatal(err)
	}

	if err := km.RotateKey("tenant-new"); err != nil {
		t.Fatal(err)
	}

	key, kid, err := km.LoadOrCreateKey("tenant-new")
	if err != nil {
		t.Fatal(err)
	}

	ts := NewTokenService(key, "http://test")
	user := &User{ID: "u1", Email: "a@b.com", Name: "A", Role: "user"}
	tokenStr, err := ts.CreateAccessToken(user, "aud", "tenant-new")
	if err != nil {
		t.Fatal(err)
	}

	// Parse without verification to check kid header.
	parser := jwt.NewParser()
	parsed, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}

	tokenKid, _ := parsed.Header["kid"].(string)
	if tokenKid != kid {
		t.Errorf("token kid = %q, want newest kid %q", tokenKid, kid)
	}
}

func TestOldTokenVerifiesDuringGracePeriod(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	oldKey, _, err := km.LoadOrCreateKey("tenant-grace")
	if err != nil {
		t.Fatal(err)
	}

	// Sign a token with the old key.
	oldTS := NewTokenService(oldKey, "http://test")
	user := &User{ID: "u1", Email: "a@b.com", Name: "A", Role: "user"}
	oldToken, err := oldTS.CreateAccessToken(user, "aud", "tenant-grace")
	if err != nil {
		t.Fatal(err)
	}

	// Rotate.
	if err := km.RotateKey("tenant-grace"); err != nil {
		t.Fatal(err)
	}

	// Get JWKS — should contain both keys.
	jwks, err := km.JWKS("tenant-grace")
	if err != nil {
		t.Fatal(err)
	}

	jwksBytes, _ := json.Marshal(jwks)

	// Build a key function that resolves kid from JWKS.
	_, err = jwt.Parse(oldToken, func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		return findPublicKeyInJWKS(t, jwksBytes, kid)
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		t.Errorf("old token should verify during grace period: %v", err)
	}
}

func TestExpiredKeyRemovedFromJWKS(t *testing.T) {
	store := newKeyTestDB(t)
	// Use a very short grace period.
	km := NewKeyManager(store, nil, 1*time.Millisecond)

	_, oldKid, err := km.LoadOrCreateKey("tenant-exp")
	if err != nil {
		t.Fatal(err)
	}

	if err := km.RotateKey("tenant-exp"); err != nil {
		t.Fatal(err)
	}

	// Wait for grace period to expire.
	time.Sleep(10 * time.Millisecond)

	if err := km.RemoveExpiredKeys("tenant-exp"); err != nil {
		t.Fatal(err)
	}

	jwks, err := km.JWKS("tenant-exp")
	if err != nil {
		t.Fatal(err)
	}

	jwksBytes, _ := json.Marshal(jwks)
	if strings.Contains(string(jwksBytes), oldKid) {
		t.Errorf("expired kid %q still in JWKS after grace period", oldKid)
	}

	// Should have exactly 1 key.
	rawKeys, _ := jwks["keys"].([]interface{})
	if rawKeys == nil {
		keys, _ := jwks["keys"].([]map[string]interface{})
		if len(keys) != 1 {
			t.Errorf("JWKS has %d keys after expiry, want 1", len(keys))
		}
	} else if len(rawKeys) != 1 {
		t.Errorf("JWKS has %d keys after expiry, want 1", len(rawKeys))
	}
}

func TestRotateKeyEndpointRequiresAdmin(t *testing.T) {
	env := newHandlerEnv(t)

	// Register a normal user.
	postJSON(t, env, "/register", `{"email":"user@test.com","password":"password123","name":"User"}`)
	resp := postJSON(t, env, "/login", `{"email":"user@test.com","password":"password123"}`)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()

	resp = doReq(t, env, "POST", "/admin/rotate-keys", tok.AccessToken, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin rotate: status = %d, want 403", resp.StatusCode)
	}
}

func TestRotateKeyEndpointWorks(t *testing.T) {
	env := newHandlerEnv(t)
	token := adminToken(t, env)

	// Get JWKS before rotation.
	resp1 := doReq(t, env, "GET", "/jwks", "", "")
	jwks1 := readJSON(t, resp1)

	// Trigger rotation.
	resp := doReq(t, env, "POST", "/admin/rotate-keys", token, "")
	if resp.StatusCode != http.StatusOK {
		body := readJSON(t, resp)
		t.Fatalf("admin rotate: status = %d, body = %v", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Get JWKS after rotation.
	resp2 := doReq(t, env, "GET", "/jwks", "", "")
	jwks2 := readJSON(t, resp2)

	keys1 := jwksKids(t, jwks1)
	keys2 := jwksKids(t, jwks2)

	if len(keys2) < 2 {
		t.Errorf("JWKS after rotation has %d key(s), want >= 2", len(keys2))
	}

	// New JWKS should contain all old kids plus a new one.
	for _, k := range keys1 {
		found := false
		for _, k2 := range keys2 {
			if k == k2 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("old kid %q missing from JWKS after rotation", k)
		}
	}
}

func TestConcurrentRotationSafe(t *testing.T) {
	store := newKeyTestDB(t)
	km := NewKeyManager(store, nil, 24*time.Hour)

	_, _, err := km.LoadOrCreateKey("tenant-conc")
	if err != nil {
		t.Fatal(err)
	}

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = km.RotateKey("tenant-conc")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("rotation %d failed: %v", i, err)
		}
	}

	// After concurrent rotations, JWKS should be consistent.
	jwks, err := km.JWKS("tenant-conc")
	if err != nil {
		t.Fatalf("JWKS after concurrent rotation: %v", err)
	}

	kids := jwksKidsFromMap(t, jwks)
	// Should have the original + n rotated keys.
	if len(kids) < 2 {
		t.Errorf("JWKS has %d keys, want >= 2 after concurrent rotations", len(kids))
	}

	// All kids must be unique (no corruption).
	seen := map[string]bool{}
	for _, k := range kids {
		if seen[k] {
			t.Errorf("duplicate kid %q — concurrent rotation corrupted state", k)
		}
		seen[k] = true
	}

	// Current key must be loadable.
	key, _, err := km.LoadOrCreateKey("tenant-conc")
	if err != nil {
		t.Fatalf("load after concurrent rotation: %v", err)
	}
	if key == nil {
		t.Fatal("nil key after concurrent rotation")
	}
}

// ============================================================
// Test helpers
// ============================================================

// findPublicKeyInJWKS extracts an RSA public key from JWKS JSON by kid.
func findPublicKeyInJWKS(t *testing.T, jwksJSON []byte, kid string) (*rsa.PublicKey, error) {
	t.Helper()
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
			Kty string `json:"kty"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(jwksJSON, &jwks); err != nil {
		return nil, fmt.Errorf("parse JWKS: %w", err)
	}
	for _, k := range jwks.Keys {
		if k.Kid == kid {
			// Decode n and e from base64url.
			nBytes, err := base64URLDecode(k.N)
			if err != nil {
				return nil, fmt.Errorf("decode n: %w", err)
			}
			eBytes, err := base64URLDecode(k.E)
			if err != nil {
				return nil, fmt.Errorf("decode e: %w", err)
			}
			var n, e big.Int
			n.SetBytes(nBytes)
			e.SetBytes(eBytes)
			return &rsa.PublicKey{N: &n, E: int(e.Int64())}, nil
		}
	}
	return nil, fmt.Errorf("kid %q not found in JWKS", kid)
}

func jwksKids(t *testing.T, jwks map[string]any) []string {
	t.Helper()
	rawKeys, ok := jwks["keys"].([]any)
	if !ok {
		t.Fatal("JWKS missing keys array")
	}
	var kids []string
	for _, rk := range rawKeys {
		k, ok := rk.(map[string]any)
		if !ok {
			continue
		}
		kid, _ := k["kid"].(string)
		if kid != "" {
			kids = append(kids, kid)
		}
	}
	return kids
}

func jwksKidsFromMap(t *testing.T, jwks map[string]interface{}) []string {
	t.Helper()
	rawKeys, ok := jwks["keys"].([]interface{})
	if !ok {
		// Try typed slice.
		typedKeys, ok2 := jwks["keys"].([]map[string]interface{})
		if !ok2 {
			t.Fatalf("JWKS keys unexpected type: %T", jwks["keys"])
		}
		var kids []string
		for _, k := range typedKeys {
			kid, _ := k["kid"].(string)
			if kid != "" {
				kids = append(kids, kid)
			}
		}
		return kids
	}
	var kids []string
	for _, rk := range rawKeys {
		k, _ := rk.(map[string]interface{})
		kid, _ := k["kid"].(string)
		if kid != "" {
			kids = append(kids, kid)
		}
	}
	return kids
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
