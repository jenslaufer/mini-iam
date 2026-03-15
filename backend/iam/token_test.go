package iam

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func newTestTokenService(t *testing.T) *TokenService {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return NewTokenService(key, "http://test-issuer")
}

func TestCreateAccessToken(t *testing.T) {
	ts := newTestTokenService(t)
	user := &User{ID: "u1", Email: "test@example.com", Name: "Test", Role: "user"}

	token, err := ts.CreateAccessToken(user, "test-audience")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Error("token is empty")
	}
}

func TestValidateAccessToken(t *testing.T) {
	ts := newTestTokenService(t)
	user := &User{ID: "u1", Email: "test@example.com", Name: "Test", Role: "admin"}

	token, _ := ts.CreateAccessToken(user, "aud")
	claims, err := ts.ValidateAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != "u1" {
		t.Errorf("sub = %v", claims["sub"])
	}
	if claims["email"] != "test@example.com" {
		t.Errorf("email = %v", claims["email"])
	}
	if claims["role"] != "admin" {
		t.Errorf("role = %v", claims["role"])
	}
	if claims["iss"] != "http://test-issuer" {
		t.Errorf("iss = %v", claims["iss"])
	}
}

func TestValidateAccessTokenInvalid(t *testing.T) {
	ts := newTestTokenService(t)

	_, err := ts.ValidateAccessToken("invalid.token.string")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestValidateAccessTokenWrongKey(t *testing.T) {
	ts1 := newTestTokenService(t)
	ts2 := newTestTokenService(t)

	user := &User{ID: "u1", Email: "test@example.com", Name: "Test", Role: "user"}
	token, _ := ts1.CreateAccessToken(user, "aud")

	_, err := ts2.ValidateAccessToken(token)
	if err == nil {
		t.Error("expected error for token signed with different key")
	}
}

func TestCreateIDToken(t *testing.T) {
	ts := newTestTokenService(t)
	user := &User{ID: "u1", Email: "test@example.com", Name: "Test", Role: "user"}

	token, err := ts.CreateIDToken(user, "aud", "nonce123")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Error("token is empty")
	}

	claims, _ := ts.ValidateAccessToken(token)
	if claims["nonce"] != "nonce123" {
		t.Errorf("nonce = %v", claims["nonce"])
	}
}

func TestCreateIDTokenWithoutNonce(t *testing.T) {
	ts := newTestTokenService(t)
	user := &User{ID: "u1", Email: "test@example.com", Name: "Test", Role: "user"}

	token, _ := ts.CreateIDToken(user, "aud", "")
	claims, _ := ts.ValidateAccessToken(token)
	if _, ok := claims["nonce"]; ok {
		t.Error("nonce should not be present when empty")
	}
}

func TestJWKS(t *testing.T) {
	ts := newTestTokenService(t)

	jwks := ts.JWKS()
	keys, ok := jwks["keys"].([]map[string]interface{})
	if !ok || len(keys) != 1 {
		t.Fatal("expected 1 key in JWKS")
	}
	key := keys[0]
	if key["kty"] != "RSA" {
		t.Errorf("kty = %v", key["kty"])
	}
	if key["alg"] != "RS256" {
		t.Errorf("alg = %v", key["alg"])
	}
	if key["kid"] != "main" {
		t.Errorf("kid = %v", key["kid"])
	}
	if key["n"] == nil || key["n"] == "" {
		t.Error("n is empty")
	}
	if key["e"] == nil || key["e"] == "" {
		t.Error("e is empty")
	}
}

func TestJWKSBytes(t *testing.T) {
	ts := newTestTokenService(t)

	data, err := ts.JWKSBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("empty JWKS bytes")
	}
}

func TestVerifyPKCE(t *testing.T) {
	verifier := "test-code-verifier-string"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	if !VerifyPKCE(verifier, challenge, "S256") {
		t.Error("valid PKCE rejected")
	}
}

func TestVerifyPKCEWrongVerifier(t *testing.T) {
	h := sha256.Sum256([]byte("correct-verifier"))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	if VerifyPKCE("wrong-verifier", challenge, "S256") {
		t.Error("invalid PKCE accepted")
	}
}

func TestVerifyPKCEUnsupportedMethod(t *testing.T) {
	if VerifyPKCE("verifier", "challenge", "plain") {
		t.Error("unsupported method should return false")
	}
}
