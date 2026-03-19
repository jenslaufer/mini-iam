# Developer Guide

## Project Structure

```
backend/
  main.go                 Entry point, routing, migrations
  middleware.go            CORS middleware (origin allowlist)
  iam/
    handlers.go            OAuth2/OIDC + admin endpoints
    token.go               JWT creation/validation (RS256), JWKS, PKCE
    store.go               Users, clients, auth codes, refresh tokens, RSA keys
    registry.go            Per-tenant TokenService cache
    models.go              Data models (User, Client, TokenResponse, etc.)
  marketing/
    handlers.go            Contacts, segments, campaigns admin + public
    store.go               Marketing data layer
    sender.go              Async campaign sender, per-tenant SMTP
    models.go              Contact, Segment, Campaign, CampaignStats
  tenant/
    tenant.go              Tenant model, store, middleware, registration policy
    export_import.go       Tenant JSON import/export + batch import
  tenantctx/
    tenantctx.go           Request context helpers (tenant ID + slug)

frontend/                  Vue 3 admin UI (see frontend/README.md)
example/                   Demo app (FastAPI + Vue 3)
```

## Running Locally

### Prerequisites

- Go 1.23+
- Node.js 18+ and npm
- Docker and Docker Compose (for full stack)

### Backend

```bash
cd backend
go build -o launch-kit .
ADMIN_EMAIL=admin@local ADMIN_PASSWORD=changeme DEFAULT_TENANT=default ./launch-kit
```

The backend starts on port 8080. SQLite database is created automatically.

### Frontend

```bash
cd frontend
npm install
npm run dev
```

Dev server starts on port 3000. Vite proxies `/auth/*` requests to `http://localhost:8080`.

### Full Stack (Docker)

```bash
cp .env.example .env
docker compose up
```

## Running Tests

```bash
# Backend — Go unit + integration tests
cd backend && go test ./...

# Frontend — Vitest unit tests
cd frontend && npm run test

# Frontend — Playwright E2E tests (requires docker compose up)
cd frontend && npx playwright test
```

E2E tests need a running Launch Kit instance. The test environment is configured in `.env.test`.

## Adding a New API Endpoint

1. **Write the handler** in the appropriate package (`iam/handlers.go` or `marketing/handlers.go`):

```go
func (h *Handler) MyEndpoint(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
        return
    }
    // For admin endpoints:
    if _, ok := h.requireAdmin(w, r); !ok {
        return
    }
    // Tenant-scoped store:
    store := h.tenantStore(r)
    // ...
    WriteJSON(w, http.StatusOK, result)
}
```

2. **Add store methods** if the endpoint needs new data access (`iam/store.go` or `marketing/store.go`).

3. **Register the route** in `main.go`:

```go
mux.HandleFunc("/my-endpoint", iamHandler.MyEndpoint)
```

## Adding a New Frontend View

1. **Create the Vue component** in `frontend/src/views/MyView.vue`.

2. **Add the route** in `frontend/src/router/index.js`:

```js
{ path: '/my-view', component: () => import('../views/MyView.vue') }
```

3. **Add a sidebar entry** in `frontend/src/components/AppSidebar.vue`.

4. **Create an API module** in `frontend/src/api/my-resource.js` using the shared client:

```js
import apiClient from './client.js'

export const getMyResources = () => apiClient.get('/my-resources').then(r => r.data)
```

The `apiClient` automatically attaches the `Bearer` token and `X-Tenant` header.

## Code Conventions

### Error Responses

All errors use the standard format:

```json
{"error": "error_code", "error_description": "Human-readable message"}
```

Common error codes: `invalid_request`, `invalid_grant`, `invalid_token`, `insufficient_scope`, `server_error`, `not_found`.

### Auth Middleware

- `requireAdmin(w, r)` — validates Bearer token + admin role. Platform admins can access any tenant.
- `requireAuth(w, r)` — validates Bearer token, any role.
- `CheckAdminCrossTenant(...)` — validates platform admin (default tenant admin only). Used for tenant management endpoints.

### Tenant Context

Every handler gets the tenant from the request context:

```go
tenantID := tenantctx.FromContext(r.Context())
slug := tenantctx.SlugFromContext(r.Context())
store := h.tenantStore(r)  // returns a tenant-scoped store
```

### Frontend API Client

`frontend/src/api/client.js` creates an Axios instance that:
- Adds `Authorization: Bearer <token>` from the auth store
- Adds `X-Tenant: <slug>` from the tenant store
- Redirects to `/login` on 401 responses

## Database Schema

SQLite with WAL mode and foreign keys. Tables:

| Table | Key Columns | Description |
|---|---|---|
| `tenants` | id, slug, name, registration_enabled, smtp_* | Tenant registry |
| `users` | id, tenant_id, email, password_hash, name, role | User accounts |
| `clients` | id, tenant_id, name, redirect_uris | OAuth2 clients |
| `auth_codes` | code, tenant_id, client_id, user_id, code_challenge | Authorization codes (PKCE) |
| `refresh_tokens` | token, tenant_id, client_id, user_id, revoked | Refresh tokens |
| `keys` | id, tenant_id, private_key_pem | Per-tenant RSA keys |
| `contacts` | id, tenant_id, email, unsubscribe_token, invite_token | Marketing contacts |
| `segments` | id, tenant_id, name, description | Contact segments |
| `contact_segments` | contact_id, segment_id | Many-to-many junction |
| `campaigns` | id, tenant_id, subject, html_body, status | Email campaigns |
| `campaign_segments` | campaign_id, segment_id | Campaign targeting |
| `campaign_recipients` | id, campaign_id, contact_id, status, opened_at | Delivery tracking |

All tables with `tenant_id` are scoped per tenant. Junction tables cascade on delete.

Migrations run automatically on startup (`backend/main.go:261-406`). New columns are added idempotently with `ALTER TABLE ... ADD COLUMN`.
