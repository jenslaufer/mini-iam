# Launch Kit

Multi-tenant IAM and email marketing. One Go binary, one Vue admin UI.

## What it does

**Identity & Access Management** — OIDC-compliant auth server. OAuth2 with PKCE, RS256 JWTs, per-tenant RSA keys, user registration, role-based access. Works as a drop-in replacement for Keycloak, Auth0, or Authentik.

**Email Marketing** — Contacts, segments, campaigns. Async sending, open tracking, unsubscribe handling, GDPR consent tracking.

**Multi-Tenant** — Isolated users, keys, contacts, and campaigns per tenant. Platform admin manages all tenants. Tenant admin sees only their own data.

## Architecture

```
Browser ──auth──> Launch Kit IAM (:8080)     ← OIDC endpoints
  │                     │
  │ Bearer token        │ JWKS (per-tenant RSA keys)
  v                     v
Your App API  <──validates── Public Keys via OIDC Discovery
```

Your app never handles passwords. It validates JWTs via JWKS — swap Launch Kit for any OIDC provider by changing env vars.

## Quick Start

```bash
cp .env.example .env  # edit credentials
docker compose up
```

Admin UI: http://localhost:3000 — `admin@launch-kit.local` / `changeme`

### Demo App (FastAPI + Vue 3)

A working example app that uses Launch Kit as its IAM provider.

```bash
cd example
docker compose up
```

| Service | Port | Purpose |
|---|---|---|
| Launch Kit IAM | `:8080` | Auth endpoints (direct access) |
| Admin Frontend | `:3001` | Platform admin UI |
| Demo Frontend | `:4000` | Example app (register, login, notes) |
| Demo Backend | internal | FastAPI, validates tokens via JWKS |

Demo credentials: register on http://localhost:4000 or use `admin@demo.app` / `admin1234`.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Backend HTTP port |
| `ISSUER_URL` | `http://localhost:8080` | Token issuer / base URL |
| `CORS_ORIGINS` | — | Comma-separated allowed origins (empty = reject) |
| `ADMIN_EMAIL` | — | Seed admin email |
| `ADMIN_PASSWORD` | — | Seed admin password |
| `DEFAULT_TENANT` | — | Default tenant slug (auto-created) |
| `DEFAULT_TENANT_REGISTRATION` | — | Set to enable public registration on default tenant |
| `TENANT_CONFIG` | — | Path to JSON file for auto-import on startup |
| `SMTP_HOST` | — | SMTP server (empty = log-only) |
| `SMTP_PORT` | `587` | SMTP port |
| `SMTP_USER` | — | SMTP username |
| `SMTP_PASSWORD` | — | SMTP password |
| `SMTP_FROM` | — | Default sender email |
| `SMTP_FROM_NAME` | `launch-kit` | Default sender name |
| `SMTP_RATE_MS` | `100` | Delay between emails (ms) |

## Multi-Tenant

Each tenant gets isolated users, RSA keys, contacts, segments, and campaigns.

**Tenant routing** (in priority order):
1. Path prefix: `/t/{slug}/login`, `/t/{slug}/jwks`, etc.
2. `X-Tenant` HTTP header
3. `DEFAULT_TENANT` fallback

**Access control:**
- **Platform admin** (default tenant) — sees all tenants, manages any tenant's data
- **Tenant admin** — sees only their own tenant's data
- **Per-tenant registration** — `registration_enabled` flag controls public `/register`

```bash
# Import a tenant with open registration
curl -X POST /admin/tenants/import -H "Authorization: Bearer $TOKEN" \
  -d '{"slug":"my-app","name":"My App","registration_enabled":true}'

# Register a user in that tenant
curl -X POST /t/my-app/register \
  -d '{"email":"user@my-app.com","password":"secret1234","name":"User"}'

# OIDC Discovery for the tenant
curl /t/my-app/.well-known/openid-configuration
```

JWT tokens contain `tid` (tenant ID) and `iss` (tenant-scoped issuer). Tokens are signed with per-tenant RSA keys — a token from tenant A fails signature validation on tenant B.

### Tenant Import/Export

```json
{
  "slug": "my-app",
  "name": "My App",
  "registration_enabled": true,
  "admin": { "email": "admin@my-app.com", "password": "change-me" },
  "clients": [{ "name": "My SPA", "redirect_uris": ["https://my-app.com/callback"] }],
  "segments": [{ "name": "newsletter", "description": "Subscribers" }],
  "contacts": [{ "email": "alice@example.com", "name": "Alice", "segments": ["newsletter"] }]
}
```

## Using Launch Kit as OIDC Provider

Your app needs three env vars:

```env
# Public issuer (must match `iss` claim in tokens)
OIDC_ISSUER_URL=http://localhost:8080/t/my-app

# JWKS endpoint (internal URL in Docker)
OIDC_JWKS_URI=http://launch-kit:8080/t/my-app/jwks

# Optional audience validation
OIDC_AUDIENCE=
```

To switch to Keycloak, Auth0, or any OIDC provider — change these three vars. No code changes.

| Provider | OIDC_ISSUER_URL | OIDC_JWKS_URI |
|---|---|---|
| Launch Kit | `http://localhost:8080/t/my-app` | `http://launch-kit:8080/t/my-app/jwks` |
| Keycloak | `https://auth.example.com/realms/myrealm` | via Discovery |
| Auth0 | `https://myapp.auth0.com` | via Discovery |
| Authentik | `https://auth.example.com/application/o/myapp/` | via Discovery |

## Endpoints

### OIDC / Auth

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/register` | — | User registration (gated by `registration_enabled`) |
| POST | `/login` | — | Login (returns access, id, refresh tokens) |
| GET/POST | `/authorize` | — | OAuth2 authorization (login form / code grant) |
| POST | `/token` | — | Token exchange (auth code + PKCE, refresh) |
| GET | `/userinfo` | Bearer | OIDC userinfo |
| GET | `/.well-known/openid-configuration` | — | OIDC Discovery |
| GET | `/jwks` | — | JSON Web Key Set |
| POST | `/revoke` | — | Token revocation (RFC 7009) |
| POST | `/clients` | Admin | Register OAuth2 client |
| GET/POST | `/activate/{token}` | — | Account activation (invite flow) |

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

### Admin — Tenants (Platform Admin)

| Method | Path | Description |
|---|---|---|
| GET | `/admin/tenants` | List tenants |
| POST | `/admin/tenants/import` | Import tenant from JSON |
| GET | `/admin/tenants/{id}/export` | Export tenant config |
| DELETE | `/admin/tenants/{id}` | Delete tenant (cascades) |

### Public

| Method | Path | Description |
|---|---|---|
| GET | `/track/{id}` | Open tracking pixel |
| GET/POST | `/unsubscribe/{token}` | Unsubscribe |
| GET | `/health` | Health check |

## Project Structure

```
backend/
  main.go                 Entry point, routing, migrations
  middleware.go            CORS (origin allowlist validation)
  iam/
    handlers.go            OAuth2/OIDC + admin endpoints
    token.go               JWT creation/validation (RS256), JWKS, PKCE
    store.go               Users, clients, auth codes, refresh tokens, RSA keys
    registry.go            Per-tenant TokenService cache
    models.go              Data models
  marketing/
    handlers.go            Contacts, segments, campaigns admin + public
    store.go               Marketing data layer
    sender.go              Async campaign sender, per-tenant SMTP
  tenant/
    tenant.go              Tenant model, store, middleware, registration policy
    export_import.go       Tenant JSON import/export
  tenantctx/
    tenantctx.go           Request context helpers (tenant ID + slug)

frontend/
  src/
    views/                 Login, Dashboard, Users, Clients, Contacts,
                           Segments, Campaigns, Tenants
    components/            Layout, sidebar, modals, inputs, toasts
    api/                   Axios client with tenant header interceptor
    stores/                Pinia: auth (JWT, login), tenant (selector, platform admin)
  e2e/                     58 Playwright E2E tests
  nginx.conf               Reverse proxy (/auth → backend)

example/
  demo-backend/            FastAPI app (validates JWTs via OIDC Discovery)
  demo-frontend/           Vue 3 app (register, login, notes CRUD)
  docker-compose.yml       Full stack: IAM + admin + demo app
  tenant-demo.json         Demo tenant config (registration enabled)
```

## Development

```bash
# Backend
cd backend && go build -o launch-kit .
ADMIN_EMAIL=admin@local ADMIN_PASSWORD=changeme DEFAULT_TENANT=default ./launch-kit

# Frontend
cd frontend && npm install && npm run dev

# Tests
cd backend && go test ./...            # Go unit + integration tests
cd frontend && npm run test:e2e        # 58 Playwright E2E tests (requires docker compose up)
```

## Email Template Variables

| Variable | Description |
|---|---|
| `{{.Name}}` | Contact name |
| `{{.Email}}` | Contact email |
| `{{.UnsubscribeURL}}` | Unsubscribe link |
| `{{.TrackingPixelURL}}` | Open tracking pixel |
| `{{.InviteURL}}` | Account activation link |

## Security

- Per-tenant RS256 key pairs (isolated signing)
- PKCE required for public OAuth2 clients
- JWT issuer validation (`iss` claim checked)
- `Cache-Control: no-store` on all token responses
- Password minimum 8 characters, email max 254 characters
- CORS origin allowlist (no wildcard default)
- Bcrypt password hashing
- Parameterized SQL queries (no injection)
- Refresh token rotation with revocation
- Platform admin / tenant admin role separation
- Registration disabled by default per tenant
