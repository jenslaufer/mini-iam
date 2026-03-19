# Launch Kit Admin Frontend

Admin dashboard for managing tenants, users, OAuth2 clients, contacts, segments, and email campaigns.

## Tech Stack

- Vue 3 (Composition API, `<script setup>`)
- Vite 7
- Tailwind CSS 4
- Vue Router 5
- Pinia 3 (state management)
- Axios (HTTP client)
- Heroicons (icons)
- Playwright (E2E tests)
- Vitest (unit tests)

## Project Structure

```
src/
  views/              Page components
    LoginView.vue       Login form
    DashboardView.vue   Overview stats
    UsersView.vue       User management (CRUD, role assignment)
    ClientsView.vue     OAuth2 client management
    ContactsView.vue    Marketing contacts (create, import, delete)
    SegmentsView.vue    Contact segments (CRUD, assign contacts)
    CampaignsView.vue   Email campaigns (create, edit, send, stats)
    TenantsView.vue     Multi-tenant management (platform admin)
    SettingsView.vue    User settings (password change)
  components/         Reusable UI
    AppLayout.vue       Main layout shell
    AppSidebar.vue      Navigation sidebar
    AppTopBar.vue       Top bar with tenant selector
    BaseModal.vue       Modal dialog
    BaseInput.vue       Form input
    BaseButton.vue      Button
    ConfirmDialog.vue   Confirmation prompt
    StatCard.vue        Dashboard stat card
    RoleBadge.vue       User role badge
    ToastContainer.vue  Toast notifications
    ToastItem.vue       Single toast
  api/                HTTP client modules
    client.js           Axios instance (auth + tenant interceptors)
    auth.js             Login
    account.js          Password change
    users.js            Admin user API
    clients.js          Admin client API
    contacts.js         Admin contact API
    segments.js         Admin segment API
    campaigns.js        Admin campaign API
    tenants.js          Admin tenant API
  stores/             Pinia stores
    auth.js             JWT token, login/logout
    tenant.js           Tenant list, selector, platform admin flag
    toast.js            Toast notifications
  router/             Vue Router configuration
e2e/                  Playwright E2E tests (66 tests)
nginx.conf            Production Nginx config (SPA + backend proxy)
```

## Development

```bash
npm install
npm run dev     # http://localhost:3000
```

The Vite dev server proxies `/auth/*` to `http://localhost:8080` (the Go backend). Start the backend first.

## Build

```bash
npm run build    # output: dist/
npm run preview  # preview production build
```

## Tests

```bash
npm run test         # Vitest unit tests
npm run test:e2e     # Playwright E2E (requires running backend)
npm run test:e2e:ui  # Playwright with interactive UI
```

## Backend Connection

The `api/client.js` Axios instance handles all backend communication:

- **Base URL**: `VITE_API_URL` env var (default: `/auth`)
- **Auth**: Automatically attaches `Authorization: Bearer <token>` from the auth store
- **Tenant**: Automatically attaches `X-Tenant: <slug>` from the tenant store
- **401 handling**: Clears token and redirects to `/login`

In production, Nginx serves the SPA and proxies `/auth/` to the backend:

```nginx
location /auth/ {
    proxy_pass http://launch-kit:8080/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Tenant $http_x_tenant;
}
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `VITE_API_URL` | `/auth` | Backend API base URL |
