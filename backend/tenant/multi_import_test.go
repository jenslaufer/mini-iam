package tenant_test

// multi_import_test.go tests multi-tenant import (Issue #14).
//
// TENANT_CONFIG must accept both a single JSON object and a JSON array.
// These tests define the expected contract for ParseTenantConfigs and
// multi-tenant ImportTenantConfig flows.
//
// Tests are in RED phase: they compile but FAIL until production code is written.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/marketing"
	"github.com/jenslaufer/launch-kit/tenant"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// multiImportEnv reuses the same wiring as exportEnv from export_import_test.go.
func newMultiImportEnv(t *testing.T) *exportEnv {
	t.Helper()
	return newExportEnv(t)
}

// importDirect calls ImportTenantConfig programmatically and returns the result.
func importDirect(t *testing.T, env *exportEnv, cfg tenant.ImportConfig) (*tenant.ImportResult, error) {
	t.Helper()
	return tenant.ImportTenantConfig(env.tenantStore, env.iamStore, env.mktStore, cfg)
}

// minimalConfig returns a valid ImportConfig with the given slug.
func minimalConfig(slug string) tenant.ImportConfig {
	return tenant.ImportConfig{
		Slug: slug,
		Name: slug + " App",
		Admin: &tenant.AdminConfig{
			Email:    slug + "-admin@test.com",
			Password: "testpass1",
		},
	}
}

// fullConfig returns an ImportConfig with all fields populated.
func fullConfig(slug string) tenant.ImportConfig {
	return tenant.ImportConfig{
		Slug:                slug,
		Name:                slug + " Full App",
		RegistrationEnabled: true,
		SMTP: &tenant.SMTPConfig{
			Host:     "smtp." + slug + ".test",
			Port:     "587",
			User:     slug + "@mail.test",
			Password: "smtppass",
			From:     slug + "@mail.test",
			FromName: slug + " Mailer",
			RateMS:   100,
		},
		Admin: &tenant.AdminConfig{
			Email:    slug + "-admin@test.com",
			Password: "adminpass1",
		},
		Users: []tenant.UserConfig{
			{Email: slug + "-user@test.com", Password: "userpass1", Name: slug + " User", Role: "member"},
		},
		Clients: []tenant.ClientConfig{
			{Name: slug + " SPA", RedirectURIs: []string{"https://" + slug + ".test/callback"}},
		},
	}
}

// doMultiBatchImport sends a POST to /admin/tenants/import-batch with ImportConfig configs.
func doMultiBatchImport(t *testing.T, env *exportEnv, configs []tenant.ImportConfig, token string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(configs)
	req, _ := http.NewRequest(http.MethodPost,
		env.srv.URL+"/admin/tenants/import-batch",
		bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("batch import request: %v", err)
	}
	return resp
}

// doMultiRawImport sends a raw JSON body to /admin/tenants/import.
func doMultiRawImport(t *testing.T, env *exportEnv, rawJSON string, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost,
		env.srv.URL+"/admin/tenants/import",
		strings.NewReader(rawJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("raw import request: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// ParseTenantConfigs tests (function expected in tenant package)
// ---------------------------------------------------------------------------

// TestParseSingleTenantJSON verifies a single JSON object is accepted.
func TestParseSingleTenantJSON(t *testing.T) {
	data := []byte(`{"slug":"demo","name":"Demo App"}`)
	configs, err := tenant.ParseTenantConfigs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Slug != "demo" {
		t.Errorf("slug = %q, want %q", configs[0].Slug, "demo")
	}
	if configs[0].Name != "Demo App" {
		t.Errorf("name = %q, want %q", configs[0].Name, "Demo App")
	}
}

// TestParseSingleTenantFields verifies all fields survive round-trip through ParseTenantConfigs.
func TestParseSingleTenantFields(t *testing.T) {
	data := []byte(`{
		"slug": "full",
		"name": "Full App",
		"registration_enabled": true,
		"smtp": {
			"smtp_host": "smtp.full.test",
			"smtp_port": "587",
			"smtp_user": "user@full.test",
			"smtp_password": "secret",
			"smtp_from": "noreply@full.test",
			"smtp_from_name": "Full Mailer",
			"smtp_rate_ms": 200
		},
		"admin": {"email": "admin@full.test", "password": "adminpass"},
		"users": [{"email": "u@full.test", "password": "upass", "name": "U", "role": "member"}],
		"clients": [{"name": "App", "redirect_uris": ["https://app.test/cb"]}]
	}`)
	configs, err := tenant.ParseTenantConfigs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	c := configs[0]
	if c.Slug != "full" {
		t.Errorf("slug = %q", c.Slug)
	}
	if !c.RegistrationEnabled {
		t.Error("registration_enabled should be true")
	}
	if c.SMTP == nil {
		t.Fatal("smtp is nil")
	}
	if c.SMTP.Host != "smtp.full.test" {
		t.Errorf("smtp.host = %q", c.SMTP.Host)
	}
	if c.SMTP.Port != "587" {
		t.Errorf("smtp.port = %q", c.SMTP.Port)
	}
	if c.SMTP.RateMS != 200 {
		t.Errorf("smtp.rate_ms = %d", c.SMTP.RateMS)
	}
	if c.Admin == nil || c.Admin.Email != "admin@full.test" {
		t.Errorf("admin = %+v", c.Admin)
	}
	if len(c.Users) != 1 || c.Users[0].Email != "u@full.test" {
		t.Errorf("users = %+v", c.Users)
	}
	if len(c.Clients) != 1 || c.Clients[0].Name != "App" {
		t.Errorf("clients = %+v", c.Clients)
	}
}

// TestParseMultipleTenants verifies a JSON array of tenant configs is accepted.
func TestParseMultipleTenants(t *testing.T) {
	data := []byte(`[
		{"slug":"alpha","name":"Alpha"},
		{"slug":"beta","name":"Beta"},
		{"slug":"gamma","name":"Gamma"}
	]`)
	configs, err := tenant.ParseTenantConfigs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 3 {
		t.Fatalf("expected 3 configs, got %d", len(configs))
	}
	slugs := []string{configs[0].Slug, configs[1].Slug, configs[2].Slug}
	want := []string{"alpha", "beta", "gamma"}
	for i, s := range slugs {
		if s != want[i] {
			t.Errorf("configs[%d].slug = %q, want %q", i, s, want[i])
		}
	}
}

// TestParseEmptyArray verifies empty array produces zero configs without error.
func TestParseEmptyArray(t *testing.T) {
	configs, err := tenant.ParseTenantConfigs([]byte(`[]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(configs))
	}
}

// TestParseSingleElementArray verifies array with one element works.
func TestParseSingleElementArray(t *testing.T) {
	configs, err := tenant.ParseTenantConfigs([]byte(`[{"slug":"solo","name":"Solo"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Slug != "solo" {
		t.Errorf("slug = %q, want %q", configs[0].Slug, "solo")
	}
}

// TestParseInvalidJSON verifies malformed JSON produces a clear error.
func TestParseInvalidJSON(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"broken object", `{invalid`},
		{"broken array", `[{"slug":"a"},`},
		{"bare string", `"just a string"`},
		{"bare number", `42`},
		{"missing closing brace", `{"slug":"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tenant.ParseTenantConfigs([]byte(tc.data))
			if err == nil {
				t.Error("expected error for invalid JSON")
			}
		})
	}
}

// TestParseEmptyString verifies empty input is handled gracefully.
func TestParseEmptyString(t *testing.T) {
	configs, err := tenant.ParseTenantConfigs([]byte(""))
	if err != nil {
		t.Fatalf("empty input should not error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(configs))
	}
}

// TestParseWhitespace verifies whitespace-only input is handled gracefully.
func TestParseWhitespace(t *testing.T) {
	configs, err := tenant.ParseTenantConfigs([]byte("   \n\t  "))
	if err != nil {
		t.Fatalf("whitespace-only input should not error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(configs))
	}
}

// ---------------------------------------------------------------------------
// Programmatic multi-tenant import tests
// ---------------------------------------------------------------------------

// TestImportSingleTenantJSON verifies single-object import still works (backward compat).
func TestImportSingleTenantJSON(t *testing.T) {
	env := newMultiImportEnv(t)
	cfg := minimalConfig("single")
	result, err := importDirect(t, env, cfg)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.TenantID == "" {
		t.Error("tenant_id is empty")
	}
	if result.Skipped {
		t.Error("should not be skipped on first import")
	}

	// Verify tenant exists in store.
	tn, err := env.tenantStore.GetBySlug("single")
	if err != nil {
		t.Fatalf("tenant not found: %v", err)
	}
	if tn.Name != "single App" {
		t.Errorf("name = %q, want %q", tn.Name, "single App")
	}
}

// TestImportSingleTenantFields verifies all fields are populated after import.
func TestImportSingleTenantFields(t *testing.T) {
	env := newMultiImportEnv(t)
	cfg := fullConfig("fields")
	result, err := importDirect(t, env, cfg)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	tn, err := env.tenantStore.GetBySlug("fields")
	if err != nil {
		t.Fatalf("tenant not found: %v", err)
	}
	if tn.Name != "fields Full App" {
		t.Errorf("name = %q", tn.Name)
	}
	if !tn.RegistrationEnabled {
		t.Error("registration_enabled should be true")
	}
	if tn.SMTP.Host != "smtp.fields.test" {
		t.Errorf("smtp.host = %q", tn.SMTP.Host)
	}
	if tn.SMTP.Port != "587" {
		t.Errorf("smtp.port = %q", tn.SMTP.Port)
	}
	if tn.SMTP.RateMS != 100 {
		t.Errorf("smtp.rate_ms = %d", tn.SMTP.RateMS)
	}

	// Verify admin user.
	scopedIAM := env.iamStore.ForTenant(result.TenantID)
	admin, err := scopedIAM.GetUserByEmail("fields-admin@test.com")
	if err != nil {
		t.Fatalf("admin not found: %v", err)
	}
	if admin.Role != "admin" {
		t.Errorf("admin role = %q", admin.Role)
	}

	// Verify regular user.
	user, err := scopedIAM.GetUserByEmail("fields-user@test.com")
	if err != nil {
		t.Fatalf("user not found: %v", err)
	}
	if user.Name != "fields User" {
		t.Errorf("user name = %q", user.Name)
	}

	// Verify client.
	if len(result.Clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(result.Clients))
	}
	if result.Clients[0].Name != "fields SPA" {
		t.Errorf("client name = %q", result.Clients[0].Name)
	}
}

// TestImportSingleTenantIdempotent verifies importing the same tenant twice doesn't duplicate.
func TestImportSingleTenantIdempotent(t *testing.T) {
	env := newMultiImportEnv(t)
	cfg := minimalConfig("idempotent")

	result1, err := importDirect(t, env, cfg)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if result1.Skipped {
		t.Error("first import should not be skipped")
	}

	result2, err := importDirect(t, env, cfg)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !result2.Skipped {
		t.Error("second import should be skipped")
	}
	if result2.TenantID != result1.TenantID {
		t.Errorf("tenant IDs differ: %q vs %q", result1.TenantID, result2.TenantID)
	}

	// Verify only one tenant with this slug.
	tenants, err := env.tenantStore.List()
	if err != nil {
		t.Fatalf("list tenants: %v", err)
	}
	count := 0
	for _, tn := range tenants {
		if tn.Slug == "idempotent" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 tenant with slug 'idempotent', got %d", count)
	}
}

// TestImportMultipleTenants verifies importing multiple tenants sequentially.
func TestImportMultipleTenants(t *testing.T) {
	env := newMultiImportEnv(t)
	slugs := []string{"multi-a", "multi-b", "multi-c"}

	ids := make(map[string]string)
	for _, slug := range slugs {
		cfg := minimalConfig(slug)
		result, err := importDirect(t, env, cfg)
		if err != nil {
			t.Fatalf("import %q: %v", slug, err)
		}
		if result.TenantID == "" {
			t.Errorf("import %q returned empty tenant_id", slug)
		}
		ids[slug] = result.TenantID
	}

	// All IDs should be unique.
	seen := map[string]bool{}
	for slug, id := range ids {
		if seen[id] {
			t.Errorf("duplicate tenant_id for slug %q", slug)
		}
		seen[id] = true
	}

	// All tenants should be retrievable.
	for _, slug := range slugs {
		tn, err := env.tenantStore.GetBySlug(slug)
		if err != nil {
			t.Errorf("tenant %q not found: %v", slug, err)
		}
		if tn.Name != slug+" App" {
			t.Errorf("tenant %q name = %q", slug, tn.Name)
		}
	}
}

// TestImportMultipleTenantsAllFields verifies each tenant has correct independent fields.
func TestImportMultipleTenantsAllFields(t *testing.T) {
	env := newMultiImportEnv(t)

	cfg1 := fullConfig("mf-one")
	cfg2 := fullConfig("mf-two")
	cfg2.Name = "Two Special"
	cfg2.RegistrationEnabled = false

	r1, err := importDirect(t, env, cfg1)
	if err != nil {
		t.Fatalf("import mf-one: %v", err)
	}
	r2, err := importDirect(t, env, cfg2)
	if err != nil {
		t.Fatalf("import mf-two: %v", err)
	}

	tn1, _ := env.tenantStore.GetBySlug("mf-one")
	tn2, _ := env.tenantStore.GetBySlug("mf-two")

	if tn1.Name != "mf-one Full App" {
		t.Errorf("tn1 name = %q", tn1.Name)
	}
	if tn2.Name != "Two Special" {
		t.Errorf("tn2 name = %q", tn2.Name)
	}
	if !tn1.RegistrationEnabled {
		t.Error("tn1 registration should be enabled")
	}
	if tn2.RegistrationEnabled {
		t.Error("tn2 registration should be disabled")
	}
	if tn1.SMTP.Host != "smtp.mf-one.test" {
		t.Errorf("tn1 smtp.host = %q", tn1.SMTP.Host)
	}
	if tn2.SMTP.Host != "smtp.mf-two.test" {
		t.Errorf("tn2 smtp.host = %q", tn2.SMTP.Host)
	}

	// Users are tenant-isolated.
	scoped1 := env.iamStore.ForTenant(r1.TenantID)
	scoped2 := env.iamStore.ForTenant(r2.TenantID)
	_, err = scoped1.GetUserByEmail("mf-one-admin@test.com")
	if err != nil {
		t.Errorf("mf-one admin not found: %v", err)
	}
	_, err = scoped2.GetUserByEmail("mf-two-admin@test.com")
	if err != nil {
		t.Errorf("mf-two admin not found: %v", err)
	}
	// Cross-tenant isolation: mf-one admin should NOT exist in mf-two's scope.
	_, err = scoped2.GetUserByEmail("mf-one-admin@test.com")
	if err == nil {
		t.Error("mf-one admin should not be visible in mf-two scope")
	}
}

// TestImportLargeArray verifies 50 tenants can be imported successfully.
func TestImportLargeArray(t *testing.T) {
	env := newMultiImportEnv(t)

	for i := 0; i < 50; i++ {
		slug := fmt.Sprintf("bulk-%03d", i)
		cfg := tenant.ImportConfig{
			Slug: slug,
			Name: fmt.Sprintf("Bulk %d", i),
			Admin: &tenant.AdminConfig{
				Email:    fmt.Sprintf("admin-%d@bulk.test", i),
				Password: "bulkpass1",
			},
		}
		result, err := importDirect(t, env, cfg)
		if err != nil {
			t.Fatalf("import %q failed: %v", slug, err)
		}
		if result.TenantID == "" {
			t.Fatalf("import %q returned empty tenant_id", slug)
		}
	}

	tenants, err := env.tenantStore.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	count := 0
	for _, tn := range tenants {
		if strings.HasPrefix(tn.Slug, "bulk-") {
			count++
		}
	}
	if count != 50 {
		t.Errorf("expected 50 bulk tenants, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Validation tests
// ---------------------------------------------------------------------------

// TestImportDuplicateSlugsInArray verifies that duplicate slugs in a batch are handled.
// The second tenant with the same slug should be skipped (idempotent) or error.
func TestImportDuplicateSlugsInArray(t *testing.T) {
	env := newMultiImportEnv(t)
	cfg := minimalConfig("dup-slug")

	r1, err := importDirect(t, env, cfg)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if r1.Skipped {
		t.Fatal("first import should not be skipped")
	}

	r2, err := importDirect(t, env, cfg)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !r2.Skipped {
		t.Error("second import of same slug should be skipped")
	}
}

// TestImportMissingRequiredFields verifies a tenant without slug produces an error.
func TestImportMissingRequiredFields(t *testing.T) {
	env := newMultiImportEnv(t)

	cfg := tenant.ImportConfig{
		Name: "No Slug App",
		Admin: &tenant.AdminConfig{
			Email:    "admin@noslug.test",
			Password: "pass1234",
		},
	}
	_, err := importDirect(t, env, cfg)
	if err == nil {
		t.Error("expected error for missing slug")
	}
}

// TestImportInvalidSlugFormat verifies invalid slug formats are rejected.
func TestImportInvalidSlugFormat(t *testing.T) {
	env := newMultiImportEnv(t)

	cases := []struct {
		name string
		slug string
	}{
		{"uppercase", "Demo"},
		{"spaces", "my tenant"},
		{"special chars", "my@tenant"},
		{"starts with dash", "-demo"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tenant.ImportConfig{
				Slug: tc.slug,
				Name: "Invalid",
				Admin: &tenant.AdminConfig{
					Email:    "admin@invalid.test",
					Password: "pass1234",
				},
			}
			_, err := importDirect(t, env, cfg)
			if err == nil {
				t.Errorf("expected error for slug %q", tc.slug)
			}
		})
	}
}

// TestImportPartialFailure verifies behavior when one tenant in a batch fails.
// Uses the batch HTTP endpoint to test partial failure semantics.
func TestImportPartialFailure(t *testing.T) {
	env := newMultiImportEnv(t)

	// Create platform tenant for admin auth.
	platform, err := env.tenantStore.Create("platform-pf", "Platform")
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	token := adminToken(t, env, platform.ID, "admin@platform-pf.test", "platformpass1")

	configs := []tenant.ImportConfig{
		minimalConfig("batch-ok-1"),
		{Slug: "INVALID-SLUG", Name: "Bad"}, // invalid slug
		minimalConfig("batch-ok-2"),
	}

	resp := doMultiBatchImport(t, env, configs, token)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch import status = %d, body = %s", resp.StatusCode, body)
	}

	var results []struct {
		Slug     string `json:"slug"`
		TenantID string `json:"tenant_id"`
		Error    string `json:"error"`
		Skipped  bool   `json:"skipped"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First should succeed.
	if results[0].TenantID == "" || results[0].Error != "" {
		t.Errorf("results[0] should succeed: %+v", results[0])
	}
	// Second should fail.
	if results[1].Error == "" {
		t.Errorf("results[1] should have error: %+v", results[1])
	}
	// Third should succeed (batch is not atomic).
	if results[2].TenantID == "" || results[2].Error != "" {
		t.Errorf("results[2] should succeed: %+v", results[2])
	}
}

// ---------------------------------------------------------------------------
// Edge case tests
// ---------------------------------------------------------------------------

// TestImportNestedJSON verifies tenant with complex SMTP config nested object.
func TestImportNestedJSON(t *testing.T) {
	env := newMultiImportEnv(t)
	cfg := tenant.ImportConfig{
		Slug: "nested",
		Name: "Nested App",
		SMTP: &tenant.SMTPConfig{
			Host:     "smtp.nested.test",
			Port:     "465",
			User:     "user@nested.test",
			Password: "s3cret!@#$%",
			From:     "noreply@nested.test",
			FromName: "Nested Mailer",
			RateMS:   500,
		},
		Admin: &tenant.AdminConfig{
			Email:    "admin@nested.test",
			Password: "nestedpass1",
		},
	}
	result, err := importDirect(t, env, cfg)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	tn, err := env.tenantStore.GetBySlug("nested")
	if err != nil {
		t.Fatalf("tenant not found: %v", err)
	}
	if tn.SMTP.Host != "smtp.nested.test" {
		t.Errorf("smtp.host = %q", tn.SMTP.Host)
	}
	if tn.SMTP.Password != "s3cret!@#$%" {
		t.Errorf("smtp.password not preserved")
	}
	if tn.SMTP.RateMS != 500 {
		t.Errorf("smtp.rate_ms = %d", tn.SMTP.RateMS)
	}
	_ = result
}

// ---------------------------------------------------------------------------
// Batch HTTP endpoint tests
// ---------------------------------------------------------------------------

// TestBatchImportMultipleTenants verifies the batch endpoint creates multiple tenants.
func TestBatchImportMultipleTenants(t *testing.T) {
	env := newMultiImportEnv(t)

	platform, err := env.tenantStore.Create("platform-batch", "Platform")
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	token := adminToken(t, env, platform.ID, "admin@platform-batch.test", "platformpass1")

	configs := []tenant.ImportConfig{
		minimalConfig("batch-a"),
		minimalConfig("batch-b"),
	}

	resp := doMultiBatchImport(t, env, configs, token)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	var results []struct {
		Slug     string `json:"slug"`
		TenantID string `json:"tenant_id"`
		Skipped  bool   `json:"skipped"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r.TenantID == "" {
			t.Errorf("results[%d] missing tenant_id", i)
		}
		if r.Skipped {
			t.Errorf("results[%d] should not be skipped", i)
		}
	}
}

// TestBatchImportEmptyArray verifies empty array returns empty result.
func TestBatchImportEmptyArray(t *testing.T) {
	env := newMultiImportEnv(t)

	platform, err := env.tenantStore.Create("platform-empty", "Platform")
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	token := adminToken(t, env, platform.ID, "admin@platform-empty.test", "platformpass1")

	resp := doMultiBatchImport(t, env, []tenant.ImportConfig{}, token)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	var results []interface{}
	if err := json.Unmarshal(body, &results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestBatchImportIdempotent verifies batch import skips already-existing tenants.
func TestBatchImportIdempotent(t *testing.T) {
	env := newMultiImportEnv(t)

	platform, err := env.tenantStore.Create("platform-idem", "Platform")
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	token := adminToken(t, env, platform.ID, "admin@platform-idem.test", "platformpass1")

	cfg := minimalConfig("idem-batch")
	configs := []tenant.ImportConfig{cfg}

	// First import.
	resp1 := doMultiBatchImport(t, env, configs, token)
	resp1.Body.Close()

	// Second import — same slug.
	resp2 := doMultiBatchImport(t, env, configs, token)
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)

	var results []struct {
		Slug    string `json:"slug"`
		Skipped bool   `json:"skipped"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Skipped {
		t.Error("second batch import should mark tenant as skipped")
	}
}

// TestBatchImportInvalidJSON verifies malformed JSON body is rejected.
func TestBatchImportInvalidJSON(t *testing.T) {
	env := newMultiImportEnv(t)

	platform, err := env.tenantStore.Create("platform-inv", "Platform")
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	token := adminToken(t, env, platform.ID, "admin@platform-inv.test", "platformpass1")

	req, _ := http.NewRequest(http.MethodPost,
		env.srv.URL+"/admin/tenants/import-batch",
		strings.NewReader(`[{invalid json`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for invalid JSON body")
	}
}

// TestImportHTTPInvalidJSON verifies single import endpoint rejects malformed JSON.
func TestImportHTTPInvalidJSON(t *testing.T) {
	env := newMultiImportEnv(t)

	platform, err := env.tenantStore.Create("platform-hinv", "Platform")
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	token := adminToken(t, env, platform.ID, "admin@platform-hinv.test", "platformpass1")

	resp := doMultiRawImport(t, env, `{not valid json}`, token)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		t.Error("expected error status for malformed JSON")
	}
}

// TestImportHTTPMissingSlug verifies single import endpoint rejects missing slug.
func TestImportHTTPMissingSlug(t *testing.T) {
	env := newMultiImportEnv(t)

	platform, err := env.tenantStore.Create("platform-noslug", "Platform")
	if err != nil {
		t.Fatalf("create platform: %v", err)
	}
	token := adminToken(t, env, platform.ID, "admin@platform-noslug.test", "platformpass1")

	resp := doMultiRawImport(t, env, `{"name":"No Slug"}`, token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 400, got %d: %s", resp.StatusCode, body)
	}
}

// ---------------------------------------------------------------------------
// Suppress unused import warnings
// ---------------------------------------------------------------------------
var _ = iam.NewStore
var _ = marketing.NewStore
