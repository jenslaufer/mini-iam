# mini-iam

Minimal Identity and Access Management service. OAuth2 + OpenID Connect in a single Go binary.

## Features

- OAuth2 authorization code flow with PKCE
- OpenID Connect discovery, JWKS, userinfo
- RS256 JWT tokens (RSA key auto-generated, stored in SQLite)
- Refresh token rotation
- SQLite database (pure Go, no CGo)
- CORS middleware

## Quick Start

```bash
go build -o mini-iam .
./mini-iam
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `ISSUER_URL` | `http://localhost:8080` | Token issuer / base URL |
| `CORS_ORIGINS` | `*` | Allowed CORS origins |

## Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/register` | User registration |
| POST | `/login` | Direct login (returns tokens) |
| GET | `/authorize` | OAuth2 authorization (login form) |
| POST | `/authorize` | Process authorization |
| POST | `/token` | OAuth2 token exchange |
| GET | `/userinfo` | OIDC userinfo |
| GET | `/.well-known/openid-configuration` | OIDC discovery |
| GET | `/jwks` | JSON Web Key Set |
| POST | `/revoke` | Token revocation |
| POST | `/clients` | Register OAuth2 client |
| GET | `/health` | Health check |

## Project Structure

```
main.go         - Server startup, routing
store.go        - SQLite operations
models.go       - Data models
token.go        - JWT creation/validation, JWKS
handlers.go     - HTTP handlers
middleware.go   - CORS middleware
```

## Client Libraries

Compatible with:
- **Python**: Authlib
- **Vue/JS**: oidc-client-ts
