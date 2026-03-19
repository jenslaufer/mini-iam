# Launch Kit

Multi-tenant IAM and email marketing. One Go binary, one Vue admin UI.

## What It Does

**Identity & Access Management** — OIDC-compliant auth server. OAuth2 with PKCE, RS256 JWTs, per-tenant RSA keys, user registration, role-based access. Drop-in replacement for Keycloak, Auth0, or Authentik.

**Email Marketing** — Contacts, segments, campaigns. Async sending, open tracking, unsubscribe handling, GDPR consent tracking.

**Multi-Tenant** — Isolated users, keys, contacts, and campaigns per tenant. Platform admin manages all tenants. Tenant admin sees only their own data.

## Architecture

```
Browser ──auth──> Launch Kit IAM (:8080)     <- OIDC endpoints
  |                     |
  | Bearer token        | JWKS (per-tenant RSA keys)
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

## Configuration

All environment variables are documented in [.env.example](.env.example). Key settings:

| Variable | Default | Description |
|---|---|---|
| `ISSUER_URL` | `http://localhost:8080` | Token issuer / base URL |
| `CORS_ORIGINS` | — | Comma-separated allowed origins |
| `ADMIN_EMAIL` | — | Seed admin email |
| `DEFAULT_TENANT` | — | Default tenant slug |
| `SMTP_HOST` | — | SMTP server (empty = log-only) |

## Multi-Tenant

Each tenant gets isolated users, RSA keys, contacts, segments, and campaigns.

**Tenant routing** (priority order):
1. Path prefix: `/t/{slug}/login`, `/t/{slug}/jwks`, etc.
2. `X-Tenant` HTTP header
3. `DEFAULT_TENANT` fallback

**Access control:**
- **Platform admin** (default tenant) — manages all tenants
- **Tenant admin** — sees only their own data
- **Per-tenant registration** — `registration_enabled` flag controls `/register`

## Using Launch Kit as OIDC Provider

Your app needs three env vars:

```env
OIDC_ISSUER_URL=http://localhost:8080/t/my-app
OIDC_JWKS_URI=http://launch-kit:8080/t/my-app/jwks
OIDC_AUDIENCE=
```

To switch to Keycloak, Auth0, or any OIDC provider — change these three vars. No code changes.

## API

50+ API endpoints covering OIDC/Auth, user management, marketing (contacts, segments, campaigns), and tenant administration.

See [API Reference](docs/api.md) for complete endpoint documentation with request/response examples.

## Documentation

| Guide | Description |
|---|---|
| [API Reference](docs/api.md) | All endpoints with request/response examples |
| [Deployment Guide](docs/deployment.md) | Docker, Nginx, SMTP, SQLite, security checklist |
| [Developer Guide](docs/development.md) | Project structure, local setup, adding endpoints/views |
| [Integration Guide](docs/integration.md) | OIDC flow, token validation, FastAPI/Vue examples |

## Email Template Variables

| Variable | Description |
|---|---|
| `{{.Name}}` | Contact name |
| `{{.Email}}` | Contact email |
| `{{.UnsubscribeURL}}` | Unsubscribe link |
| `{{.TrackingPixelURL}}` | Open tracking pixel |
| `{{.InviteURL}}` | Account activation link |

## Security

See [SECURITY.md](SECURITY.md) for implemented controls, open findings, and reporting guidelines.
