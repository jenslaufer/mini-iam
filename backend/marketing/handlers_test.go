package marketing

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

// testHandlerEnv bundles everything needed for handler tests.
type testHandlerEnv struct {
	srv      *httptest.Server
	store    *Store
	iamStore *iam.Store
}

func newHandlerEnv(t *testing.T) *testHandlerEnv {
	t.Helper()

	db := newTestHandlerDB(t)
	iamStore := iam.NewStore(db)
	mktStore := NewStore(db)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tokens := iam.NewTokenService(key, "http://test-issuer")
	registry := iam.NewStaticTokenRegistry(tokens)

	iamHandler := iam.NewHandler(iamStore, registry, "http://test-issuer")
	mktHandler := NewHandler(mktStore, iamStore, registry)

	sender := NewCampaignSender(mktStore, &LogMailer{}, "http://test-issuer", 0)
	sender.StartSync()
	mktHandler.SetSender(sender)

	mux := http.NewServeMux()
	// IAM routes needed for login
	mux.HandleFunc("/login", iamHandler.Login)
	// Marketing routes
	mux.HandleFunc("/admin/contacts/import", mktHandler.AdminImportContacts)
	mux.HandleFunc("/admin/contacts", mktHandler.AdminContacts)
	mux.HandleFunc("/admin/contacts/", mktHandler.AdminContactByID)
	mux.HandleFunc("/admin/segments", mktHandler.AdminSegments)
	mux.HandleFunc("/admin/segments/", mktHandler.AdminSegmentByID)
	mux.HandleFunc("/admin/campaigns", mktHandler.AdminCampaigns)
	mux.HandleFunc("/admin/campaigns/", mktHandler.AdminCampaignByID)
	mux.HandleFunc("/track/", mktHandler.TrackOpen)
	mux.HandleFunc("/unsubscribe/", mktHandler.Unsubscribe)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close(); db.Close() })

	return &testHandlerEnv{srv: srv, store: mktStore, iamStore: iamStore}
}

func newTestHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	schema := `
	PRAGMA foreign_keys = ON;
	CREATE TABLE users (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, password_hash TEXT NOT NULL,
		name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user', created_at DATETIME NOT NULL,
		reset_token TEXT, reset_token_expires_at DATETIME,
		UNIQUE(tenant_id, email)
	);
	CREATE TABLE clients (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', secret_hash TEXT NOT NULL, name TEXT NOT NULL,
		redirect_uris TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE auth_codes (
		code TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL, user_id TEXT NOT NULL,
		redirect_uri TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '', nonce TEXT NOT NULL DEFAULT '',
		code_challenge TEXT NOT NULL DEFAULT '', code_challenge_method TEXT NOT NULL DEFAULT '',
		expires_at DATETIME NOT NULL, used INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE refresh_tokens (
		token TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL, user_id TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL, revoked INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE keys (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', private_key_pem TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE contacts (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
		user_id TEXT REFERENCES users(id), unsubscribed INTEGER NOT NULL DEFAULT 0,
		unsubscribe_token TEXT UNIQUE NOT NULL, invite_token TEXT UNIQUE, invite_token_expires_at DATETIME,
		consent_source TEXT NOT NULL, consent_at DATETIME NOT NULL, created_at DATETIME NOT NULL,
		UNIQUE(tenant_id, email)
	);
	CREATE TABLE segments (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL, UNIQUE(tenant_id, name)
	);
	CREATE TABLE contact_segments (
		contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (contact_id, segment_id)
	);
	CREATE TABLE campaigns (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', subject TEXT NOT NULL, html_body TEXT NOT NULL,
		from_name TEXT NOT NULL DEFAULT '', from_email TEXT NOT NULL DEFAULT '',
		attachment_url TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'draft',
		sent_at DATETIME, created_at DATETIME NOT NULL
	);
	CREATE TABLE campaign_segments (
		campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (campaign_id, segment_id)
	);
	CREATE TABLE campaign_recipients (
		id TEXT PRIMARY KEY, campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		contact_id TEXT NOT NULL REFERENCES contacts(id),
		status TEXT NOT NULL DEFAULT 'queued', error_message TEXT NOT NULL DEFAULT '',
		sent_at DATETIME, opened_at DATETIME, UNIQUE(campaign_id, contact_id)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func adminToken(t *testing.T, env *testHandlerEnv) string {
	t.Helper()
	if err := env.iamStore.SeedAdmin("admin@test.com", "adminpass", "Admin"); err != nil {
		t.Fatal(err)
	}
	body := `{"email":"admin@test.com","password":"adminpass"}`
	resp, err := http.Post(env.srv.URL+"/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed: %d %s", resp.StatusCode, b)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	return tok.AccessToken
}

func doReq(t *testing.T, env *testHandlerEnv, method, path, token, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, env.srv.URL+path, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
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

func readJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return m
}

func readJSONArray(t *testing.T, resp *http.Response) []any {
	t.Helper()
	defer resp.Body.Close()
	var a []any
	json.NewDecoder(resp.Body).Decode(&a)
	return a
}

// --- Contact Handler Tests ---

func TestAdminContactsUnauthorized(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "GET", "/admin/contacts", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminContactsCRUD(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create contact
	resp := doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"test@example.com","name":"Test"}`)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status = %d, body = %s", resp.StatusCode, b)
	}
	created := readJSON(t, resp)
	id := created["id"].(string)
	if created["email"] != "test@example.com" {
		t.Errorf("email = %v", created["email"])
	}
	if created["invite_token"] == nil || created["invite_token"] == "" {
		t.Error("invite_token should be returned on create")
	}

	// List contacts
	resp = doReq(t, env, "GET", "/admin/contacts", tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d", resp.StatusCode)
	}
	contacts := readJSONArray(t, resp)
	if len(contacts) != 1 {
		t.Errorf("got %d contacts, want 1", len(contacts))
	}

	// Get by ID
	resp = doReq(t, env, "GET", "/admin/contacts/"+id, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d", resp.StatusCode)
	}
	got := readJSON(t, resp)
	if got["email"] != "test@example.com" {
		t.Errorf("email = %v", got["email"])
	}

	// Delete
	resp = doReq(t, env, "DELETE", "/admin/contacts/"+id, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: status = %d", resp.StatusCode)
	}
	readJSON(t, resp)

	// Verify deleted
	resp = doReq(t, env, "GET", "/admin/contacts/"+id, tok, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminCreateContactDuplicate(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"dup@example.com","name":"Dup"}`).Body.Close()

	resp := doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"dup@example.com","name":"Dup2"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestAdminCreateContactInvalidEmail(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"not-an-email","name":"Bad"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminCreateContactInvalidJSON(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/contacts", tok, `{bad json}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Contact Update Tests ---

func TestUpdateContact(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create contact
	resp := doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"upd@example.com","name":"Original"}`)
	created := readJSON(t, resp)
	id := created["id"].(string)

	// Update
	resp = doReq(t, env, "PUT", "/admin/contacts/"+id, tok,
		`{"name":"Updated","email":"new@example.com"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update: status = %d, body = %s", resp.StatusCode, b)
	}
	updated := readJSON(t, resp)
	if updated["name"] != "Updated" {
		t.Errorf("name = %v, want Updated", updated["name"])
	}
	if updated["email"] != "new@example.com" {
		t.Errorf("email = %v, want new@example.com", updated["email"])
	}

	// GET to verify persistence
	resp = doReq(t, env, "GET", "/admin/contacts/"+id, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d", resp.StatusCode)
	}
	got := readJSON(t, resp)
	if got["name"] != "Updated" {
		t.Errorf("after get: name = %v", got["name"])
	}
	if got["email"] != "new@example.com" {
		t.Errorf("after get: email = %v", got["email"])
	}
}

func TestUpdateContactNotFound(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "PUT", "/admin/contacts/00000000-0000-0000-0000-000000000000", tok,
		`{"name":"X","email":"x@example.com"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUpdateContactDuplicateEmail(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create two contacts
	doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"a@example.com","name":"A"}`).Body.Close()
	resp := doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"b@example.com","name":"B"}`)
	created := readJSON(t, resp)
	idB := created["id"].(string)

	// Try to update B's email to A's email
	resp = doReq(t, env, "PUT", "/admin/contacts/"+idB, tok,
		`{"name":"B","email":"a@example.com"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestUpdateContactRequiresAdmin(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"auth@example.com","name":"Auth"}`)
	created := readJSON(t, resp)
	id := created["id"].(string)

	// Without token
	resp = doReq(t, env, "PUT", "/admin/contacts/"+id, "",
		`{"name":"Hacked","email":"hack@example.com"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}
}

// --- Import Handler Tests ---

func TestAdminImportContacts(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/contacts/import", tok,
		`[{"email":"imp1@example.com","name":"Imp1"},{"email":"imp2@example.com","name":"Imp2"}]`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import: status = %d, body = %s", resp.StatusCode, b)
	}
	result := readJSON(t, resp)
	if result["imported"] != float64(2) {
		t.Errorf("imported = %v, want 2", result["imported"])
	}
}

func TestAdminImportContactsUnauthorized(t *testing.T) {
	env := newHandlerEnv(t)
	resp := doReq(t, env, "POST", "/admin/contacts/import", "", `[]`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// --- Segment Handler Tests ---

func TestAdminSegmentsCRUD(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create
	resp := doReq(t, env, "POST", "/admin/segments", tok,
		`{"name":"Newsletter","description":"Monthly updates"}`)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status = %d, body = %s", resp.StatusCode, b)
	}
	created := readJSON(t, resp)
	segID := created["id"].(string)

	// List
	resp = doReq(t, env, "GET", "/admin/segments", tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d", resp.StatusCode)
	}
	segs := readJSONArray(t, resp)
	if len(segs) != 1 {
		t.Errorf("got %d segments, want 1", len(segs))
	}

	// Get by ID
	resp = doReq(t, env, "GET", "/admin/segments/"+segID, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d", resp.StatusCode)
	}
	readJSON(t, resp)

	// Update
	resp = doReq(t, env, "PUT", "/admin/segments/"+segID, tok,
		`{"name":"Updated","description":"New desc"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status = %d", resp.StatusCode)
	}
	updated := readJSON(t, resp)
	if updated["name"] != "Updated" {
		t.Errorf("name = %v, want Updated", updated["name"])
	}

	// Delete
	resp = doReq(t, env, "DELETE", "/admin/segments/"+segID, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminSegmentCreateNoName(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/segments", tok, `{"description":"no name"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminSegmentContactManagement(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create segment and contact
	resp := doReq(t, env, "POST", "/admin/segments", tok, `{"name":"VIP"}`)
	seg := readJSON(t, resp)
	segID := seg["id"].(string)

	resp = doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"vip@example.com","name":"VIP User"}`)
	contact := readJSON(t, resp)
	contactID := contact["id"].(string)

	// Add contact to segment
	resp = doReq(t, env, "POST", "/admin/segments/"+segID+"/contacts", tok,
		`{"contact_id":"`+contactID+`"}`)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("add: status = %d, body = %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// Get segment with contacts
	resp = doReq(t, env, "GET", "/admin/segments/"+segID, tok, "")
	detail := readJSON(t, resp)
	contacts := detail["contacts"].([]any)
	if len(contacts) != 1 {
		t.Errorf("got %d contacts in segment, want 1", len(contacts))
	}

	// Remove contact from segment
	resp = doReq(t, env, "DELETE", "/admin/segments/"+segID+"/contacts/"+contactID, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove: status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminSegmentAddContactNoID(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/segments", tok, `{"name":"S"}`)
	seg := readJSON(t, resp)
	segID := seg["id"].(string)

	resp = doReq(t, env, "POST", "/admin/segments/"+segID+"/contacts", tok, `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Campaign Handler Tests ---

func TestAdminCampaignsCRUD(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create
	resp := doReq(t, env, "POST", "/admin/campaigns", tok,
		`{"subject":"Welcome","html_body":"<h1>Hi</h1>","from_name":"Test","from_email":"test@example.com"}`)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status = %d, body = %s", resp.StatusCode, b)
	}
	created := readJSON(t, resp)
	campID := created["id"].(string)
	if created["status"] != "draft" {
		t.Errorf("status = %v, want draft", created["status"])
	}

	// List
	resp = doReq(t, env, "GET", "/admin/campaigns", tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d", resp.StatusCode)
	}
	camps := readJSONArray(t, resp)
	if len(camps) != 1 {
		t.Errorf("got %d campaigns, want 1", len(camps))
	}

	// Get by ID
	resp = doReq(t, env, "GET", "/admin/campaigns/"+campID, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d", resp.StatusCode)
	}
	detail := readJSON(t, resp)
	campaign := detail["campaign"].(map[string]any)
	if campaign["subject"] != "Welcome" {
		t.Errorf("subject = %v", campaign["subject"])
	}

	// Update
	resp = doReq(t, env, "PUT", "/admin/campaigns/"+campID, tok,
		`{"subject":"Updated","html_body":"<h1>New</h1>","from_name":"N","from_email":"n@example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status = %d", resp.StatusCode)
	}
	updated := readJSON(t, resp)
	if updated["subject"] != "Updated" {
		t.Errorf("subject = %v, want Updated", updated["subject"])
	}

	// Delete
	resp = doReq(t, env, "DELETE", "/admin/campaigns/"+campID, tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAdminCampaignCreateNoSubject(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/campaigns", tok,
		`{"html_body":"<p>body</p>"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminCampaignCreateNoBody(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/campaigns", tok,
		`{"subject":"No body"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminCampaignSend(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create segment, contact, campaign
	resp := doReq(t, env, "POST", "/admin/segments", tok, `{"name":"Send"}`)
	seg := readJSON(t, resp)
	segID := seg["id"].(string)

	resp = doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"send@example.com","name":"Send"}`)
	contact := readJSON(t, resp)
	contactID := contact["id"].(string)

	doReq(t, env, "POST", "/admin/segments/"+segID+"/contacts", tok,
		`{"contact_id":"`+contactID+`"}`).Body.Close()

	resp = doReq(t, env, "POST", "/admin/campaigns", tok,
		`{"subject":"Test","html_body":"<p>Hi {{.Name}}</p>","from_name":"N","from_email":"n@example.com","segment_ids":["`+segID+`"]}`)
	camp := readJSON(t, resp)
	campID := camp["id"].(string)

	// Send
	resp = doReq(t, env, "POST", "/admin/campaigns/"+campID+"/send", tok, "")
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("send: status = %d, body = %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// Check stats
	resp = doReq(t, env, "GET", "/admin/campaigns/"+campID+"/stats", tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: status = %d", resp.StatusCode)
	}
	stats := readJSON(t, resp)
	if stats["sent"] != float64(1) {
		t.Errorf("sent = %v, want 1", stats["sent"])
	}
}

func TestAdminCampaignSendNonDraft(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/campaigns", tok,
		`{"subject":"S","html_body":"<p>x</p>"}`)
	camp := readJSON(t, resp)
	campID := camp["id"].(string)

	// Send it once
	doReq(t, env, "POST", "/admin/campaigns/"+campID+"/send", tok, "").Body.Close()

	// Try to send again
	resp = doReq(t, env, "POST", "/admin/campaigns/"+campID+"/send", tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminCampaignDeleteNonDraft(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/campaigns", tok,
		`{"subject":"S","html_body":"<p>x</p>"}`)
	camp := readJSON(t, resp)
	campID := camp["id"].(string)

	doReq(t, env, "POST", "/admin/campaigns/"+campID+"/send", tok, "").Body.Close()

	resp = doReq(t, env, "DELETE", "/admin/campaigns/"+campID, tok, "")
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("should not be able to delete non-draft campaign")
	}
}

func TestAdminCampaignUpdateNonDraft(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "POST", "/admin/campaigns", tok,
		`{"subject":"S","html_body":"<p>x</p>"}`)
	camp := readJSON(t, resp)
	campID := camp["id"].(string)

	doReq(t, env, "POST", "/admin/campaigns/"+campID+"/send", tok, "").Body.Close()

	resp = doReq(t, env, "PUT", "/admin/campaigns/"+campID, tok,
		`{"subject":"New","html_body":"<p>y</p>"}`)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("should not be able to update non-draft campaign")
	}
}

// --- Public Endpoint Tests ---

func TestTrackOpenReturnsGIF(t *testing.T) {
	env := newHandlerEnv(t)

	resp := doReq(t, env, "GET", "/track/some-id", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "image/gif" {
		t.Errorf("content-type = %q, want image/gif", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("empty body")
	}
}

func TestUnsubscribeGetInvalidToken(t *testing.T) {
	env := newHandlerEnv(t)

	resp := doReq(t, env, "GET", "/unsubscribe/bad-token", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUnsubscribeFlow(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	// Create contact
	resp := doReq(t, env, "POST", "/admin/contacts", tok,
		`{"email":"unsub@example.com","name":"Unsub"}`)
	readJSON(t, resp)

	// Get the unsubscribe token from the store
	contact, err := env.store.GetContactByEmail("unsub@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// GET unsubscribe page
	resp = doReq(t, env, "GET", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get unsub page: status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "unsub@example.com") {
		t.Error("unsubscribe page should show email")
	}

	// POST unsubscribe
	resp = doReq(t, env, "POST", "/unsubscribe/"+contact.UnsubscribeToken, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post unsub: status = %d", resp.StatusCode)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "unsubscribed") {
		t.Error("confirmation page should confirm unsubscription")
	}

	// Verify contact is unsubscribed
	contact, _ = env.store.GetContactByEmail("unsub@example.com")
	if !contact.Unsubscribed {
		t.Error("contact should be unsubscribed")
	}
}

func TestUnsubscribePostInvalidToken(t *testing.T) {
	env := newHandlerEnv(t)

	resp := doReq(t, env, "POST", "/unsubscribe/invalid", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// --- Method Not Allowed Tests ---

func TestContactsMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "PUT", "/admin/contacts", tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestImportMethodNotAllowed(t *testing.T) {
	env := newHandlerEnv(t)
	tok := adminToken(t, env)

	resp := doReq(t, env, "GET", "/admin/contacts/import", tok, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
