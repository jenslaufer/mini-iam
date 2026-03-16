package tenant_test

// export_import_test.go tests the tenant export (GET /admin/tenants/{id}/export)
// and import (POST /admin/tenants/import) endpoints.
//
// These tests FAIL until the export/import handlers are implemented.
// They define the expected API surface and JSON contract.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/marketing"
	"github.com/jenslaufer/launch-kit/tenant"
)

// ---------------------------------------------------------------------------
// Export/import data types (define the expected API contract)
// ---------------------------------------------------------------------------

// TenantExport is the JSON structure for tenant export and import.
type TenantExport struct {
	Slug      string          `json:"slug"`
	Name      string          `json:"name"`
	Admin     *AdminExport    `json:"admin,omitempty"`
	Clients   []ClientExport  `json:"clients,omitempty"`
	Contacts  []ContactExport `json:"contacts,omitempty"`
	Segments  []SegmentExport `json:"segments,omitempty"`
	Campaigns []CampaignExport `json:"campaigns,omitempty"`
}

type AdminExport struct {
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
}

type ClientExport struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	// Secret is NOT exported; a new one is generated on import.
}

type ContactExport struct {
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	Segments      []string `json:"segments,omitempty"`
	ConsentSource string   `json:"consent_source"`
}

type SegmentExport struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CampaignExport struct {
	Subject   string   `json:"subject"`
	HTMLBody  string   `json:"html_body"`
	FromName  string   `json:"from_name"`
	FromEmail string   `json:"from_email"`
	Segments  []string `json:"segments,omitempty"`
}

// ImportResponse is returned by POST /admin/tenants/import.
type ImportResponse struct {
	TenantID string          `json:"tenant_id"`
	Slug     string          `json:"slug"`
	Clients  []ClientImported `json:"clients,omitempty"`
}

type ClientImported struct {
	Name         string   `json:"name"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
}

// ---------------------------------------------------------------------------
// Test environment for export/import
// ---------------------------------------------------------------------------

// exportEnv wires a server with both tenant management and export/import routes.
type exportEnv struct {
	srv         *httptest.Server
	tenantStore *tenant.Store
	iamStore    *iam.Store
	mktStore    *marketing.Store
	registry    *iam.TokenRegistry
}

func newExportEnv(t *testing.T) *exportEnv {
	t.Helper()
	db := newRoutingDB(t) // reuse the full-schema helper from tenant_routing_test.go

	tenantStore := tenant.NewStore(db)
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)

	registry := iam.NewTokenRegistry(iamStore, "http://test-issuer")

	iamHandler := iam.NewHandler(iamStore, registry, "http://test-issuer")
	mktHandler := marketing.NewHandler(mktStore, iamStore, registry)
	_ = mktHandler

	// Import/export handler — to be implemented in the tenant package.
	exportHandler := tenant.NewExportImportHandler(tenantStore, iamStore, mktStore, registry, "")

	mux := http.NewServeMux()
	mux.HandleFunc("/login", iamHandler.Login)

	// Tenant management routes
	mux.HandleFunc("/admin/tenants/import", exportHandler.Import)
	mux.HandleFunc("/admin/tenants/", exportHandler.ExportOrDelete)

	handler := tenant.Middleware(tenantStore, "")(mux)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &exportEnv{
		srv:         srv,
		tenantStore: tenantStore,
		iamStore:    iamStore,
		mktStore:    mktStore,
		registry:    registry,
	}
}

// adminToken returns a Bearer token for an admin user in the given tenant.
func adminToken(t *testing.T, env *exportEnv, tenantID, email, password string) string {
	t.Helper()
	scopedIAM := env.iamStore.ForTenant(tenantID)
	if err := scopedIAM.SeedAdmin(email, password, "Admin"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}

	// Load (or create) RSA key so the token service works.
	if _, err := scopedIAM.LoadOrCreateRSAKey(); err != nil {
		t.Fatalf("LoadOrCreateRSAKey: %v", err)
	}

	body := map[string]string{"email": email, "password": password}
	b, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, env.srv.URL+"/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	// Route to the correct tenant via path prefix.
	// For tests without a slug we set tenantID via header; but the admin
	// token check uses the token's "tid" claim vs the request tenant ID,
	// so we log in via path prefix when a slug exists.
	// Here we resolve the slug from the store directly.
	tn, err := env.tenantStore.GetByID(tenantID)
	if err != nil {
		t.Fatalf("GetByID(%s): %v", tenantID, err)
	}
	req2, _ := http.NewRequest(http.MethodPost,
		env.srv.URL+"/t/"+tn.Slug+"/login",
		bytes.NewReader(b))
	req2.Header.Set("Content-Type", "application/json")
	_ = req

	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed %d: %s", resp.StatusCode, raw)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	if tok.AccessToken == "" {
		t.Fatal("empty access_token from login")
	}
	return tok.AccessToken
}

// doExport calls GET /admin/tenants/{id}/export with an admin Bearer token.
func doExport(t *testing.T, env *exportEnv, tenantID, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet,
		env.srv.URL+"/admin/tenants/"+tenantID+"/export", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("export request: %v", err)
	}
	return resp
}

// doImport calls POST /admin/tenants/import with an admin Bearer token.
func doImport(t *testing.T, env *exportEnv, payload TenantExport, token string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost,
		env.srv.URL+"/admin/tenants/import",
		bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("import request: %v", err)
	}
	return resp
}

// seedFullTenant creates a tenant with an admin, one client, one segment,
// one contact (assigned to the segment), and one draft campaign.
// Returns the tenant and its admin token.
func seedFullTenant(t *testing.T, env *exportEnv, slug string) (*tenant.Tenant, string) {
	t.Helper()
	tn, err := env.tenantStore.Create(slug, slug+" App")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	iamScoped := env.iamStore.ForTenant(tn.ID)
	mktScoped := env.mktStore.ForTenant(tn.ID)

	// Admin
	const adminEmail = "admin@seed.com"
	const adminPass = "seedpass1"
	if err := iamScoped.SeedAdmin(adminEmail, adminPass, "Admin"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}

	// Client
	_, _, err = iamScoped.CreateClient("My SPA", []string{"https://app.example.com/callback"})
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}

	// Segment
	seg, err := mktScoped.CreateSegment("newsletter", "Main newsletter list")
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}

	// Contact assigned to segment
	contact, err := mktScoped.CreateContact("alice@example.com", "Alice", "api")
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if err := mktScoped.AddContactToSegment(contact.ID, seg.ID); err != nil {
		t.Fatalf("AddContactToSegment: %v", err)
	}

	// Draft campaign targeting the segment
	_, err = mktScoped.CreateCampaign(
		"Welcome", "<h1>Hi</h1>", "App", "hi@app.example.com",
		[]string{seg.ID},
	)
	if err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}

	tok := adminToken(t, env, tn.ID, adminEmail, adminPass)
	return tn, tok
}

// ---------------------------------------------------------------------------
// Export tests
// ---------------------------------------------------------------------------

func TestExport_RequiresAdminAuth(t *testing.T) {
	env := newExportEnv(t)
	tn, _ := seedFullTenant(t, env, "auth-check")

	req, _ := http.NewRequest(http.MethodGet,
		env.srv.URL+"/admin/tenants/"+tn.ID+"/export", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}
}

func TestExport_Returns404ForUnknownTenant(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "404-tenant")
	_ = tn

	// We need a token that belongs to some tenant; reuse the seeded one but
	// request an unknown tenant ID.
	req, _ := http.NewRequest(http.MethodGet,
		env.srv.URL+"/admin/tenants/nonexistent-id/export", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown tenant, got %d", resp.StatusCode)
	}
}

func TestExport_SlugAndName(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "slug-name-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var export TenantExport
	if err := json.NewDecoder(resp.Body).Decode(&export); err != nil {
		t.Fatalf("decode export: %v", err)
	}

	if export.Slug != tn.Slug {
		t.Errorf("slug = %q, want %q", export.Slug, tn.Slug)
	}
	if export.Name != tn.Name {
		t.Errorf("name = %q, want %q", export.Name, tn.Name)
	}
}

func TestExport_IncludesAdminEmail(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "admin-email-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	var export TenantExport
	json.NewDecoder(resp.Body).Decode(&export)

	if export.Admin == nil {
		t.Fatal("export.admin is nil; expected admin section with email")
	}
	if export.Admin.Email != "admin@seed.com" {
		t.Errorf("admin.email = %q, want %q", export.Admin.Email, "admin@seed.com")
	}
	// Password hash must NOT be exported.
	if export.Admin.Password != "" {
		t.Error("admin.password must not be included in export")
	}
}

func TestExport_IncludesClients(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "clients-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	var export TenantExport
	json.NewDecoder(resp.Body).Decode(&export)

	if len(export.Clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(export.Clients))
	}
	c := export.Clients[0]
	if c.Name != "My SPA" {
		t.Errorf("client.name = %q, want %q", c.Name, "My SPA")
	}
	if len(c.RedirectURIs) != 1 || c.RedirectURIs[0] != "https://app.example.com/callback" {
		t.Errorf("client.redirect_uris = %v, want [https://app.example.com/callback]", c.RedirectURIs)
	}
}

func TestExport_ClientsHaveNoSecrets(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "no-secrets-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	// Decode as raw map to catch any unexpected secret field.
	var raw struct {
		Clients []map[string]interface{} `json:"clients"`
	}
	rawBody, _ := io.ReadAll(resp.Body)
	json.Unmarshal(rawBody, &raw)

	for _, c := range raw.Clients {
		if _, ok := c["secret"]; ok {
			t.Error("client export contains 'secret' field — must not export secrets")
		}
		if _, ok := c["client_secret"]; ok {
			t.Error("client export contains 'client_secret' field — must not export secrets")
		}
		if _, ok := c["secret_hash"]; ok {
			t.Error("client export contains 'secret_hash' field — must not export secrets")
		}
	}
}

func TestExport_IncludesSegments(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "segments-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	var export TenantExport
	json.NewDecoder(resp.Body).Decode(&export)

	if len(export.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(export.Segments))
	}
	s := export.Segments[0]
	if s.Name != "newsletter" {
		t.Errorf("segment.name = %q, want %q", s.Name, "newsletter")
	}
	if s.Description != "Main newsletter list" {
		t.Errorf("segment.description = %q, want %q", s.Description, "Main newsletter list")
	}
}

func TestExport_IncludesContactsWithSegmentNames(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "contacts-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	var export TenantExport
	json.NewDecoder(resp.Body).Decode(&export)

	if len(export.Contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(export.Contacts))
	}
	c := export.Contacts[0]
	if c.Email != "alice@example.com" {
		t.Errorf("contact.email = %q, want %q", c.Email, "alice@example.com")
	}
	if c.Name != "Alice" {
		t.Errorf("contact.name = %q, want %q", c.Name, "Alice")
	}
	if c.ConsentSource != "api" {
		t.Errorf("contact.consent_source = %q, want %q", c.ConsentSource, "api")
	}
	if len(c.Segments) != 1 || c.Segments[0] != "newsletter" {
		t.Errorf("contact.segments = %v, want [newsletter]", c.Segments)
	}
}

func TestExport_ContactsHaveNoTokens(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "no-tokens-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	var raw struct {
		Contacts []map[string]interface{} `json:"contacts"`
	}
	rawBody, _ := io.ReadAll(resp.Body)
	json.Unmarshal(rawBody, &raw)

	for _, c := range raw.Contacts {
		if _, ok := c["unsubscribe_token"]; ok {
			t.Error("contact export contains 'unsubscribe_token' — must not be exported")
		}
		if _, ok := c["invite_token"]; ok {
			t.Error("contact export contains 'invite_token' — must not be exported")
		}
	}
}

func TestExport_IncludesDraftCampaigns(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "campaigns-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	var export TenantExport
	json.NewDecoder(resp.Body).Decode(&export)

	if len(export.Campaigns) != 1 {
		t.Fatalf("expected 1 campaign, got %d", len(export.Campaigns))
	}
	camp := export.Campaigns[0]
	if camp.Subject != "Welcome" {
		t.Errorf("campaign.subject = %q, want %q", camp.Subject, "Welcome")
	}
	if camp.HTMLBody != "<h1>Hi</h1>" {
		t.Errorf("campaign.html_body = %q, want %q", camp.HTMLBody, "<h1>Hi</h1>")
	}
	if camp.FromName != "App" {
		t.Errorf("campaign.from_name = %q, want %q", camp.FromName, "App")
	}
	if camp.FromEmail != "hi@app.example.com" {
		t.Errorf("campaign.from_email = %q, want %q", camp.FromEmail, "hi@app.example.com")
	}
	if len(camp.Segments) != 1 || camp.Segments[0] != "newsletter" {
		t.Errorf("campaign.segments = %v, want [newsletter]", camp.Segments)
	}
}

func TestExport_CampaignsHaveNoRecipients(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "no-recipients-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	var raw struct {
		Campaigns []map[string]interface{} `json:"campaigns"`
	}
	rawBody, _ := io.ReadAll(resp.Body)
	json.Unmarshal(rawBody, &raw)

	for _, c := range raw.Campaigns {
		if _, ok := c["recipients"]; ok {
			t.Error("campaign export contains 'recipients' — must not be exported")
		}
		if _, ok := c["sent_at"]; ok && c["sent_at"] != nil {
			t.Error("campaign export contains 'sent_at' — draft campaigns have no sent_at")
		}
	}
}

func TestExport_DoesNotExportRSAKeys(t *testing.T) {
	env := newExportEnv(t)
	tn, tok := seedFullTenant(t, env, "no-keys-export")

	resp := doExport(t, env, tn.ID, tok)
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	body := string(rawBody)

	if strings.Contains(body, "PRIVATE KEY") {
		t.Error("export contains RSA private key material — must not be exported")
	}
	if strings.Contains(body, "private_key") {
		t.Error("export contains 'private_key' field — must not be exported")
	}
}

// ---------------------------------------------------------------------------
// Import tests
// ---------------------------------------------------------------------------

func TestImport_RequiresAdminAuth(t *testing.T) {
	env := newExportEnv(t)

	payload := TenantExport{Slug: "import-noauth", Name: "Import No Auth"}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(env.srv.URL+"/admin/tenants/import", "application/json",
		bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}
}

// seedGlobalAdmin creates a "platform" tenant whose admin can call
// /admin/tenants/import (no tenant scope needed — treated as global admin).
func seedGlobalAdmin(t *testing.T, env *exportEnv) string {
	t.Helper()
	tn, err := env.tenantStore.Create("platform", "Platform")
	if err != nil {
		// Already exists if multiple tests share the env.
		tn, err = env.tenantStore.GetBySlug("platform")
		if err != nil {
			t.Fatalf("get platform tenant: %v", err)
		}
	}
	return adminToken(t, env, tn.ID, "superadmin@platform.com", "platformpass1")
}

func TestImport_MinimalSlugAndName(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{Slug: "minimal-import", Name: "Minimal Import"}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	var result ImportResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Slug != "minimal-import" {
		t.Errorf("result.slug = %q, want %q", result.Slug, "minimal-import")
	}
	if result.TenantID == "" {
		t.Error("result.tenant_id is empty")
	}

	// Verify tenant exists in store.
	tn, err := env.tenantStore.GetBySlug("minimal-import")
	if err != nil {
		t.Fatalf("tenant not created in store: %v", err)
	}
	if tn.Name != "Minimal Import" {
		t.Errorf("tenant.name = %q, want %q", tn.Name, "Minimal Import")
	}
}

func TestImport_Returns400ForMissingSlug(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{Name: "No Slug"}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing slug, got %d", resp.StatusCode)
	}
}

func TestImport_Returns409ForDuplicateSlug(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{Slug: "dupe-slug", Name: "First"}
	resp1 := doImport(t, env, payload, tok)
	resp1.Body.Close()

	resp2 := doImport(t, env, payload, tok)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for duplicate slug, got %d", resp2.StatusCode)
	}
}

func TestImport_CreatesAdminUser(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{
		Slug: "admin-import",
		Name: "Admin Import",
		Admin: &AdminExport{
			Email:    "newadmin@example.com",
			Password: "importpass1",
		},
	}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	var result ImportResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// Verify the admin can log in.
	iamScoped := env.iamStore.ForTenant(result.TenantID)
	user, err := iamScoped.AuthenticateUser("newadmin@example.com", "importpass1")
	if err != nil {
		t.Fatalf("admin cannot authenticate after import: %v", err)
	}
	if user.Role != "admin" {
		t.Errorf("imported user role = %q, want admin", user.Role)
	}
}

func TestImport_CreatesClients(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{
		Slug: "clients-import",
		Name: "Clients Import",
		Clients: []ClientExport{
			{Name: "My SPA", RedirectURIs: []string{"https://app.com/callback"}},
		},
	}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	var result ImportResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Clients) != 1 {
		t.Fatalf("expected 1 client in response, got %d", len(result.Clients))
	}
	c := result.Clients[0]
	if c.Name != "My SPA" {
		t.Errorf("client.name = %q, want %q", c.Name, "My SPA")
	}
	if c.ClientID == "" {
		t.Error("client.client_id is empty")
	}
	// New secret must be generated and returned.
	if c.ClientSecret == "" {
		t.Error("client.client_secret is empty — new secret must be generated on import")
	}
	if len(c.RedirectURIs) != 1 || c.RedirectURIs[0] != "https://app.com/callback" {
		t.Errorf("client.redirect_uris = %v, want [https://app.com/callback]", c.RedirectURIs)
	}
}

func TestImport_CreatesSegments(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{
		Slug: "segments-import",
		Name: "Segments Import",
		Segments: []SegmentExport{
			{Name: "newsletter", Description: "Main list"},
		},
	}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	var result ImportResponse
	json.NewDecoder(resp.Body).Decode(&result)

	mktScoped := env.mktStore.ForTenant(result.TenantID)
	segs, err := mktScoped.ListSegments()
	if err != nil {
		t.Fatalf("ListSegments: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Name != "newsletter" {
		t.Errorf("segment.name = %q, want newsletter", segs[0].Name)
	}
	if segs[0].Description != "Main list" {
		t.Errorf("segment.description = %q, want 'Main list'", segs[0].Description)
	}
}

func TestImport_CreatesContactsWithSegments(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{
		Slug: "contacts-import",
		Name: "Contacts Import",
		Segments: []SegmentExport{
			{Name: "newsletter", Description: ""},
		},
		Contacts: []ContactExport{
			{
				Email:         "bob@example.com",
				Name:          "Bob",
				Segments:      []string{"newsletter"},
				ConsentSource: "api",
			},
		},
	}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	var result ImportResponse
	json.NewDecoder(resp.Body).Decode(&result)

	mktScoped := env.mktStore.ForTenant(result.TenantID)
	contact, err := mktScoped.GetContactByEmail("bob@example.com")
	if err != nil {
		t.Fatalf("contact not found after import: %v", err)
	}
	if contact.Name != "Bob" {
		t.Errorf("contact.name = %q, want Bob", contact.Name)
	}
	if contact.ConsentSource != "api" {
		t.Errorf("contact.consent_source = %q, want api", contact.ConsentSource)
	}

	// New tokens must be generated (not blank).
	if contact.UnsubscribeToken == "" {
		t.Error("contact.unsubscribe_token is empty after import")
	}

	// Segment assignment.
	segs, err := mktScoped.GetContactSegments(contact.ID)
	if err != nil {
		t.Fatalf("GetContactSegments: %v", err)
	}
	if len(segs) != 1 || segs[0].Name != "newsletter" {
		t.Errorf("contact segments = %v, want [newsletter]", segs)
	}
}

func TestImport_CreatesCampaignsWithSegments(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{
		Slug: "campaigns-import",
		Name: "Campaigns Import",
		Segments: []SegmentExport{
			{Name: "newsletter", Description: ""},
		},
		Campaigns: []CampaignExport{
			{
				Subject:   "Welcome",
				HTMLBody:  "<h1>Hello</h1>",
				FromName:  "App",
				FromEmail: "hi@app.com",
				Segments:  []string{"newsletter"},
			},
		},
	}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	var result ImportResponse
	json.NewDecoder(resp.Body).Decode(&result)

	mktScoped := env.mktStore.ForTenant(result.TenantID)
	campaigns, err := mktScoped.ListCampaigns()
	if err != nil {
		t.Fatalf("ListCampaigns: %v", err)
	}
	if len(campaigns) != 1 {
		t.Fatalf("expected 1 campaign, got %d", len(campaigns))
	}
	camp := campaigns[0]
	if camp.Subject != "Welcome" {
		t.Errorf("campaign.subject = %q, want Welcome", camp.Subject)
	}
	if camp.Status != "draft" {
		t.Errorf("campaign.status = %q, want draft", camp.Status)
	}

	// Verify segment assignment via GetCampaignByID.
	full, err := mktScoped.GetCampaignByID(camp.ID)
	if err != nil {
		t.Fatalf("GetCampaignByID: %v", err)
	}
	if len(full.SegmentIDs) != 1 {
		t.Errorf("campaign has %d segments, want 1", len(full.SegmentIDs))
	}
}

func TestImport_MultipleClients(t *testing.T) {
	env := newExportEnv(t)
	tok := seedGlobalAdmin(t, env)

	payload := TenantExport{
		Slug: "multi-clients-import",
		Name: "Multi Clients",
		Clients: []ClientExport{
			{Name: "SPA", RedirectURIs: []string{"https://spa.example.com/cb"}},
			{Name: "Mobile", RedirectURIs: []string{"myapp://callback"}},
		},
	}
	resp := doImport(t, env, payload, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	var result ImportResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Clients) != 2 {
		t.Fatalf("expected 2 clients in response, got %d", len(result.Clients))
	}

	// Each client must have a unique generated secret.
	secrets := map[string]bool{}
	for _, c := range result.Clients {
		if c.ClientSecret == "" {
			t.Errorf("client %q has empty client_secret", c.Name)
		}
		secrets[c.ClientSecret] = true
	}
	if len(secrets) != 2 {
		t.Error("clients share the same secret — each must get a unique secret")
	}
}

// ---------------------------------------------------------------------------
// Startup / programmatic import test
// ---------------------------------------------------------------------------

// TestImportTenantConfig tests the programmatic import function directly,
// without going through the HTTP layer. This covers the startup-import use case
// where a TENANT_CONFIG env var seeds a tenant on first boot.
func TestImportTenantConfig_Idempotent(t *testing.T) {
	db := newRoutingDB(t)
	tenantStore := tenant.NewStore(db)
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)

	cfg := tenant.ImportConfig{
		Slug: "startup-tenant",
		Name: "Startup App",
		Admin: &tenant.AdminConfig{
			Email:    "admin@startup.com",
			Password: "startpass1",
		},
		Clients: []tenant.ClientConfig{
			{Name: "SPA", RedirectURIs: []string{"https://startup.com/cb"}},
		},
		Segments: []tenant.SegmentConfig{
			{Name: "beta", Description: "Beta users"},
		},
		Contacts: []tenant.ContactConfig{
			{Email: "beta@startup.com", Name: "Beta User", Segments: []string{"beta"}, ConsentSource: "api"},
		},
	}

	// First call: imports the tenant.
	result1, err := tenant.ImportTenantConfig(tenantStore, iamStore, mktStore, cfg)
	if err != nil {
		t.Fatalf("first ImportTenantConfig: %v", err)
	}
	if result1.TenantID == "" {
		t.Error("result1.TenantID is empty")
	}

	// Second call: must be idempotent — skips import when tenant already exists.
	result2, err := tenant.ImportTenantConfig(tenantStore, iamStore, mktStore, cfg)
	if err != nil {
		t.Fatalf("second ImportTenantConfig (idempotent): %v", err)
	}
	if result2.TenantID != result1.TenantID {
		t.Errorf("second call returned different tenant ID: %q vs %q",
			result2.TenantID, result1.TenantID)
	}
	if !result2.Skipped {
		t.Error("result2.Skipped should be true when tenant already exists")
	}
}

func TestImportTenantConfig_SeedsSegmentsAndContacts(t *testing.T) {
	db := newRoutingDB(t)
	tenantStore := tenant.NewStore(db)
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)

	cfg := tenant.ImportConfig{
		Slug: "seeded-tenant",
		Name: "Seeded App",
		Segments: []tenant.SegmentConfig{
			{Name: "users", Description: "All users"},
		},
		Contacts: []tenant.ContactConfig{
			{Email: "user@seeded.com", Name: "User", Segments: []string{"users"}, ConsentSource: "import"},
		},
	}

	result, err := tenant.ImportTenantConfig(tenantStore, iamStore, mktStore, cfg)
	if err != nil {
		t.Fatalf("ImportTenantConfig: %v", err)
	}

	mktScoped := mktStore.ForTenant(result.TenantID)
	contact, err := mktScoped.GetContactByEmail("user@seeded.com")
	if err != nil {
		t.Fatalf("contact not found: %v", err)
	}

	segs, err := mktScoped.GetContactSegments(contact.ID)
	if err != nil {
		t.Fatalf("GetContactSegments: %v", err)
	}
	if len(segs) != 1 || segs[0].Name != "users" {
		t.Errorf("contact segments = %v, want [users]", segs)
	}
}
