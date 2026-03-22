package tenant_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/marketing"
	"github.com/jenslaufer/launch-kit/tenant"
	_ "modernc.org/sqlite"
)

// newRoutingDB creates a migrated in-memory SQLite database for routing tests.
func newRoutingDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY, slug TEXT UNIQUE NOT NULL, name TEXT NOT NULL,
		registration_enabled INTEGER NOT NULL DEFAULT 0,
		smtp_host TEXT NOT NULL DEFAULT '', smtp_port TEXT NOT NULL DEFAULT '',
		smtp_user TEXT NOT NULL DEFAULT '', smtp_password TEXT NOT NULL DEFAULT '',
		smtp_from TEXT NOT NULL DEFAULT '', smtp_from_name TEXT NOT NULL DEFAULT '',
		smtp_rate_ms INTEGER NOT NULL DEFAULT 0, smtp_tls_mode TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL,
		password_hash TEXT NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user',
		created_at DATETIME NOT NULL, reset_token TEXT, reset_token_expires_at DATETIME,
		UNIQUE(tenant_id, email)
	);
	CREATE TABLE IF NOT EXISTS clients (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', secret_hash TEXT NOT NULL,
		name TEXT NOT NULL, redirect_uris TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS auth_codes (
		code TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL,
		user_id TEXT NOT NULL, redirect_uri TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '',
		nonce TEXT NOT NULL DEFAULT '', code_challenge TEXT NOT NULL DEFAULT '',
		code_challenge_method TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL,
		used INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS refresh_tokens (
		token TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL,
		user_id TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL,
		revoked INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS keys (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', private_key_pem TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS contacts (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '', user_id TEXT REFERENCES users(id),
		unsubscribed INTEGER NOT NULL DEFAULT 0, unsubscribe_token TEXT UNIQUE NOT NULL,
		invite_token TEXT UNIQUE, invite_token_expires_at DATETIME, consent_source TEXT NOT NULL, consent_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL, UNIQUE(tenant_id, email)
	);
	CREATE TABLE IF NOT EXISTS segments (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL,
		UNIQUE(tenant_id, name)
	);
	CREATE TABLE IF NOT EXISTS contact_segments (
		contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (contact_id, segment_id)
	);
	CREATE TABLE IF NOT EXISTS campaigns (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', subject TEXT NOT NULL,
		html_body TEXT NOT NULL, from_name TEXT NOT NULL DEFAULT '',
		from_email TEXT NOT NULL DEFAULT '', attachment_url TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'draft', sent_at DATETIME, created_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS campaign_segments (
		campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (campaign_id, segment_id)
	);
	CREATE TABLE IF NOT EXISTS campaign_recipients (
		id TEXT PRIMARY KEY, campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		contact_id TEXT NOT NULL REFERENCES contacts(id), status TEXT NOT NULL DEFAULT 'queued',
		error_message TEXT NOT NULL DEFAULT '', sent_at DATETIME, opened_at DATETIME,
		UNIQUE(campaign_id, contact_id)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

// routingEnv holds the wired-up server and tenant store for routing tests.
type routingEnv struct {
	srv         *httptest.Server
	tenantStore *tenant.Store
	iamStore    *iam.Store
}

func newRoutingEnv(t *testing.T) *routingEnv {
	t.Helper()
	db := newRoutingDB(t)

	tenantStore := tenant.NewStore(db)
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)

	registry := iam.NewTokenRegistry(iamStore, "http://test-issuer")

	iamHandler := iam.NewHandler(iamStore, registry, "http://test-issuer")
	mktHandler := marketing.NewHandler(mktStore, iamStore, registry)

	mux := http.NewServeMux()
	mux.HandleFunc("/login", iamHandler.Login)
	mux.HandleFunc("/register", iamHandler.Register)
	mux.HandleFunc("/token", iamHandler.Token)
	mux.HandleFunc("/.well-known/openid-configuration", iamHandler.Discovery)
	mux.HandleFunc("/jwks", iamHandler.JWKS)
	mux.HandleFunc("/userinfo", iamHandler.UserInfo)
	mux.HandleFunc("/authorize", iamHandler.Authorize)
	mux.HandleFunc("/revoke", iamHandler.Revoke)
	mux.HandleFunc("/clients", iamHandler.CreateClient)
	mux.HandleFunc("/activate/", iamHandler.Activate)
	mux.HandleFunc("/track/", mktHandler.TrackOpen)
	mux.HandleFunc("/unsubscribe/", mktHandler.Unsubscribe)
	mux.HandleFunc("/admin/users", iamHandler.AdminListUsers)
	mux.HandleFunc("/admin/contacts/import", mktHandler.AdminImportContacts)
	mux.HandleFunc("/admin/contacts", mktHandler.AdminContacts)
	mux.HandleFunc("/admin/contacts/", mktHandler.AdminContactByID)
	mux.HandleFunc("/admin/segments", mktHandler.AdminSegments)
	mux.HandleFunc("/admin/segments/", mktHandler.AdminSegmentByID)
	mux.HandleFunc("/admin/campaigns", mktHandler.AdminCampaigns)
	mux.HandleFunc("/admin/campaigns/", mktHandler.AdminCampaignByID)

	// Middleware handles both /t/{slug}/... path prefix and X-Tenant header
	handler := tenant.Middleware(tenantStore, "")(mux)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &routingEnv{
		srv:         srv,
		tenantStore: tenantStore,
		iamStore:    iamStore,
	}
}

func createTestTenant(t *testing.T, store *tenant.Store, slug string) *tenant.Tenant {
	t.Helper()
	tn, err := store.Create(slug, slug)
	if err != nil {
		t.Fatalf("create tenant %q: %v", slug, err)
	}
	return tn
}

// --- Tests ---

func TestPathPrefix_LoginResolvesSlug(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	body := `{"email":"x@example.com","password":"wrong"}`
	resp, err := http.Post(env.srv.URL+"/t/my-tenant/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("expected handler to be reached, got 404")
	}
}

func TestPathPrefix_RegisterResolvesSlug(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	body := `{"email":"new@example.com","password":"pass123","name":"New"}`
	resp, err := http.Post(env.srv.URL+"/t/my-tenant/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("expected handler to be reached, got 404")
	}
}

func TestPathPrefix_TokenEndpoint(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	resp, err := http.Post(env.srv.URL+"/t/my-tenant/token", "application/x-www-form-urlencoded",
		strings.NewReader("grant_type=authorization_code&code=bad"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("got 404 — route /t/my-tenant/token not handled")
	}
}

func TestPathPrefix_WellKnownOpenIDConfiguration(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	resp, err := http.Get(env.srv.URL + "/t/my-tenant/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPathPrefix_JWKS(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	resp, err := http.Get(env.srv.URL + "/t/my-tenant/jwks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPathPrefix_UnknownTenantReturns404(t *testing.T) {
	env := newRoutingEnv(t)

	resp, err := http.Get(env.srv.URL + "/t/ghost/jwks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown tenant, got %d", resp.StatusCode)
	}
}

func TestPathPrefix_TenantIDInjectedIntoContext(t *testing.T) {
	env := newRoutingEnv(t)
	tn := createTestTenant(t, env.tenantStore, "ctx-tenant")

	scopedStore := env.iamStore.ForTenant(tn.ID)
	if err := scopedStore.SeedAdmin("admin@example.com", "secret123", "Admin"); err != nil {
		t.Fatal(err)
	}

	body := `{"email":"admin@example.com","password":"secret123"}`
	resp, err := http.Post(env.srv.URL+"/t/ctx-tenant/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 login, got %d: %s", resp.StatusCode, b)
	}
}

func TestPathPrefix_PathStrippedBeforeHandler(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "strip-test")

	resp, err := http.Get(env.srv.URL + "/t/strip-test/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("path not stripped before handler: got %d (expected 200)", resp.StatusCode)
	}
}

func TestPathPrefix_TrackEndpoint(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	resp, err := http.Get(env.srv.URL + "/t/my-tenant/track/some-id")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// TrackOpen returns a GIF pixel, so 200 is expected
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for track endpoint, got %d", resp.StatusCode)
	}
}

func TestPathPrefix_UnsubscribeEndpoint(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	resp, err := http.Get(env.srv.URL + "/t/my-tenant/unsubscribe/some-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	// Handler responds with HTML for invalid tokens — this proves the route was found.
	// A routing miss returns plain "404 page not found".
	if resp.StatusCode == http.StatusNotFound && strings.Contains(string(b), "page not found") {
		t.Fatalf("routing miss for /t/my-tenant/unsubscribe/: %s", b)
	}
}

func TestPathPrefix_ActivateEndpoint(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "my-tenant")

	resp, err := http.Get(env.srv.URL + "/t/my-tenant/activate/some-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	// Handler responds with HTML for invalid tokens — this proves the route was found.
	if resp.StatusCode == http.StatusNotFound && strings.Contains(string(b), "page not found") {
		t.Fatalf("routing miss for /t/my-tenant/activate/: %s", b)
	}
}

func TestPathPrefix_AdminAPIUsesXTenantHeader(t *testing.T) {
	env := newRoutingEnv(t)
	tn := createTestTenant(t, env.tenantStore, "header-tenant")

	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/admin/users", nil)
	req.Header.Set("X-Tenant", tn.Slug)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Expect 401 (no auth token), not 404 (route found)
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("admin route not found — /admin/users should work with X-Tenant header")
	}
}

func TestPathPrefix_SubdomainDoesNotResolveTenant(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "sub-tenant")

	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/jwks", nil)
	req.Host = "sub-tenant.example.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(resp.Body)
		json.Unmarshal(b, &e)
		if e.Error == "unknown_tenant" {
			t.Fatal("subdomain still resolves tenant — subdomain logic should be removed")
		}
	}
}

func TestPathPrefix_DiscoveryContainsTenantIssuer(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "disco-test")

	resp, err := http.Get(env.srv.URL + "/t/disco-test/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var doc struct {
		Issuer   string `json:"issuer"`
		JwksURI  string `json:"jwks_uri"`
		TokenURL string `json:"token_endpoint"`
	}
	json.NewDecoder(resp.Body).Decode(&doc)

	if !strings.Contains(doc.Issuer, "/t/disco-test") {
		t.Errorf("issuer = %q, expected it to contain /t/disco-test", doc.Issuer)
	}
	if !strings.Contains(doc.JwksURI, "/t/disco-test/jwks") {
		t.Errorf("jwks_uri = %q, expected it to contain /t/disco-test/jwks", doc.JwksURI)
	}
}

func TestPathPrefix_PerTenantJWKSDifferentKeys(t *testing.T) {
	env := newRoutingEnv(t)
	createTestTenant(t, env.tenantStore, "tenant-a")
	createTestTenant(t, env.tenantStore, "tenant-b")

	getJWKSModulus := func(slug string) string {
		resp, err := http.Get(env.srv.URL + "/t/" + slug + "/jwks")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var jwks struct {
			Keys []map[string]interface{} `json:"keys"`
		}
		json.NewDecoder(resp.Body).Decode(&jwks)
		if len(jwks.Keys) == 0 {
			t.Fatalf("no keys for tenant %s", slug)
		}
		return jwks.Keys[0]["n"].(string)
	}

	nA := getJWKSModulus("tenant-a")
	nB := getJWKSModulus("tenant-b")

	if nA == nB {
		t.Fatal("tenant-a and tenant-b have the same JWKS public key; expected distinct per-tenant keys")
	}
}

func TestPlatformAdminCanAccessOtherTenantUsers(t *testing.T) {
	env := newRoutingEnv(t)

	// Create default (platform) tenant and a second tenant
	platform := createTestTenant(t, env.tenantStore, "platform")
	other := createTestTenant(t, env.tenantStore, "other-app")

	// Re-create env with platform as default tenant so CheckAdmin can recognize it
	db := newRoutingDB(t)
	tenantStore := tenant.NewStore(db)
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)

	platformTn, _ := tenantStore.Create("platform", "platform")
	otherTn, _ := tenantStore.Create("other-app", "Other App")
	_ = platform
	_ = other

	// Seed platform admin
	platformIAM := iamStore.ForTenant(platformTn.ID)
	platformIAM.SeedAdmin("admin@platform.com", "adminpass123", "Platform Admin")

	// Seed a user in other tenant
	otherIAM := iamStore.ForTenant(otherTn.ID)
	otherIAM.CreateUser("user@other.com", "password123", "Other User")

	registry := iam.NewTokenRegistry(iamStore, "http://test-issuer")
	iamHandler := iam.NewHandler(iamStore, registry, "http://test-issuer")
	iamHandler.PlatformTenantID = platformTn.ID
	mktHandler := marketing.NewHandler(mktStore, iamStore, registry)
	mktHandler.PlatformTenantID = platformTn.ID

	mux := http.NewServeMux()
	mux.HandleFunc("/login", iamHandler.Login)
	mux.HandleFunc("/admin/users", iamHandler.AdminListUsers)
	mux.HandleFunc("/admin/contacts", mktHandler.AdminContacts)

	handler := tenant.Middleware(tenantStore, platformTn.ID)(mux)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Login as platform admin (via path prefix)
	loginBody := `{"email":"admin@platform.com","password":"adminpass123"}`
	resp, err := http.Post(srv.URL+"/t/platform/login", "application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if tok.AccessToken == "" {
		t.Fatal("platform admin login failed")
	}

	// Access other tenant's users using X-Tenant header
	req, _ := http.NewRequest("GET", srv.URL+"/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("X-Tenant", "other-app")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("platform admin accessing other tenant users: status = %d, want 200, body = %s", resp.StatusCode, b)
	}
}

// marketingTenantEnv creates two tenants with admin users and a wired server
// for marketing endpoint routing tests. Returns the server, slugs, and tokens.
func marketingTenantEnv(t *testing.T) (srv *httptest.Server, slugA, slugB, tokenA, tokenB string) {
	t.Helper()
	db := newRoutingDB(t)
	tenantStore := tenant.NewStore(db)
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)

	tnA, _ := tenantStore.Create("tenant-a", "Tenant A")
	tnB, _ := tenantStore.Create("tenant-b", "Tenant B")

	iamStore.ForTenant(tnA.ID).SeedAdmin("admin-a@test.com", "pass123", "Admin A")
	iamStore.ForTenant(tnB.ID).SeedAdmin("admin-b@test.com", "pass123", "Admin B")

	registry := iam.NewTokenRegistry(iamStore, "http://test-issuer")
	iamHandler := iam.NewHandler(iamStore, registry, "http://test-issuer")
	mktHandler := marketing.NewHandler(mktStore, iamStore, registry)

	mux := http.NewServeMux()
	mux.HandleFunc("/login", iamHandler.Login)
	mux.HandleFunc("/admin/contacts", mktHandler.AdminContacts)
	mux.HandleFunc("/admin/segments", mktHandler.AdminSegments)
	mux.HandleFunc("/admin/campaigns", mktHandler.AdminCampaigns)

	handler := tenant.Middleware(tenantStore, "")(mux)
	srv = httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	login := func(slug, email string) string {
		body := `{"email":"` + email + `","password":"pass123"}`
		resp, err := http.Post(srv.URL+"/t/"+slug+"/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var tok struct {
			AccessToken string `json:"access_token"`
		}
		json.NewDecoder(resp.Body).Decode(&tok)
		if tok.AccessToken == "" {
			t.Fatalf("login failed for %s on %s", email, slug)
		}
		return tok.AccessToken
	}

	tokenA = login("tenant-a", "admin-a@test.com")
	tokenB = login("tenant-b", "admin-b@test.com")
	return srv, "tenant-a", "tenant-b", tokenA, tokenB
}

func TestMarketingEndpointsTenantRouting(t *testing.T) {
	srv, slugA, slugB, tokenA, tokenB := marketingTenantEnv(t)

	createContact := func(slug, token, email string) {
		body := `{"email":"` + email + `","name":"Test"}`
		req, _ := http.NewRequest("POST", srv.URL+"/t/"+slug+"/admin/contacts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create contact on %s: status %d, body %s", slug, resp.StatusCode, b)
		}
	}

	// Create contacts in each tenant
	createContact(slugA, tokenA, "alice@a.com")
	createContact(slugA, tokenA, "bob@a.com")
	createContact(slugB, tokenB, "carol@b.com")

	listContacts := func(slug, token string) []map[string]interface{} {
		req, _ := http.NewRequest("GET", srv.URL+"/t/"+slug+"/admin/contacts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("list contacts on %s: status %d, body %s", slug, resp.StatusCode, b)
		}
		var contacts []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&contacts)
		return contacts
	}

	// Tenant A should have 2 contacts
	contactsA := listContacts(slugA, tokenA)
	if len(contactsA) != 2 {
		t.Errorf("tenant-a contacts: got %d, want 2", len(contactsA))
	}

	// Tenant B should have 1 contact
	contactsB := listContacts(slugB, tokenB)
	if len(contactsB) != 1 {
		t.Errorf("tenant-b contacts: got %d, want 1", len(contactsB))
	}

	// Verify tenant A's contacts are alice and bob
	emails := map[string]bool{}
	for _, c := range contactsA {
		emails[c["email"].(string)] = true
	}
	if !emails["alice@a.com"] || !emails["bob@a.com"] {
		t.Errorf("tenant-a contacts missing expected emails: %v", emails)
	}

	// Verify tenant B's contact is carol
	if contactsB[0]["email"] != "carol@b.com" {
		t.Errorf("tenant-b contact email = %v, want carol@b.com", contactsB[0]["email"])
	}
}

func TestMarketingCrossTenantIsolation(t *testing.T) {
	srv, _, slugB, tokenA, _ := marketingTenantEnv(t)

	// Tenant A's admin token should NOT access tenant B's contacts
	req, _ := http.NewRequest("GET", srv.URL+"/t/"+slugB+"/admin/contacts", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("cross-tenant access: got status %d, want 403, body: %s", resp.StatusCode, b)
	}

	// Also test POST (create) cross-tenant
	body := `{"email":"evil@attacker.com","name":"Evil"}`
	req, _ = http.NewRequest("POST", srv.URL+"/t/"+slugB+"/admin/contacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenA)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("cross-tenant create: got status %d, want 403, body: %s", resp.StatusCode, b)
	}
}
