package audit

import (
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jenslaufer/launch-kit/iam"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	PRAGMA foreign_keys = ON;
	CREATE TABLE users (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL,
		password_hash TEXT NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user',
		created_at DATETIME NOT NULL, reset_token TEXT, reset_token_expires_at DATETIME,
		UNIQUE(tenant_id, email)
	);
	CREATE TABLE clients (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', secret_hash TEXT NOT NULL,
		name TEXT NOT NULL, redirect_uris TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE auth_codes (
		code TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL,
		user_id TEXT NOT NULL, redirect_uri TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '',
		nonce TEXT NOT NULL DEFAULT '', code_challenge TEXT NOT NULL DEFAULT '',
		code_challenge_method TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL,
		used INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE refresh_tokens (
		token TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL,
		user_id TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL,
		revoked INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE keys (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', private_key_pem TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);
	CREATE TABLE audit_log (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', actor_id TEXT NOT NULL,
		actor_type TEXT NOT NULL, action TEXT NOT NULL, target_type TEXT NOT NULL,
		target_id TEXT NOT NULL, details TEXT NOT NULL DEFAULT '', timestamp DATETIME NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRecordAndList(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db).ForTenant("t1")

	if err := s.Record("actor1", "user", "user.role_change", "user", "target1",
		map[string]string{"old_role": "user", "new_role": "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record("actor2", "service", "client.create", "client", "target2",
		map[string]string{"name": "my-app"}); err != nil {
		t.Fatal(err)
	}

	// List all
	entries, err := s.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Most recent first
	if entries[0].Action != "client.create" {
		t.Errorf("expected client.create first, got %s", entries[0].Action)
	}

	// Filter by action
	entries, err = s.List(Filter{Action: "user.role_change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ActorID != "actor1" {
		t.Errorf("expected actor1, got %s", entries[0].ActorID)
	}

	// Filter by actor
	entries, err = s.List(Filter{ActorID: "actor2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// Filter by target type
	entries, err = s.List(Filter{TargetType: "client"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// Tenant isolation
	s2 := NewStore(db).ForTenant("t2")
	entries, err = s2.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for t2, got %d", len(entries))
	}
}

func TestRecordDetails(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db).ForTenant("t1")

	details := map[string]string{"old_role": "user", "new_role": "admin"}
	if err := s.Record("actor1", "user", "user.role_change", "user", "u1", details); err != nil {
		t.Fatal(err)
	}

	entries, err := s.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}

	var got map[string]string
	if err := json.Unmarshal(entries[0].Details, &got); err != nil {
		t.Fatal(err)
	}
	if got["old_role"] != "user" || got["new_role"] != "admin" {
		t.Errorf("unexpected details: %v", got)
	}
}

// --- Integration tests via HTTP handler ---

type testEnv struct {
	srv        *httptest.Server
	iamStore   *iam.Store
	auditStore *Store
}

func newHandlerEnv(t *testing.T) *testEnv {
	t.Helper()
	db := newTestDB(t)

	iamStore := iam.NewStore(db)
	auditStore := NewStore(db)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tokens := iam.NewTokenService(key, "http://test-issuer")
	registry := iam.NewStaticTokenRegistry(tokens)

	iamH := iam.NewHandler(iamStore, registry, "http://test-issuer")
	iamH.AuditFn = func(tenantID, actorID, actorType, action, targetType, targetID string, details interface{}) {
		auditStore.ForTenant(tenantID).Record(actorID, actorType, action, targetType, targetID, details)
	}

	auditH := NewHandler(auditStore, iamStore, registry, "")

	mux := http.NewServeMux()
	mux.HandleFunc("/login", iamH.Login)
	mux.HandleFunc("/clients", iamH.CreateClient)
	mux.HandleFunc("/admin/users", iamH.AdminListUsers)
	mux.HandleFunc("/admin/users/", iamH.AdminUserByID)
	mux.HandleFunc("/admin/clients/", iamH.AdminClientByID)
	mux.HandleFunc("/admin/audit-log", auditH.ListAuditLog)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })

	return &testEnv{srv: srv, iamStore: iamStore, auditStore: auditStore}
}

func adminToken(t *testing.T, env *testEnv) string {
	t.Helper()
	if err := env.iamStore.SeedAdmin("admin@test.com", "adminpass", "Admin"); err != nil {
		t.Fatalf("SeedAdmin failed: %v", err)
	}
	resp := postJSON(t, env, "/login", `{"email":"admin@test.com","password":"adminpass"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed: %d: %s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	return tok.AccessToken
}

func postJSON(t *testing.T, env *testEnv, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(env.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func doReq(t *testing.T, env *testEnv, method, path, token, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, env.srv.URL+path, bodyReader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&m)
	return m
}

func TestAuditOnClientCreate(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"test-app","redirect_uris":["http://localhost/cb"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	result := readJSON(t, resp)
	clientID := result["client_id"].(string)

	// Check audit log
	entries, err := env.auditStore.ForTenant("").List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Action != "client.create" {
		t.Errorf("expected client.create, got %s", e.Action)
	}
	if e.TargetType != "client" {
		t.Errorf("expected target_type=client, got %s", e.TargetType)
	}
	if e.TargetID != clientID {
		t.Errorf("expected target_id=%s, got %s", clientID, e.TargetID)
	}
	if e.ActorType != "user" {
		t.Errorf("expected actor_type=user, got %s", e.ActorType)
	}
}

func TestAuditOnClientDelete(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create client first
	resp := doReq(t, env, "POST", "/clients", tok, `{"name":"del-app","redirect_uris":["http://localhost/cb"]}`)
	defer resp.Body.Close()
	result := readJSON(t, resp)
	clientID := result["client_id"].(string)

	// Delete client
	resp2 := doReq(t, env, "DELETE", "/admin/clients/"+clientID, tok, "")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}

	entries, err := env.auditStore.ForTenant("").List(Filter{Action: "client.delete"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for client.delete, got %d", len(entries))
	}
	if entries[0].TargetID != clientID {
		t.Errorf("expected target_id=%s, got %s", clientID, entries[0].TargetID)
	}
}

func TestAuditOnUserRoleChange(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create a regular user via register-like flow (seed + update)
	env.iamStore.CreateUser("user@test.com", "password123", "Test User")
	// Get user ID
	users, _ := env.iamStore.ListUsers()
	var userID string
	for _, u := range users {
		if u.Email == "user@test.com" {
			userID = u.ID
			break
		}
	}

	// Change role to admin
	resp := doReq(t, env, "PUT", "/admin/users/"+userID, tok, `{"role":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	entries, err := env.auditStore.ForTenant("").List(Filter{Action: "user.role_change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	e := entries[0]
	if e.TargetType != "user" {
		t.Errorf("expected target_type=user, got %s", e.TargetType)
	}
	if e.TargetID != userID {
		t.Errorf("expected target_id=%s, got %s", userID, e.TargetID)
	}

	var details map[string]string
	json.Unmarshal(e.Details, &details)
	if details["old_role"] != "user" {
		t.Errorf("expected old_role=user, got %s", details["old_role"])
	}
	if details["new_role"] != "admin" {
		t.Errorf("expected new_role=admin, got %s", details["new_role"])
	}
}

func TestAuditLogAPIFiltering(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Generate some audit entries
	s := env.auditStore.ForTenant("")
	s.Record("a1", "user", "client.create", "client", "c1", nil)
	s.Record("a1", "user", "client.delete", "client", "c2", nil)
	s.Record("a2", "user", "user.role_change", "user", "u1", nil)

	// List all
	resp := doReq(t, env, "GET", "/admin/audit-log", tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var entries []Entry
	json.NewDecoder(resp.Body).Decode(&entries)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Filter by action
	resp2 := doReq(t, env, "GET", "/admin/audit-log?action=client.create", tok, "")
	defer resp2.Body.Close()
	var filtered []Entry
	json.NewDecoder(resp2.Body).Decode(&filtered)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered entry, got %d", len(filtered))
	}
	if filtered[0].Action != "client.create" {
		t.Errorf("expected client.create, got %s", filtered[0].Action)
	}

	// Filter by actor_id
	resp3 := doReq(t, env, "GET", "/admin/audit-log?actor_id=a2", tok, "")
	defer resp3.Body.Close()
	var byActor []Entry
	json.NewDecoder(resp3.Body).Decode(&byActor)
	if len(byActor) != 1 {
		t.Fatalf("expected 1 entry for a2, got %d", len(byActor))
	}

	// Filter by target_type
	resp4 := doReq(t, env, "GET", "/admin/audit-log?target_type=client", tok, "")
	defer resp4.Body.Close()
	var byTarget []Entry
	json.NewDecoder(resp4.Body).Decode(&byTarget)
	if len(byTarget) != 2 {
		t.Fatalf("expected 2 client entries, got %d", len(byTarget))
	}
}

func TestAuditLogAPIRequiresAdmin(t *testing.T) {
	env := newHandlerEnv(t)

	resp := doReq(t, env, "GET", "/admin/audit-log", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
