# Integration Guide

How to use Launch Kit as the IAM and marketing backend for your application.

## Using Launch Kit as OIDC Provider

Your app delegates authentication to Launch Kit and validates JWTs. Three env vars are all you need:

```env
OIDC_ISSUER_URL=http://localhost:8080/t/my-app    # Must match `iss` claim
OIDC_JWKS_URI=http://launch-kit:8080/t/my-app/jwks # Internal URL in Docker
OIDC_AUDIENCE=                                      # Optional audience check
```

To switch to Keycloak, Auth0, or any OIDC provider — change these three vars. No code changes.

## PKCE Authorization Code Flow

Step-by-step for a single-page app:

### 1. Register an OAuth2 Client

```bash
TOKEN=$(curl -s http://localhost:8080/login \
  -d '{"email":"admin@my-app.com","password":"secret"}' \
  -H "X-Tenant: my-app" | jq -r .access_token)

curl -X POST http://localhost:8080/clients \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant: my-app" \
  -d '{"name": "My SPA", "redirect_uris": ["http://localhost:4000/callback"]}'
```

Save the returned `client_id` and `client_secret`.

### 2. Generate PKCE Challenge

```javascript
function generatePKCE() {
  const verifier = crypto.randomUUID() + crypto.randomUUID()
  const encoder = new TextEncoder()
  const data = encoder.encode(verifier)
  return crypto.subtle.digest('SHA-256', data).then(hash => {
    const challenge = btoa(String.fromCharCode(...new Uint8Array(hash)))
      .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    return { verifier, challenge }
  })
}
```

### 3. Redirect to Authorize

```
http://localhost:8080/t/my-app/authorize?
  response_type=code&
  client_id=CLIENT_ID&
  redirect_uri=http://localhost:4000/callback&
  scope=openid profile email&
  state=random-state&
  code_challenge=CHALLENGE&
  code_challenge_method=S256
```

The user sees a login form. After authentication, Launch Kit redirects to your `redirect_uri` with `?code=...&state=...`.

### 4. Exchange Code for Tokens

```bash
curl -X POST http://localhost:8080/t/my-app/token \
  -d "grant_type=authorization_code" \
  -d "code=AUTH_CODE" \
  -d "redirect_uri=http://localhost:4000/callback" \
  -d "client_id=CLIENT_ID" \
  -d "code_verifier=VERIFIER"
```

Response:

```json
{
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_token": "rt-...",
  "id_token": "eyJ..."
}
```

### 5. Refresh Tokens

```bash
curl -X POST http://localhost:8080/t/my-app/token \
  -d "grant_type=refresh_token" \
  -d "refresh_token=rt-..."
```

## Token Validation

Validate tokens using the JWKS endpoint. Tokens are RS256-signed.

**JWKS endpoint:** `GET /t/{slug}/jwks`

**Key validation rules:**
- Algorithm: RS256
- Issuer (`iss`): must match `OIDC_ISSUER_URL`
- Audience (`aud`): optional, matches `client_id`
- Expiry (`exp`): tokens are valid for 1 hour
- Tenant (`tid`): tenant UUID for cross-tenant isolation

## Example: FastAPI Backend

From `example/demo-backend/main.py` — a complete working example:

```python
import os
import jwt
from fastapi import Depends, FastAPI, HTTPException, status
from fastapi.security import HTTPBearer

OIDC_ISSUER_URL = os.environ["OIDC_ISSUER_URL"].rstrip("/")
OIDC_JWKS_URI = os.environ["OIDC_JWKS_URI"]
OIDC_AUDIENCE = os.environ.get("OIDC_AUDIENCE", "")

app = FastAPI()
security = HTTPBearer()
jwks_client: jwt.PyJWKClient = None

@app.on_event("startup")
def startup():
    global jwks_client
    jwks_client = jwt.PyJWKClient(OIDC_JWKS_URI, cache_jwk_set=True, lifespan=300)

def get_current_user(creds=Depends(security)) -> dict:
    """Validate JWT: signature via JWKS, issuer, expiry."""
    try:
        signing_key = jwks_client.get_signing_key_from_jwt(creds.credentials)
        payload = jwt.decode(
            creds.credentials,
            signing_key.key,
            algorithms=["RS256"],
            issuer=OIDC_ISSUER_URL,
            audience=OIDC_AUDIENCE or None,
            options={"verify_aud": bool(OIDC_AUDIENCE)},
        )
        return {"sub": payload["sub"], "email": payload.get("email")}
    except jwt.InvalidTokenError as e:
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, str(e))

@app.get("/api/dashboard")
def dashboard(user=Depends(get_current_user)):
    return {"email": user["email"], "message": "Welcome!"}
```

**Dependencies:** `pip install fastapi uvicorn PyJWT[crypto]`

**Environment:**
```env
OIDC_ISSUER_URL=http://localhost:8080/t/my-app
OIDC_JWKS_URI=http://launch-kit:8080/t/my-app/jwks
```

## Example: Vue 3 Frontend with OIDC Login

The demo app at `example/demo-frontend/` shows a complete Vue 3 SPA that:

1. Redirects to Launch Kit's `/authorize` endpoint with PKCE
2. Handles the callback with the authorization code
3. Exchanges the code for tokens via `/token`
4. Stores tokens and attaches them to API requests

Key configuration:

```env
VITE_OIDC_ISSUER_URL=http://localhost:8080/t/my-app
VITE_API_URL=/api
```

The frontend builds the authorize URL from the OIDC issuer and redirects the user. On callback, it exchanges the code using the PKCE verifier stored in `sessionStorage`.

## Marketing API Integration

### Creating Contacts

```bash
curl -X POST http://localhost:8080/admin/contacts \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "X-Tenant: my-app" \
  -d '{"email": "alice@example.com", "name": "Alice"}'
```

The response includes an `invite_token` for the activation flow.

### Managing Segments

```bash
# Create a segment
curl -X POST http://localhost:8080/admin/segments \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "X-Tenant: my-app" \
  -d '{"name": "newsletter", "description": "Newsletter subscribers"}'

# Add a contact to the segment
curl -X POST http://localhost:8080/admin/segments/SEG_ID/contacts \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "X-Tenant: my-app" \
  -d '{"contact_id": "CONTACT_ID"}'
```

### Sending Campaigns

```bash
# Create a campaign targeting a segment
curl -X POST http://localhost:8080/admin/campaigns \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "X-Tenant: my-app" \
  -d '{
    "subject": "Welcome!",
    "html_body": "<h1>Hello {{.Name}}</h1><p><a href=\"{{.UnsubscribeURL}}\">Unsubscribe</a></p>",
    "from_name": "My App",
    "from_email": "hello@my-app.com",
    "segment_ids": ["SEG_ID"]
  }'

# Send it
curl -X POST http://localhost:8080/admin/campaigns/CAMPAIGN_ID/send \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "X-Tenant: my-app"
```

Delivery is asynchronous. Check progress with `GET /admin/campaigns/CAMPAIGN_ID/stats`.

## Invite-Based Onboarding Flow

Launch Kit supports an invite flow where contacts become users:

1. **Create a contact** via `POST /admin/contacts` — returns an `invite_token`
2. **Send an invite email** with a campaign containing `{{.InviteURL}}`
3. **User clicks the link** — `GET /activate/{token}` renders a password form
4. **User sets password** — `POST /activate/{token}` creates the user account
5. **User can now log in** via `/login` or the OAuth2 flow

This enables a subscribe-then-activate pattern. The demo app at `example/demo-backend/main.py` shows a complete implementation of the subscribe → activate → login flow.

## Tenant Import/Export for Data Migration

### Export a Tenant

```bash
curl http://localhost:8080/admin/tenants/TENANT_ID/export \
  -H "Authorization: Bearer $PLATFORM_ADMIN_TOKEN"
```

Returns the full tenant config (users, clients, segments, contacts, draft campaigns). Passwords and SMTP secrets are never exported.

### Import to Another Instance

Take the exported JSON, add passwords, and import:

```bash
curl -X POST http://localhost:8080/admin/tenants/import \
  -H "Authorization: Bearer $PLATFORM_ADMIN_TOKEN" \
  -d @tenant-export.json
```

If the slug already exists, the import is skipped (409 Conflict).

### Batch Migration

Import multiple tenants in one request:

```bash
curl -X POST http://localhost:8080/admin/tenants/import-batch \
  -H "Authorization: Bearer $PLATFORM_ADMIN_TOKEN" \
  -d '[{"slug":"app-1",...}, {"slug":"app-2",...}]'
```

Returns per-tenant results with status, created client credentials, or errors.
