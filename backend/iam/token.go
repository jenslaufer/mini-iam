package iam

import (
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenService struct {
	privateKey *rsa.PrivateKey
	kid        string
	keys       []keyEntry // all active keys for JWKS + validation
	issuer     string
}

type keyEntry struct {
	kid        string
	privateKey *rsa.PrivateKey
}

func (ts *TokenService) Issuer() string { return ts.issuer }

func NewTokenService(key *rsa.PrivateKey, issuer string) *TokenService {
	return &TokenService{
		privateKey: key,
		kid:        "main",
		keys:       []keyEntry{{kid: "main", privateKey: key}},
		issuer:     issuer,
	}
}

// NewTokenServiceMultiKey creates a TokenService with multiple keys.
// The last key in the slice is used for signing new tokens.
func NewTokenServiceMultiKey(records []KeyRecord, issuer string) *TokenService {
	entries := make([]keyEntry, len(records))
	for i, r := range records {
		entries[i] = keyEntry{kid: r.Kid, privateKey: r.PrivateKey}
	}
	newest := entries[len(entries)-1]
	return &TokenService{
		privateKey: newest.privateKey,
		kid:        newest.kid,
		keys:       entries,
		issuer:     issuer,
	}
}

func (ts *TokenService) CreateAccessToken(user *User, audience string, tenantID string) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":   user.ID,
		"iss":   ts.issuer,
		"aud":   audience,
		"exp":   now.Add(1 * time.Hour).Unix(),
		"iat":   now.Unix(),
		"email": user.Email,
		"name":  user.Name,
		"role":  user.Role,
		"tid":   tenantID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = ts.kid
	return token.SignedString(ts.privateKey)
}

func (ts *TokenService) CreateServiceToken(clientID, audience, tenantID string) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":  clientID,
		"iss":  ts.issuer,
		"aud":  audience,
		"exp":  now.Add(1 * time.Hour).Unix(),
		"iat":  now.Unix(),
		"tid":  tenantID,
		"type": "client_credentials",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = ts.kid
	return token.SignedString(ts.privateKey)
}

func (ts *TokenService) CreateIDToken(user *User, audience, nonce string, tenantID string) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":   user.ID,
		"iss":   ts.issuer,
		"aud":   audience,
		"exp":   now.Add(1 * time.Hour).Unix(),
		"iat":   now.Unix(),
		"email": user.Email,
		"name":  user.Name,
		"role":  user.Role,
		"tid":   tenantID,
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = ts.kid
	return token.SignedString(ts.privateKey)
}

func (ts *TokenService) ValidateAccessToken(tokenString string, expectedAudience string) (jwt.MapClaims, error) {
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(ts.issuer),
	}
	if expectedAudience != "" {
		opts = append(opts, jwt.WithAudience(expectedAudience))
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		for _, k := range ts.keys {
			if k.kid == kid {
				return &k.privateKey.PublicKey, nil
			}
		}
		// Fallback to primary key if kid not found (backward compat).
		return &ts.privateKey.PublicKey, nil
	}, opts...)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}
	return claims, nil
}

// JWKS returns the JSON Web Key Set containing all active public keys.
func (ts *TokenService) JWKS() map[string]interface{} {
	jwkKeys := make([]map[string]interface{}, len(ts.keys))
	for i, k := range ts.keys {
		pub := &k.privateKey.PublicKey
		jwkKeys[i] = map[string]interface{}{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": k.kid,
			"n":   base64URLEncode(pub.N.Bytes()),
			"e":   base64URLEncode(big.NewInt(int64(pub.E)).Bytes()),
		}
	}
	return map[string]interface{}{
		"keys": jwkKeys,
	}
}

func base64URLEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// VerifyPKCE verifies the code_verifier against the stored code_challenge using S256.
func VerifyPKCE(codeVerifier, codeChallenge, method string) bool {
	if method != "S256" {
		return false
	}
	h := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])
	return computed == codeChallenge
}

// JWKSBytes returns the JWKS as JSON bytes.
func (ts *TokenService) JWKSBytes() ([]byte, error) {
	return json.Marshal(ts.JWKS())
}

