# mini-iam

Minimal Identity and Access Management service. OAuth2 + OpenID Connect in a single Go binary with Vue 3 admin UI.

## Features

- OAuth2 authorization code flow with PKCE
- OpenID Connect discovery, JWKS, userinfo
- RS256 JWT tokens (RSA key auto-generated, stored in SQLite)
- Refresh token rotation and revocation
- Admin UI for user and client management
- Role-based access control (user/admin)
- SQLite database (pure Go, no CGo)

## Quick Start

```bash
docker compose up
```

Open http://localhost:3000 and log in with the admin credentials.

### Development

Backend:
```bash
cd backend
go build -o mini-iam .
ADMIN_EMAIL=admin@mini-iam.local ADMIN_PASSWORD=changeme ./mini-iam
```

Frontend:
```bash
cd frontend
npm install
npm run dev
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Backend HTTP port |
| `ISSUER_URL` | `http://localhost:8080` | Token issuer / base URL |
| `CORS_ORIGINS` | `*` | Allowed CORS origins |
| `ADMIN_EMAIL` | — | Seed admin account email |
| `ADMIN_PASSWORD` | — | Seed admin account password |

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
| GET | `/admin/users` | List all users (admin) |
| GET | `/admin/users/{id}` | Get user by ID (admin) |
| PUT | `/admin/users/{id}` | Update user (admin) |
| DELETE | `/admin/users/{id}` | Delete user (admin) |
| GET | `/admin/clients` | List all clients (admin) |
| DELETE | `/admin/clients/{id}` | Delete client (admin) |

## Project Structure

```
backend/
  main.go           - Server startup, routing
  store.go          - SQLite operations
  models.go         - Data models
  token.go          - JWT creation/validation, JWKS
  handlers.go       - HTTP handlers
  middleware.go     - CORS middleware
  main_test.go      - 102 integration tests
frontend/
  src/
    views/          - Login, Dashboard, Users, Clients
    components/     - Reusable UI components
    api/            - Backend API client
    stores/         - Pinia stores (auth, toast)
  nginx.conf        - Reverse proxy config
docker-compose.yml
```

## Client Libraries

Compatible with:
- **Python**: Authlib
- **Vue/JS**: oidc-client-ts
