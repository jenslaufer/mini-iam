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
	issuer     string
}

func NewTokenService(key *rsa.PrivateKey, issuer string) *TokenService {
	return &TokenService{privateKey: key, issuer: issuer}
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
	token.Header["kid"] = "main"
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
	token.Header["kid"] = "main"
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
	token.Header["kid"] = "main"
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

// JWKS returns the JSON Web Key Set containing the public key.
func (ts *TokenService) JWKS() map[string]interface{} {
	pub := &ts.privateKey.PublicKey
	return map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": "main",
				"n":   base64URLEncode(pub.N.Bytes()),
				"e":   base64URLEncode(big.NewInt(int64(pub.E)).Bytes()),
			},
		},
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
