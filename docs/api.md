# API Reference

Launch Kit exposes 50+ endpoints. All endpoints accept and return JSON unless noted otherwise.

## Multi-Tenant Routing

Every request is scoped to a tenant. Resolution order:

1. **Path prefix** — `/t/{slug}/login`, `/t/{slug}/jwks`, etc. (prefix is stripped)
2. **`X-Tenant` header** — slug value
3. **`DEFAULT_TENANT` fallback** — from environment

```bash
# Path prefix
curl http://localhost:8080/t/my-app/login -d '{"email":"...","password":"..."}'

# Header
curl http://localhost:8080/login -H "X-Tenant: my-app" -d '...'
```

## Token Format

All tokens are RS256 JWTs signed with per-tenant RSA keys.

**Access token claims:**

```json
{
  "sub": "user-uuid",
  "iss": "http://localhost:8080/t/my-app",
  "aud": "client-id",
  "exp": 1700000000,
  "iat": 1699996400,
  "email": "user@example.com",
  "name": "User Name",
  "role": "user",
  "tid": "tenant-uuid"
}
```

Tokens expire after 1 hour. A token from tenant A fails signature validation on tenant B.

## Error Format

All errors follow OAuth2 conventions:

```json
{
  "error": "invalid_request",
  "error_description": "human-readable message"
}
```

---

## OIDC / Auth

### POST /register

Create a new user account. Requires `registration_enabled` on the tenant.

**Request:**
```json
{
  "email": "user@example.com",
  "password": "secret1234",
  "name": "Jane Doe"
}
```

**Response (201):**
```json
{
  "id": "uuid",
  "tenant_id": "tenant-uuid",
  "email": "user@example.com",
  "name": "Jane Doe",
  "role": "user",
  "created_at": "2025-01-15T10:00:00Z"
}
```

**Errors:**
| Code | Error | Description |
|------|-------|-------------|
| 400 | `invalid_request` | Invalid email, password < 8 chars, or name missing |
| 403 | `registration_disabled` | Public registration disabled for this tenant |
| 409 | `invalid_request` | Email already registered |

### POST /login

Authenticate and receive tokens.

**Request:**
```json
{
  "email": "user@example.com",
  "password": "secret1234"
}
```

**Response (200):**
```json
{
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "rt-...",
  "id_token": "eyJ..."
}
```

**Errors:**
| Code | Error | Description |
|------|-------|-------------|
| 401 | `invalid_grant` | Invalid credentials |

### GET /authorize

Renders an HTML login form for the OAuth2 authorization code flow. Pass parameters as query strings.

**Query parameters:** `client_id`, `redirect_uri`, `state`, `scope`, `nonce`, `code_challenge`, `code_challenge_method`, `response_type`

### POST /authorize

Submits the login form. On success, redirects to `redirect_uri` with `?code=...&state=...`.

### POST /token

Exchange an authorization code or refresh token for tokens.

**Authorization code grant (form-encoded):**

| Field | Required | Description |
|-------|----------|-------------|
| `grant_type` | yes | `authorization_code` |
| `code` | yes | Authorization code |
| `redirect_uri` | yes | Must match original request |
| `code_verifier` | PKCE | PKCE verifier (S256) |
| `client_id` | yes | OAuth2 client ID |
| `client_secret` | confidential | Client secret (if no PKCE) |

**Refresh token grant (form-encoded):**

| Field | Required | Description |
|-------|----------|-------------|
| `grant_type` | yes | `refresh_token` |
| `refresh_token` | yes | Refresh token |

**Response (200):**
```json
{
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "rt-new-...",
  "id_token": "eyJ..."
}
```

**Errors:**
| Code | Error | Description |
|------|-------|-------------|
| 400 | `invalid_grant` | Invalid code, expired, PKCE mismatch, or redirect_uri mismatch |
| 400 | `unsupported_grant_type` | Must be `authorization_code` or `refresh_token` |
| 401 | `invalid_client` | Invalid client credentials |

### GET /userinfo

Returns the authenticated user's profile. Requires `Bearer` token.

**Response (200):**
```json
{
  "sub": "user-uuid",
  "email": "user@example.com",
  "name": "Jane Doe",
  "role": "user"
}
```

### GET /.well-known/openid-configuration

OIDC Discovery document.

**Response (200):**
```json
{
  "issuer": "http://localhost:8080/t/my-app",
  "authorization_endpoint": "http://localhost:8080/t/my-app/authorize",
  "token_endpoint": "http://localhost:8080/t/my-app/token",
  "userinfo_endpoint": "http://localhost:8080/t/my-app/userinfo",
  "jwks_uri": "http://localhost:8080/t/my-app/jwks",
  "revocation_endpoint": "http://localhost:8080/t/my-app/revoke",
  "registration_endpoint": "http://localhost:8080/t/my-app/clients",
  "scopes_supported": ["openid", "profile", "email"],
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code", "refresh_token"],
  "subject_types_supported": ["public"],
  "id_token_signing_alg_values_supported": ["RS256"],
  "token_endpoint_auth_methods_supported": ["client_secret_post", "none"],
  "code_challenge_methods_supported": ["S256"]
}
```

### GET /jwks

JSON Web Key Set for token signature verification.

### POST /revoke

Revoke a refresh token (RFC 7009). Always returns 200.

**Form parameters:** `token` (the refresh token)

### POST /password

Change the authenticated user's password. Requires `Bearer` token.

**Request:**
```json
{
  "current_password": "old-password",
  "new_password": "new-password-8chars"
}
```

**Response (200):**
```json
{"status": "password_changed"}
```

**Errors:**
| Code | Error | Description |
|------|-------|-------------|
| 400 | `invalid_request` | New password < 8 or > 72 characters |
| 401 | `invalid_grant` | Current password incorrect |

### POST /clients

Register an OAuth2 client. Requires admin `Bearer` token.

**Request:**
```json
{
  "name": "My SPA",
  "redirect_uris": ["http://localhost:4000/callback"]
}
```

**Response (201):**
```json
{
  "client_id": "uuid",
  "client_secret": "random-secret",
  "name": "My SPA",
  "redirect_uris": ["http://localhost:4000/callback"]
}
```

### GET/POST /activate/{token}

Account activation for the invite flow.

- **GET** — Renders an HTML password form
- **POST** (JSON) — `{"password": "at-least-8"}` → `{"status": "activated", "user_id": "uuid"}`
- **POST** (form) — Sets password via HTML form

---

## Admin — Users & Clients

All admin endpoints require a `Bearer` token with `role: "admin"`. Platform admins (from the default tenant) can access any tenant's admin endpoints.

### GET /admin/users

List all users in the current tenant.

**Response (200):**
```json
[
  {
    "id": "uuid",
    "tenant_id": "tenant-uuid",
    "email": "user@example.com",
    "name": "Jane Doe",
    "role": "user",
    "created_at": "2025-01-15T10:00:00Z"
  }
]
```

### GET /admin/users/{id}

Get a single user by ID.

### PUT /admin/users/{id}

Update a user's name and/or role.

**Request:**
```json
{
  "name": "New Name",
  "role": "admin"
}
```

Role must be `"user"` or `"admin"`.

### DELETE /admin/users/{id}

Delete a user. Cannot delete yourself.

**Response (200):**
```json
{"status": "deleted"}
```

### GET /admin/clients

List all OAuth2 clients in the current tenant.

**Response (200):**
```json
[
  {
    "client_id": "uuid",
    "tenant_id": "tenant-uuid",
    "name": "My SPA",
    "redirect_uris": ["http://localhost:4000/callback"],
    "created_at": "2025-01-15T10:00:00Z"
  }
]
```

### DELETE /admin/clients/{id}

Delete an OAuth2 client.

---

## Admin — Marketing

All marketing admin endpoints require admin `Bearer` token.

### GET /admin/contacts

List all contacts with their segments.

**Response (200):**
```json
[
  {
    "id": "uuid",
    "tenant_id": "tenant-uuid",
    "email": "alice@example.com",
    "name": "Alice",
    "user_id": null,
    "unsubscribed": false,
    "consent_source": "api",
    "consent_at": "2025-01-15T10:00:00Z",
    "created_at": "2025-01-15T10:00:00Z",
    "segments": [{"id": "seg-uuid", "name": "newsletter"}]
  }
]
```

### POST /admin/contacts

Create a contact. Returns an `invite_token` for the activation flow.

**Request:**
```json
{
  "email": "alice@example.com",
  "name": "Alice"
}
```

**Response (201):**
```json
{
  "id": "uuid",
  "email": "alice@example.com",
  "name": "Alice",
  "unsubscribed": false,
  "invite_token": "random-token",
  "consent_source": "api",
  "consent_at": "2025-01-15T10:00:00Z",
  "created_at": "2025-01-15T10:00:00Z"
}
```

### GET /admin/contacts/{id}

Get a contact with segments.

### DELETE /admin/contacts/{id}

Delete a contact.

### POST /admin/contacts/import

Bulk import contacts.

**Request:**
```json
[
  {"email": "alice@example.com", "name": "Alice", "segments": ["newsletter"]},
  {"email": "bob@example.com", "name": "Bob"}
]
```

**Response (200):**
```json
{"imported": 2, "skipped": 0}
```

### GET /admin/segments

List all segments.

**Response (200):**
```json
[
  {
    "id": "uuid",
    "tenant_id": "tenant-uuid",
    "name": "newsletter",
    "description": "Newsletter subscribers",
    "contact_count": 42,
    "created_at": "2025-01-15T10:00:00Z"
  }
]
```

### POST /admin/segments

Create a segment.

**Request:**
```json
{
  "name": "newsletter",
  "description": "Newsletter subscribers"
}
```

**Response (201):** Segment object.

### GET /admin/segments/{id}

Get a segment with its contacts.

**Response (200):**
```json
{
  "segment": {"id": "uuid", "name": "newsletter", "description": "..."},
  "contacts": [{"id": "uuid", "email": "alice@example.com", "name": "Alice"}]
}
```

### PUT /admin/segments/{id}

Update a segment.

**Request:**
```json
{
  "name": "premium",
  "description": "Premium subscribers"
}
```

### DELETE /admin/segments/{id}

Delete a segment.

### POST /admin/segments/{id}/contacts

Add a contact to a segment.

**Request:**
```json
{"contact_id": "contact-uuid"}
```

### DELETE /admin/segments/{id}/contacts/{contact_id}

Remove a contact from a segment.

### GET /admin/campaigns

List all campaigns with stats.

**Response (200):**
```json
[
  {
    "id": "uuid",
    "subject": "Welcome!",
    "from_name": "Acme",
    "from_email": "hello@acme.com",
    "status": "draft",
    "sent_at": null,
    "created_at": "2025-01-15T10:00:00Z",
    "total": 100,
    "queued": 0,
    "sent": 0,
    "failed": 0,
    "opened": 0
  }
]
```

### POST /admin/campaigns

Create a campaign.

**Request:**
```json
{
  "subject": "Welcome!",
  "html_body": "<h1>Hello {{.Name}}</h1><p>...</p>",
  "from_name": "Acme",
  "from_email": "hello@acme.com",
  "segment_ids": ["seg-uuid"]
}
```

**Response (201):** Campaign object.

### GET /admin/campaigns/{id}

Get a campaign with stats.

**Response (200):**
```json
{
  "campaign": {
    "id": "uuid",
    "subject": "Welcome!",
    "html_body": "<h1>Hello {{.Name}}</h1>",
    "from_name": "Acme",
    "from_email": "hello@acme.com",
    "status": "draft",
    "segment_ids": ["seg-uuid"],
    "created_at": "2025-01-15T10:00:00Z"
  },
  "stats": {"total": 100, "queued": 0, "sent": 0, "failed": 0, "opened": 0}
}
```

### PUT /admin/campaigns/{id}

Update a draft campaign. Same request body as POST.

### DELETE /admin/campaigns/{id}

Delete a draft campaign. Sent campaigns cannot be deleted.

### POST /admin/campaigns/{id}/send

Send a draft campaign. Enqueues async delivery.

**Response (202):**
```json
{"status": "sending"}
```

### GET /admin/campaigns/{id}/stats

Campaign delivery statistics.

**Response (200):**
```json
{"total": 100, "queued": 5, "sent": 90, "failed": 2, "opened": 45}
```

---

## Admin — Tenants (Platform Admin)

These endpoints require a platform admin token (admin in the default tenant).

### GET /admin/tenants

List all tenants. SMTP passwords are never exposed.

**Response (200):**
```json
[
  {
    "id": "uuid",
    "slug": "my-app",
    "name": "My App",
    "registration_enabled": true,
    "smtp": {"smtp_host": "smtp.example.com", "smtp_port": "587"},
    "created_at": "2025-01-15T10:00:00Z"
  }
]
```

### POST /admin/tenants

Create a tenant.

**Request:**
```json
{"slug": "my-app", "name": "My App"}
```

Slug must match `^[a-z0-9][a-z0-9-]{0,62}$`.

**Response (201):** Tenant object.

### POST /admin/tenants/import

Import a full tenant configuration (users, clients, segments, contacts, campaigns).

**Request:**
```json
{
  "slug": "my-app",
  "name": "My App",
  "registration_enabled": true,
  "smtp": {
    "smtp_host": "smtp.example.com",
    "smtp_port": "587",
    "smtp_user": "user",
    "smtp_password": "pass",
    "smtp_from": "hello@my-app.com",
    "smtp_from_name": "My App"
  },
  "admin": {"email": "admin@my-app.com", "password": "change-me"},
  "users": [
    {"email": "alice@my-app.com", "name": "Alice", "role": "member", "password": "alice1234"}
  ],
  "clients": [
    {"name": "My SPA", "redirect_uris": ["https://my-app.com/callback"]}
  ],
  "segments": [
    {"name": "newsletter", "description": "Subscribers"}
  ],
  "contacts": [
    {"email": "bob@example.com", "name": "Bob", "segments": ["newsletter"], "consent_source": "import"}
  ],
  "campaigns": [
    {"subject": "Welcome!", "html_body": "<h1>Hi {{.Name}}</h1>", "from_name": "My App", "from_email": "hello@my-app.com", "segments": ["newsletter"]}
  ]
}
```

**Response (201):**
```json
{
  "tenant_id": "uuid",
  "slug": "my-app",
  "clients": [
    {"name": "My SPA", "client_id": "uuid", "client_secret": "secret", "redirect_uris": ["https://my-app.com/callback"]}
  ]
}
```

### POST /admin/tenants/import-batch

Import multiple tenants at once. Request body is an array of import configs.

**Response (200):**
```json
[
  {"tenant_id": "uuid", "slug": "app-1", "clients": [...]},
  {"slug": "app-2", "skipped": true, "tenant_id": "existing-uuid"},
  {"slug": "app-3", "error": "invalid slug format"}
]
```

### GET /admin/tenants/{id}/export

Export a tenant's full configuration. Passwords are never included.

### GET /admin/tenants/{id}

Get a single tenant. SMTP password is redacted.

### DELETE /admin/tenants/{id}

Delete a tenant and all its data (cascades).

---

## Public

### GET /track/{id}

Open tracking pixel. Returns a 1x1 transparent GIF. Records the open event.

### GET /unsubscribe/{token}

Renders an HTML unsubscribe confirmation page.

### POST /unsubscribe/{token}

Confirms unsubscription. Renders an HTML success page.

### GET /health

Health check.

**Response (200):**
```json
{"status": "ok"}
```

---

## Email Template Variables

Use Go template syntax in campaign `html_body`:

| Variable | Description |
|---|---|
| `{{.Name}}` | Contact name |
| `{{.Email}}` | Contact email |
| `{{.UnsubscribeURL}}` | Unsubscribe link |
| `{{.TrackingPixelURL}}` | Open tracking pixel |
| `{{.InviteURL}}` | Account activation link |
