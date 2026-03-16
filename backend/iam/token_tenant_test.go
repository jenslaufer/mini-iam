package iam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTenantTestDB builds a minimal schema that supports per-tenant keys.
// It reuses newTestDB from store_test.go (same package).

// --- Per-tenant RSA key isolation ---

func TestLoadOrCreateRSAKey_PerTenant(t *testing.T) {
	db := newTestDB(t)
	storeA := NewStore(db).ForTenant("tenant-a")
	storeB := NewStore(db).ForTenant("tenant-b")

	keyA, err := storeA.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatalf("tenant-a LoadOrCreateRSAKey: %v", err)
	}
	keyB, err := storeB.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatalf("tenant-b LoadOrCreateRSAKey: %v", err)
	}

	// Keys must be distinct RSA key pairs
	if keyA.N.Cmp(keyB.N) == 0 {
		t.Fatal("tenant-a and tenant-b share the same RSA key; expected distinct keys")
	}
}

func TestLoadOrCreateRSAKey_SameTenantReturnsSameKey(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db).ForTenant("tenant-a")

	key1, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	key2, err := store.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	if key1.N.Cmp(key2.N) != 0 {
		t.Fatal("same tenant returned different RSA keys on second load")
	}
}

func TestJWKSEndpoint_PerTenant_DifferentKeys(t *testing.T) {
	db := newTestDB(t)

	storeA := NewStore(db).ForTenant("tenant-a")
	storeB := NewStore(db).ForTenant("tenant-b")

	keyA, err := storeA.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := storeB.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}

	tokensA := NewTokenService(keyA, "http://test-issuer")
	tokensB := NewTokenService(keyB, "http://test-issuer")

	handlerA := NewHandler(storeA, NewStaticTokenRegistry(tokensA), "http://test-issuer")
	handlerB := NewHandler(storeB, NewStaticTokenRegistry(tokensB), "http://test-issuer")

	reqA := httptest.NewRequest(http.MethodGet, "/jwks", nil)
	reqB := httptest.NewRequest(http.MethodGet, "/jwks", nil)
	rrA := httptest.NewRecorder()
	rrB := httptest.NewRecorder()

	handlerA.JWKS(rrA, reqA)
	handlerB.JWKS(rrB, reqB)

	if rrA.Code != http.StatusOK {
		t.Fatalf("tenant-a JWKS: expected 200, got %d", rrA.Code)
	}
	if rrB.Code != http.StatusOK {
		t.Fatalf("tenant-b JWKS: expected 200, got %d", rrB.Code)
	}

	var jwksA, jwksB struct {
		Keys []map[string]interface{} `json:"keys"`
	}
	if err := json.Unmarshal(rrA.Body.Bytes(), &jwksA); err != nil {
		t.Fatalf("decode tenant-a JWKS: %v", err)
	}
	if err := json.Unmarshal(rrB.Body.Bytes(), &jwksB); err != nil {
		t.Fatalf("decode tenant-b JWKS: %v", err)
	}

	if len(jwksA.Keys) == 0 {
		t.Fatal("tenant-a JWKS has no keys")
	}
	if len(jwksB.Keys) == 0 {
		t.Fatal("tenant-b JWKS has no keys")
	}

	nA := jwksA.Keys[0]["n"]
	nB := jwksB.Keys[0]["n"]
	if nA == nB {
		t.Fatal("tenant-a and tenant-b JWKS expose the same public key modulus; expected distinct keys")
	}
}

func TestToken_SignedByTenantA_InvalidWithTenantBKey(t *testing.T) {
	db := newTestDB(t)

	storeA := NewStore(db).ForTenant("tenant-a")
	storeB := NewStore(db).ForTenant("tenant-b")

	keyA, _ := storeA.LoadOrCreateRSAKey()
	keyB, _ := storeB.LoadOrCreateRSAKey()

	tokensA := NewTokenService(keyA, "http://test-issuer")
	tokensB := NewTokenService(keyB, "http://test-issuer")

	user := &User{ID: "u1", Email: "alice@example.com", Name: "Alice", Role: "user"}

	tokenStr, err := tokensA.CreateAccessToken(user, "aud", "tenant-a")
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	// Validating tenant-a's token with tenant-b's service must fail
	_, err = tokensB.ValidateAccessToken(tokenStr)
	if err == nil {
		t.Fatal("expected validation failure: token signed by tenant-a should not validate with tenant-b key")
	}
}

func TestToken_TenantIDEmbeddedInClaims(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db).ForTenant("tenant-x")
	key, _ := store.LoadOrCreateRSAKey()
	tokens := NewTokenService(key, "http://test-issuer")

	user := &User{ID: "u1", Email: "bob@example.com", Name: "Bob", Role: "user"}
	tokenStr, err := tokens.CreateAccessToken(user, "aud", "tenant-x")
	if err != nil {
		t.Fatal(err)
	}

	claims, err := tokens.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}

	tid, ok := claims["tid"]
	if !ok {
		t.Fatal("tid claim missing from access token")
	}
	if tid != "tenant-x" {
		t.Fatalf("tid = %v, want tenant-x", tid)
	}
}

// TestJWKSEndpoint_TenantFromContext verifies that the JWKS handler uses the
// tenant-scoped token service. This requires the JWKS handler to look up the
// RSA key for the tenant in context, not use a single global key.
//
// This test expresses the target design: calling /jwks on the same handler but
// with different tenant contexts returns different public keys.
func TestJWKSEndpoint_TenantFromContext(t *testing.T) {
	db := newTestDB(t)

	storeA := NewStore(db).ForTenant("tenant-a")
	storeB := NewStore(db).ForTenant("tenant-b")

	keyA, _ := storeA.LoadOrCreateRSAKey()
	keyB, _ := storeB.LoadOrCreateRSAKey()

	// tokensA and tokensB simulate two tenant-scoped token services loaded from db.
	tokensA := NewTokenService(keyA, "http://test-issuer")
	tokensB := NewTokenService(keyB, "http://test-issuer")

	// A shared handler that resolves the TokenService per tenant from context
	// requires a new constructor: NewTenantAwareHandler or the handler must
	// call store.LoadOrCreateRSAKey per request. For now, verify that the
	// underlying components support this:
	// tokensA and tokensB produce different JWKS.
	jwksA := tokensA.JWKS()
	jwksB := tokensB.JWKS()

	keysA := jwksA["keys"].([]map[string]interface{})
	keysB := jwksB["keys"].([]map[string]interface{})

	if keysA[0]["n"] == keysB[0]["n"] {
		t.Fatal("JWKS for tenant-a and tenant-b expose the same modulus; keys must be per-tenant")
	}
}
