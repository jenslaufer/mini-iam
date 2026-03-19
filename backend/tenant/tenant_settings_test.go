package tenant_test

// tenant_settings_test.go tests GET /admin/tenants/{id} (detail) and
// PUT /admin/tenants/{id} (update) endpoints.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/jenslaufer/launch-kit/tenant"
)

// tenantDetail is the JSON structure returned by GET /admin/tenants/{id}.
type tenantDetail struct {
	ID                  string     `json:"id"`
	Slug                string     `json:"slug"`
	Name                string     `json:"name"`
	RegistrationEnabled bool       `json:"registration_enabled"`
	SMTP                smtpDetail `json:"smtp"`
	CreatedAt           string     `json:"created_at"`
}

type smtpDetail struct {
	Host     string `json:"smtp_host"`
	Port     string `json:"smtp_port"`
	User     string `json:"smtp_user"`
	Password string `json:"smtp_password"`
	From     string `json:"smtp_from"`
	FromName string `json:"smtp_from_name"`
	RateMS   int    `json:"smtp_rate_ms"`
}

// doGet calls GET /admin/tenants/{id} with an admin Bearer token.
func doGet(t *testing.T, env *exportEnv, tenantID, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet,
		env.srv.URL+"/admin/tenants/"+tenantID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET request: %v", err)
	}
	return resp
}

// doPut calls PUT /admin/tenants/{id} with an admin Bearer token.
func doPut(t *testing.T, env *exportEnv, tenantID, token string, body interface{}) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut,
		env.srv.URL+"/admin/tenants/"+tenantID, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT request: %v", err)
	}
	return resp
}

func TestGetTenant(t *testing.T) {
	env := newExportEnv(t)

	tn, err := env.tenantStore.CreateWithSMTP("get-tenant", "Get Tenant", tenant.SMTPConfig{
		Host: "smtp.get.com", Port: "587", User: "user@get.com",
		Password: "secret123", From: "from@get.com", FromName: "Get App", RateMS: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.tenantStore.UpdateRegistrationEnabled(tn.ID, true)

	token := adminToken(t, env, tn.ID, "admin@get-tenant.test", "pass")

	resp := doGet(t, env, tn.ID, token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var td tenantDetail
	json.NewDecoder(resp.Body).Decode(&td)

	if td.Name != "Get Tenant" {
		t.Errorf("name = %q, want %q", td.Name, "Get Tenant")
	}
	if td.Slug != "get-tenant" {
		t.Errorf("slug = %q, want %q", td.Slug, "get-tenant")
	}
	if !td.RegistrationEnabled {
		t.Error("registration_enabled should be true")
	}
	if td.SMTP.Host != "smtp.get.com" {
		t.Errorf("smtp_host = %q, want %q", td.SMTP.Host, "smtp.get.com")
	}
	if td.SMTP.From != "from@get.com" {
		t.Errorf("smtp_from = %q, want %q", td.SMTP.From, "from@get.com")
	}
	// Password must be sanitized
	if td.SMTP.Password != "" {
		t.Errorf("smtp_password should be empty (sanitized), got %q", td.SMTP.Password)
	}
	if td.CreatedAt == "" {
		t.Error("created_at should not be empty")
	}
}

func TestGetTenantNotFound(t *testing.T) {
	env := newExportEnv(t)

	// Create a platform tenant so we have an admin token
	platform, _ := env.tenantStore.Create("platform-nf", "Platform")
	token := adminToken(t, env, platform.ID, "admin@platform-nf.test", "pass")

	resp := doGet(t, env, "nonexistent-id", token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateTenantName(t *testing.T) {
	env := newExportEnv(t)

	tn, _ := env.tenantStore.Create("upd-name", "Original Name")
	token := adminToken(t, env, tn.ID, "admin@upd-name.test", "pass")

	// Update name only
	resp := doPut(t, env, tn.ID, token, map[string]interface{}{
		"name": "New Name",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var td tenantDetail
	json.NewDecoder(resp.Body).Decode(&td)
	if td.Name != "New Name" {
		t.Errorf("name = %q, want %q", td.Name, "New Name")
	}

	// Verify via GET
	resp2 := doGet(t, env, tn.ID, token)
	defer resp2.Body.Close()
	var td2 tenantDetail
	json.NewDecoder(resp2.Body).Decode(&td2)
	if td2.Name != "New Name" {
		t.Errorf("GET after update: name = %q, want %q", td2.Name, "New Name")
	}
}

func TestUpdateRegistrationEnabled(t *testing.T) {
	env := newExportEnv(t)

	tn, _ := env.tenantStore.Create("upd-reg", "Reg Tenant")
	token := adminToken(t, env, tn.ID, "admin@upd-reg.test", "pass")

	// Enable registration
	resp := doPut(t, env, tn.ID, token, map[string]interface{}{
		"registration_enabled": true,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d", resp.StatusCode)
	}

	// Verify enabled
	resp2 := doGet(t, env, tn.ID, token)
	var td tenantDetail
	json.NewDecoder(resp2.Body).Decode(&td)
	resp2.Body.Close()
	if !td.RegistrationEnabled {
		t.Error("registration_enabled should be true after update")
	}

	// Disable registration
	resp3 := doPut(t, env, tn.ID, token, map[string]interface{}{
		"registration_enabled": false,
	})
	resp3.Body.Close()

	resp4 := doGet(t, env, tn.ID, token)
	var td2 tenantDetail
	json.NewDecoder(resp4.Body).Decode(&td2)
	resp4.Body.Close()
	if td2.RegistrationEnabled {
		t.Error("registration_enabled should be false after second update")
	}
}

func TestUpdateSMTP(t *testing.T) {
	env := newExportEnv(t)

	tn, _ := env.tenantStore.Create("upd-smtp", "SMTP Tenant")
	token := adminToken(t, env, tn.ID, "admin@upd-smtp.test", "pass")

	resp := doPut(t, env, tn.ID, token, map[string]interface{}{
		"smtp": map[string]interface{}{
			"host":      "smtp.updated.com",
			"port":      "465",
			"user":      "user@updated.com",
			"password":  "newsecret",
			"from":      "noreply@updated.com",
			"from_name": "Updated App",
			"rate_ms":   50,
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	// Verify via GET — password should be sanitized
	resp2 := doGet(t, env, tn.ID, token)
	defer resp2.Body.Close()
	var td tenantDetail
	json.NewDecoder(resp2.Body).Decode(&td)

	if td.SMTP.Host != "smtp.updated.com" {
		t.Errorf("smtp_host = %q, want %q", td.SMTP.Host, "smtp.updated.com")
	}
	if td.SMTP.Port != "465" {
		t.Errorf("smtp_port = %q, want %q", td.SMTP.Port, "465")
	}
	if td.SMTP.From != "noreply@updated.com" {
		t.Errorf("smtp_from = %q, want %q", td.SMTP.From, "noreply@updated.com")
	}
	if td.SMTP.FromName != "Updated App" {
		t.Errorf("smtp_from_name = %q, want %q", td.SMTP.FromName, "Updated App")
	}
	if td.SMTP.Password != "" {
		t.Errorf("smtp_password should be sanitized, got %q", td.SMTP.Password)
	}
}

func TestUpdateTenantPartial(t *testing.T) {
	env := newExportEnv(t)

	tn, _ := env.tenantStore.CreateWithSMTP("upd-partial", "Partial Tenant", tenant.SMTPConfig{
		Host: "smtp.orig.com", Port: "587", User: "u@orig.com",
		Password: "origpass", From: "f@orig.com", FromName: "Orig", RateMS: 100,
	})
	env.tenantStore.UpdateRegistrationEnabled(tn.ID, true)
	token := adminToken(t, env, tn.ID, "admin@upd-partial.test", "pass")

	// Update only name — SMTP and registration should be unchanged
	resp := doPut(t, env, tn.ID, token, map[string]interface{}{
		"name": "Partial Updated",
	})
	resp.Body.Close()

	resp2 := doGet(t, env, tn.ID, token)
	var td tenantDetail
	json.NewDecoder(resp2.Body).Decode(&td)
	resp2.Body.Close()

	if td.Name != "Partial Updated" {
		t.Errorf("name = %q, want %q", td.Name, "Partial Updated")
	}
	if !td.RegistrationEnabled {
		t.Error("registration_enabled should still be true")
	}
	if td.SMTP.Host != "smtp.orig.com" {
		t.Errorf("smtp_host should be unchanged, got %q", td.SMTP.Host)
	}

	// Update only SMTP — name should be unchanged
	resp3 := doPut(t, env, tn.ID, token, map[string]interface{}{
		"smtp": map[string]interface{}{
			"host": "smtp.new.com",
			"port": "465",
			"from": "new@new.com",
		},
	})
	resp3.Body.Close()

	resp4 := doGet(t, env, tn.ID, token)
	var td2 tenantDetail
	json.NewDecoder(resp4.Body).Decode(&td2)
	resp4.Body.Close()

	if td2.Name != "Partial Updated" {
		t.Errorf("name should still be %q, got %q", "Partial Updated", td2.Name)
	}
	if td2.SMTP.Host != "smtp.new.com" {
		t.Errorf("smtp_host = %q, want %q", td2.SMTP.Host, "smtp.new.com")
	}
}

func TestUpdateTenantRequiresAdmin(t *testing.T) {
	env := newExportEnv(t)

	tn, _ := env.tenantStore.Create("upd-noauth", "No Auth Tenant")

	// No token at all
	resp := doPut(t, env, tn.ID, "", map[string]interface{}{
		"name": "Hacked",
	})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 without admin token")
	}
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 401 or 403, got %d", resp.StatusCode)
	}
}

func TestUpdateTenantNotFound(t *testing.T) {
	env := newExportEnv(t)

	platform, _ := env.tenantStore.Create("platform-upd-nf", "Platform")
	token := adminToken(t, env, platform.ID, "admin@platform-upd-nf.test", "pass")

	resp := doPut(t, env, "nonexistent-id", token, map[string]interface{}{
		"name": "Ghost",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
