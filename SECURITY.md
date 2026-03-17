# Security

## Reporting Vulnerabilities

Report security issues to the maintainers directly. Do not open public issues for vulnerabilities.

## Implemented Controls

- Per-tenant RS256 key pairs (isolated token signing)
- PKCE required for public OAuth2 clients (no unauthenticated code exchange)
- JWT issuer validation (`iss` claim checked against expected issuer)
- `Cache-Control: no-store` on all token responses (RFC 6749 §5.1)
- Password minimum 8 characters, email max 254 characters
- CORS origin allowlist (no wildcard default)
- Bcrypt password hashing (cost 10)
- Parameterized SQL queries (no injection)
- Refresh token rotation with revocation
- Platform admin / tenant admin role separation
- Tenant ID claim (`tid`) validated against request context
- Registration disabled by default per tenant (`registration_enabled`)
- HTML escaping on user-supplied values in rendered pages
- Tenant slug validation (`^[a-z0-9][a-z0-9-]{0,62}$`)
- Non-root container (runs as `iam` user)

## Open Findings

From internal security audit (2026-03-17). Ordered by priority.

### High — Fix before production

| ID | Finding | Risk | Effort |
|---|---|---|---|
| C-3 | **No rate limiting** on `/login`, `/register`, `/authorize`, `/token` | Brute-force, credential stuffing, DoS | Medium |
| H-2 | **RSA private keys stored unencrypted** in SQLite | Token forgery on DB compromise | Medium |
| H-3 | **RSA key size 2048 bit** — NIST recommends 3072+ for use beyond 2030 | Key may become weak before rotation | Low |
| H-4 | **No key rotation** — `kid: "main"` hardcoded, keys created once per tenant | Compromised keys cannot be rotated without downtime | High |
| H-5 | **SMTP passwords stored in plaintext** in `tenants` table | Credential exposure on DB compromise | Medium |

### Medium — Defense in depth

| ID | Finding | Risk | Effort |
|---|---|---|---|
| M-2 | **No `aud` (audience) validation** in `ValidateAccessToken` | Tokens intended for one client accepted by all endpoints | Low |
| M-3 | **No security headers** — missing `X-Content-Type-Options`, `X-Frame-Options`, HSTS, CSP | Clickjacking, MIME sniffing | Low |
| M-5 | **Junction tables lack tenant_id** — `contact_segments`, `campaign_segments` have no tenant column | Theoretical cross-tenant linking if UUIDs leak | Medium |
| M-6 | **`RecordOpen` lacks tenant scoping** — `/track/{id}` updates any tenant's tracking data | Cross-tenant tracking manipulation via public endpoint | Low |
| M-7 | **Email regex overly permissive** — accepts edge cases (control chars, long local parts) | Malformed emails, potential SMTP header injection | Low |

### Low — Hardening

| ID | Finding | Risk | Effort |
|---|---|---|---|
| L-1 | **Internal errors exposed** — `err.Error()` returned to client on import failure | Information disclosure | Low |
| L-2 | **Path parsing unvalidated** — `TrimPrefix` on `/admin/users/` accepts non-UUID strings | Unexpected values reaching DB queries (mitigated by parameterized SQL) | Low |
| L-3 | **No request body size limit** — handlers accept arbitrarily large JSON payloads | Memory exhaustion DoS | Low |
| I-1 | **SMTP TLS not enforced** — `smtp.SendMail` uses opportunistic STARTTLS | Credentials in plaintext on misconfigured networks | Low |

## Resolved Findings

| ID | Finding | Resolution | Commit |
|---|---|---|---|
| C-1 | CORS default `*` | Origin allowlist validation | `591f976` |
| C-2 | No password minimum on `/register` | 8-char minimum enforced | `591f976` |
| H-1 | PKCE not enforced for public clients | Require PKCE or client_secret | `591f976` |
| M-1 | Missing `iss` validation | `jwt.WithIssuer()` added | `591f976` |
| M-4 | No `Cache-Control` on token responses | `no-store` + `no-cache` on all token responses | `591f976` |
