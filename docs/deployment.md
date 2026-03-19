# Deployment Guide

## Docker Compose (Production)

The root `docker-compose.yml` runs two services:

- **launch-kit** — Go backend (IAM + marketing API)
- **frontend** — Nginx serving the Vue admin UI, proxying `/auth/` to the backend

```bash
cp .env.example .env   # edit credentials and URLs
docker compose up -d
```

The frontend is exposed on `EXTERNAL_PORT` (default 3000). The backend is internal — the frontend's Nginx proxies requests to it.

### Environment Variables

See [.env.example](../.env.example) for all supported variables with descriptions.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Backend HTTP port (internal) |
| `ISSUER_URL` | `http://localhost:8080` | Public URL used as JWT issuer |
| `CORS_ORIGINS` | — | Comma-separated allowed origins |
| `DATABASE_PATH` | `launch-kit.db` | SQLite file path |
| `ADMIN_EMAIL` | — | Seed admin email |
| `ADMIN_PASSWORD` | — | Seed admin password |
| `DEFAULT_TENANT` | — | Default tenant slug |
| `DEFAULT_TENANT_REGISTRATION` | — | Enable public registration on default tenant |
| `TENANT_CONFIG` | — | Path to JSON for auto-import on startup |
| `SMTP_HOST` | — | SMTP server (empty = log-only) |
| `SMTP_PORT` | `587` | SMTP port |
| `SMTP_USER` | — | SMTP username |
| `SMTP_PASSWORD` | — | SMTP password |
| `SMTP_FROM` | — | Sender email |
| `SMTP_FROM_NAME` | `launch-kit` | Sender display name |
| `SMTP_RATE_MS` | `100` | Delay between emails (ms) |
| `EXTERNAL_PORT` | `3000` | Host port for the frontend |

## Reverse Proxy (Nginx)

For production, put Nginx in front for SSL termination:

```nginx
server {
    listen 443 ssl;
    server_name auth.example.com;

    ssl_certificate     /etc/ssl/certs/auth.example.com.pem;
    ssl_certificate_key /etc/ssl/private/auth.example.com.key;

    # Admin UI
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Set `ISSUER_URL=https://auth.example.com` so JWT `iss` claims use the public URL.

## SMTP Configuration

### Common Providers

**Gmail (App Password):**
```env
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=you@gmail.com
SMTP_PASSWORD=your-app-password
SMTP_FROM=you@gmail.com
```

**Mailgun:**
```env
SMTP_HOST=smtp.mailgun.org
SMTP_PORT=587
SMTP_USER=postmaster@mg.example.com
SMTP_PASSWORD=your-mailgun-password
SMTP_FROM=noreply@example.com
```

**Amazon SES:**
```env
SMTP_HOST=email-smtp.us-east-1.amazonaws.com
SMTP_PORT=587
SMTP_USER=your-ses-smtp-user
SMTP_PASSWORD=your-ses-smtp-password
SMTP_FROM=noreply@example.com
```

**Generic SMTP:**
```env
SMTP_HOST=mail.example.com
SMTP_PORT=587
SMTP_USER=user@example.com
SMTP_PASSWORD=password
SMTP_FROM=user@example.com
```

### Per-Tenant SMTP

Each tenant can have its own SMTP configuration. Set via the tenant import API:

```json
{
  "slug": "my-app",
  "name": "My App",
  "smtp": {
    "smtp_host": "smtp.my-app.com",
    "smtp_port": "587",
    "smtp_user": "noreply@my-app.com",
    "smtp_password": "secret",
    "smtp_from": "noreply@my-app.com",
    "smtp_from_name": "My App"
  }
}
```

If a tenant has no SMTP config, the global SMTP settings are used. If no global SMTP is configured, emails are logged to stdout.

## SQLite

Launch Kit uses SQLite with WAL mode and foreign keys enabled.

### Volume Mount

In Docker Compose, data lives in a named volume:

```yaml
volumes:
  - launch-kit-data:/data
```

The default database path is `/data/launch-kit.db` inside the container.

### Backups

```bash
# Copy the database file while the container is running (WAL-safe)
docker compose exec launch-kit sqlite3 /data/launch-kit.db ".backup /data/backup.db"
docker compose cp launch-kit:/data/backup.db ./backup.db
```

SQLite WAL mode allows concurrent reads during backup. For zero-downtime backups, use `.backup` rather than copying the file directly.

### Performance

SQLite handles thousands of concurrent reads efficiently. Write throughput is sequential but sufficient for most deployments. If you need higher write throughput, consider running multiple Launch Kit instances with separate databases per tenant group.

## Health Check

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

Docker Compose includes a built-in health check that polls `/health` every 30 seconds.

## Multi-Tenant Setup

### Creating Tenants

**Option 1: Auto-import on startup** — set `TENANT_CONFIG` to a JSON file path:

```env
TENANT_CONFIG=/data/tenant-config.json
```

**Option 2: API import** — use the platform admin API:

```bash
TOKEN=$(curl -s http://localhost:8080/login \
  -d '{"email":"admin@launch-kit.local","password":"changeme"}' | jq -r .access_token)

curl -X POST http://localhost:8080/admin/tenants/import \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "my-app",
    "name": "My App",
    "registration_enabled": true,
    "admin": {"email": "admin@my-app.com", "password": "change-me"},
    "clients": [{"name": "My SPA", "redirect_uris": ["https://my-app.com/callback"]}]
  }'
```

**Option 3: Batch import** — import multiple tenants in one request:

```bash
curl -X POST http://localhost:8080/admin/tenants/import-batch \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '[
    {"slug": "app-1", "name": "App One", "admin": {"email": "admin@app1.com", "password": "..."}},
    {"slug": "app-2", "name": "App Two", "admin": {"email": "admin@app2.com", "password": "..."}}
  ]'
```

## Security Checklist

- [ ] Change `ADMIN_PASSWORD` from default
- [ ] Set `CORS_ORIGINS` to your specific domains (not `*`)
- [ ] Use HTTPS in production (`ISSUER_URL` must use `https://`)
- [ ] Set strong passwords for all admin accounts
- [ ] Configure SMTP with authentication (not open relay)
- [ ] Mount SQLite volume to persistent storage
- [ ] Back up the database regularly
- [ ] Keep `DEFAULT_TENANT_REGISTRATION` empty unless needed
- [ ] Review [SECURITY.md](../SECURITY.md) for known findings
