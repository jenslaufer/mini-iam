# Security Test Matrix

This document turns the test-only API analysis into a concrete security backlog.

## P0

| ID | Area | Endpoint / Flow | Preconditions | Abuse Case | Expected Result |
| --- | --- | --- | --- | --- | --- |
| P0-01 | Tenant isolation | `GET /admin/users` with `X-Tenant` | Regular tenant admin authenticated in tenant A | Send `X-Tenant: tenant-b` | `403` or equivalent denial; no tenant B data returned |
| P0-02 | Tenant isolation | Any admin API with `X-Tenant` | Platform admin authenticated | Send `X-Tenant` for another tenant from an untrusted origin/proxy path | Header ignored or request denied unless proxy trust conditions are met |
| P0-03 | Tenant isolation | Cross-tenant admin read | Platform admin authenticated | Read tenant B data via `X-Tenant` | `200` only if explicitly allowed; audit event recorded with actor tenant, target tenant, route, timestamp |
| P0-04 | Refresh token binding | `POST /token` | Refresh token issued for client A | Redeem same refresh token while presenting client B context | Request rejected with `400` or `401` |
| P0-05 | Refresh token replay | `POST /token` | Valid refresh token already redeemed once | Replay rotated refresh token | Request rejected; old token remains unusable |
| P0-06 | Tenant-scoped tokens | `POST /t/{slug}/token` | Refresh token minted in tenant A | Redeem token through tenant B route | Request rejected; no cross-tenant token reuse |
| P0-07 | Token revocation auth | `POST /revoke` | Confidential client exists | Revoke without authenticating client | Request rejected |
| P0-08 | Token revocation ownership | `POST /revoke` | Token belongs to client A | Client B tries to revoke token from client A | Request rejected |
| P0-09 | Invite token exposure | `POST /admin/contacts` | Admin authenticated | Create contact and inspect response/body/logging surface | Raw invite token returned only if intentionally required; otherwise omitted |
| P0-10 | Activate token replay | `POST /activate/{token}` | Valid activation token used once | Reuse same token | Request rejected with stable terminal status such as `404` or `410` |
| P0-11 | Unsubscribe token replay | `POST /unsubscribe/{token}` | Valid unsubscribe token used once | Reuse same token | Request remains safe and idempotent; no extra state changes |
| P0-12 | Lifecycle token expiry | `/activate/{token}` and `/unsubscribe/{token}` | Old token older than TTL | Attempt action with expired token | Request rejected; expired token cannot be used |
| P0-13 | RSA key storage | Tenant key generation / persistence | Tenant key pair created and stored | Inspect persisted key material at rest | Private keys encrypted at rest or wrapped by a key-management mechanism |
| P0-14 | Key rotation | JWKS and token validation flow | Tenant has existing signing key and active tokens | Rotate signing key and validate old/new tokens across grace period | Multiple `kid`s supported; old tokens validate during overlap; new tokens use new `kid` |
| P0-15 | Key strength policy | Tenant key generation | New tenant or rotated key created | Inspect generated RSA modulus size | Key size meets configured minimum, ideally 3072+ for long-lived deployments |
| P0-16 | SMTP secret storage | Tenant SMTP config persistence | Tenant SMTP password saved | Inspect persisted SMTP credentials | Secret encrypted at rest or stored via secret manager, not plaintext |

## P1

| ID | Area | Endpoint / Flow | Preconditions | Abuse Case | Expected Result |
| --- | --- | --- | --- | --- | --- |
| P1-01 | Registration abuse | `POST /register` or `POST /t/{slug}/register` | Registration enabled | Burst many signups from one IP / identity | Rate limited with explicit error contract |
| P1-02 | Login abuse | `POST /login` | Existing user | Repeated wrong passwords | Rate limited, delayed, or temporarily locked according to policy |
| P1-03 | Registration policy | `POST /t/{slug}/register` | Tenant imported with `registration_enabled=false` | Attempt self-registration | `403` with `registration_disabled` error |
| P1-04 | Redirect URI integrity | `/authorize` then `/token` | Valid auth code for registered client | Exchange code with different `redirect_uri` | Request rejected |
| P1-05 | Auth code replay | `POST /token` | Valid auth code already redeemed | Redeem same code again | Request rejected |
| P1-06 | Auth code expiry | `POST /token` | Expired auth code | Exchange expired code | Request rejected |
| P1-07 | PKCE integrity | `POST /token` | PKCE challenge created | Use wrong `code_verifier` | Request rejected |
| P1-08 | Client secret fallback | `POST /token` | Confidential client without PKCE | Exchange code with wrong or missing client secret | Request rejected |
| P1-09 | OIDC state handling | `/authorize` redirect flow | Browser-based auth request with `state` | Tamper with callback state or omit required state | Flow rejected or client-facing failure is explicit |
| P1-10 | Nonce handling | ID token issuance | OpenID flow with nonce | Replay or omit nonce when required | Token or flow validation fails |
| P1-11 | Campaign HTML safety | `POST /admin/campaigns` | Admin authenticated | Submit HTML with script/event handlers/unsafe URLs | Payload rejected or sanitized according to policy |
| P1-12 | Template safety | Campaign send flow | Campaign contains templating syntax | Attempt access to unintended template objects/functions | Rendering limited to approved fields only |
| P1-13 | Audience validation | Access token validation on protected APIs | Token minted for audience A | Present same token to endpoint expecting audience B | Request rejected because `aud` mismatch is enforced |
| P1-14 | Security headers | Browser-facing GET endpoints | Public and admin HTML/JSON routes accessible | Inspect response headers | Required headers present, such as `X-Content-Type-Options`, framing policy, HSTS where applicable, and CSP for HTML |
| P1-15 | Cross-tenant track scoping | `GET /track/{id}` | Two tenants with recipient identifiers | Call tracking endpoint with identifier from another tenant route/context | Only intended tenant data updated; no cross-tenant mutation |
| P1-16 | Segment link isolation | Contact/segment association APIs | Two tenants each with contacts and segments | Try to attach tenant A contact to tenant B segment | Request rejected; cross-tenant link impossible at DB and handler layers |
| P1-17 | Campaign link isolation | Campaign/segment association APIs | Two tenants each with campaigns and segments | Try to attach tenant A campaign to tenant B segment | Request rejected; cross-tenant link impossible at DB and handler layers |
| P1-18 | Email validation hardening | `POST /register`, contact import/create | Validation active | Submit control chars, malformed local parts, or SMTP-header-like payloads | Request rejected with stable validation error |

## P2

| ID | Area | Endpoint / Flow | Preconditions | Abuse Case | Expected Result |
| --- | --- | --- | --- | --- | --- |
| P2-01 | Import validation | `POST /admin/tenants/import` | Platform admin authenticated | Import payload with duplicate segment names | Request rejected transactionally |
| P2-02 | Import validation | `POST /admin/tenants/import` | Platform admin authenticated | Contact references unknown segment name | Request rejected transactionally |
| P2-03 | Import validation | `POST /admin/tenants/import` | Platform admin authenticated | Campaign references unknown segment name | Request rejected transactionally |
| P2-04 | Import idempotency | `POST /admin/tenants/import` | Existing tenant slug already imported | Re-import same payload | Stable duplicate/skip behavior; no partial duplication |
| P2-05 | Export minimization | `GET /admin/tenants/{id}/export` | Platform or tenant admin authenticated | Inspect export for hidden fields | No secrets, hashes, private keys, SMTP passwords, invite tokens, unsubscribe tokens |
| P2-06 | Browser CSRF | Browser POST flows such as `/authorize`, `/unsubscribe/{token}`, `/activate/{token}` | Cookie-based browser context if used | Cross-site form submission | Request blocked or safely non-destructive |
| P2-07 | Audit logging | User role changes | Admin authenticated | Promote user to admin | Audit record created with actor, target, old role, new role |
| P2-08 | Audit logging | Client lifecycle | Admin authenticated | Create or delete OAuth client | Audit record created |
| P2-09 | Audit logging | Tenant lifecycle | Platform admin authenticated | Import, export, delete tenant | Audit record created |
| P2-10 | Internal error exposure | `POST /admin/tenants/import` and other failure paths | Handler forced into error state | Trigger import/storage failure and inspect response | Generic client-safe error returned; internal error details not exposed |
| P2-11 | Path parameter validation | `/admin/users/{id}` and similar ID routes | Route reachable | Send clearly invalid non-UUID or malformed IDs | Request rejected early with `400`; invalid values do not flow into lower layers |
| P2-12 | Request body limits | JSON POST endpoints such as `/register`, `/admin/contacts/import`, `/admin/tenants/import` | Server default body handling | Send oversized payload | Request rejected with size-limit status before excessive allocation |
| P2-13 | SMTP transport security | SMTP send flow | SMTP server without STARTTLS or with invalid TLS posture | Attempt campaign send or SMTP verification | Credentials not sent over insecure transport unless explicitly allowed by policy |

## Suggested Execution Order

1. Add missing automated tests for all `P0` rows first.
2. For rows that fail, decide whether the system should deny, sanitize, rate limit, or audit.
3. Add regression coverage for each fix before changing implementation details.
4. Run the matrix for both default tenant and non-default tenant routes where applicable.

## Notes From Existing Tests

- Existing tests already cover tenant-specific JWKS separation, token response cache headers, and secret minimization in tenant export.
- Existing tests already prove that platform-admin cross-tenant access is intended behavior; the missing part is hardening around trust boundaries and auditability.
- Existing tests partially cover PKCE, but not the broader denial-path matrix around auth code misuse.
- `SECURITY.md` contains additional findings around key management, SMTP secret handling, `aud` validation, tracking scoping, input bounds, and response hardening; those are included here as testable rows as well.
