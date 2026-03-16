# LaunchKit

Auth and email marketing for startups. One Go binary, one Vue admin UI.

## What it does

**Identity & Access Management** — OAuth2 + OpenID Connect with PKCE, JWT tokens, user registration, role-based access control.

**Email Marketing** — Contacts, segments, campaigns with async sending, open tracking, unsubscribe handling, GDPR consent tracking.

**Landing Page Flow** — Collect emails (contacts without passwords), send invite campaigns, users activate accounts when your app launches.

## Quick Start

```bash
docker compose up
```

Open http://localhost:3000. Login: `admin@launch-kit.local` / `changeme`.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Backend HTTP port |
| `ISSUER_URL` | `http://localhost:8080` | Token issuer / base URL |
| `CORS_ORIGINS` | `*` | Allowed CORS origins |
| `ADMIN_EMAIL` | — | Seed admin account email |
| `ADMIN_PASSWORD` | — | Seed admin account password |
| `SMTP_HOST` | — | SMTP server (empty = log-only mode) |
| `SMTP_PORT` | `587` | SMTP port |
| `SMTP_USER` | — | SMTP username |
| `SMTP_PASSWORD` | — | SMTP password |
| `SMTP_FROM` | — | Default sender email |
| `SMTP_FROM_NAME` | `LaunchKit` | Default sender name |
| `SMTP_RATE_MS` | `100` | Delay between emails (ms) |
| `DEFAULT_TENANT` | — | Default tenant slug (auto-created on startup) |

## Multi-Tenant

Serve multiple websites from a single deployment. Each tenant gets isolated users, contacts, segments, and campaigns.

**Tenant resolution** (in order):
1. `X-Tenant` HTTP header (slug)
2. Subdomain of `Host` header (`acme.example.com` → `acme`)
3. `DEFAULT_TENANT` env var fallback

**Setup:**
```bash
# Create a tenant
curl -X POST /admin/tenants -H "Authorization: Bearer $TOKEN" \
  -d '{"slug":"acme","name":"Acme Corp"}'

# Requests scoped to tenant via header
curl -H "X-Tenant: acme" /admin/contacts

# Or via subdomain
curl https://acme.example.com/admin/contacts
```

JWT tokens include a `tid` (tenant ID) claim. Admin access is validated against the request's tenant — a token from tenant A cannot access tenant B data.

### Tenant Admin API

| Method | Path | Description |
|---|---|---|
| GET/POST | `/admin/tenants` | List / create tenants |
| GET/DELETE | `/admin/tenants/{id}` | Manage tenant |

## Endpoints

### Auth (IAM)

| Method | Path | Description |
|---|---|---|
| POST | `/register` | User registration |
| POST | `/login` | Login (returns JWT tokens) |
| GET | `/authorize` | OAuth2 authorization |
| POST | `/token` | OAuth2 token exchange |
| GET | `/userinfo` | OIDC userinfo |
| GET | `/.well-known/openid-configuration` | OIDC discovery |
| GET | `/jwks` | JSON Web Key Set |
| POST | `/revoke` | Token revocation |
| POST | `/clients` | Register OAuth2 client |
| GET | `/activate/{token}` | Account activation page |
| GET | `/health` | Health check |

### Admin — Users & Clients

| Method | Path | Description |
|---|---|---|
| GET | `/admin/users` | List users |
| GET/PUT/DELETE | `/admin/users/{id}` | Manage user |
| GET | `/admin/clients` | List OAuth2 clients |
| DELETE | `/admin/clients/{id}` | Delete client |

### Admin — Marketing

| Method | Path | Description |
|---|---|---|
| GET/POST | `/admin/contacts` | List / create contacts |
| POST | `/admin/contacts/import` | Bulk import contacts |
| GET/DELETE | `/admin/contacts/{id}` | Manage contact |
| GET/POST | `/admin/segments` | List / create segments |
| GET/PUT/DELETE | `/admin/segments/{id}` | Manage segment |
| POST/DELETE | `/admin/segments/{id}/contacts` | Assign contacts |
| GET/POST | `/admin/campaigns` | List / create campaigns |
| GET/PUT/DELETE | `/admin/campaigns/{id}` | Manage campaign |
| POST | `/admin/campaigns/{id}/send` | Send campaign |
| GET | `/admin/campaigns/{id}/stats` | Campaign statistics |

### Public

| Method | Path | Description |
|---|---|---|
| GET | `/track/{id}` | Open tracking pixel |
| GET/POST | `/unsubscribe/{token}` | Unsubscribe |

## Project Structure

```
backend/
  main.go              - Entry point, routing, migrations
  middleware.go         - CORS
  tenant/
    tenant.go           - Tenant model, store, context, middleware
  iam/
    store.go            - Users, clients, auth codes, tokens
    handlers.go         - OAuth2/OIDC + admin endpoints
    token.go            - JWT creation/validation, JWKS
    models.go           - IAM data models
  marketing/
    store.go            - Contacts, segments, campaigns
    handlers.go         - Marketing admin + public endpoints
    sender.go           - Mailer interface, background worker
    models.go           - Marketing data models
  main_test.go          - 172 integration tests
frontend/
  src/
    views/              - Login, Dashboard, Users, Clients,
                          Contacts, Segments, Campaigns
    components/         - Reusable UI components
    api/                - Backend API client
    stores/             - Pinia stores (auth, toast)
  nginx.conf            - Reverse proxy (/auth → backend)
docker-compose.yml
```

## Development

Backend:
```bash
cd backend
go build -o launchkit .
ADMIN_EMAIL=admin@local ADMIN_PASSWORD=changeme ./launchkit
```

Frontend:
```bash
cd frontend
npm install
npm run dev
```

Tests:
```bash
# Backend (172 tests)
cd backend && go test ./...

# Frontend (97 component tests)
cd frontend && npm run test

# E2E (requires docker compose up)
cd frontend && npm run test:e2e
```

## Client Libraries

Compatible with standard OAuth2/OIDC libraries:
- **Python**: Authlib
- **Vue/JS**: oidc-client-ts

## Email Template Variables

| Variable | Description |
|---|---|
| `{{.Name}}` | Contact name |
| `{{.Email}}` | Contact email |
| `{{.UnsubscribeURL}}` | Unsubscribe link |
| `{{.TrackingPixelURL}}` | Open tracking pixel |
| `{{.InviteURL}}` | Account activation link |
