package iam

import (
	"database/sql"
	"testing"

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
		id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL,
		name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user', created_at DATETIME NOT NULL
	);
	CREATE TABLE clients (
		id TEXT PRIMARY KEY, secret_hash TEXT NOT NULL, name TEXT NOT NULL,
		redirect_uris TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE auth_codes (
		code TEXT PRIMARY KEY, client_id TEXT NOT NULL, user_id TEXT NOT NULL,
		redirect_uri TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '', nonce TEXT NOT NULL DEFAULT '',
		code_challenge TEXT NOT NULL DEFAULT '', code_challenge_method TEXT NOT NULL DEFAULT '',
		expires_at DATETIME NOT NULL, used INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE refresh_tokens (
		token TEXT PRIMARY KEY, client_id TEXT NOT NULL, user_id TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL, revoked INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE keys (
		id TEXT PRIMARY KEY, private_key_pem TEXT NOT NULL, created_at DATETIME NOT NULL
	);
	CREATE TABLE contacts (
		id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, name TEXT NOT NULL DEFAULT '',
		user_id TEXT REFERENCES users(id), unsubscribed INTEGER NOT NULL DEFAULT 0,
		unsubscribe_token TEXT UNIQUE NOT NULL, invite_token TEXT UNIQUE,
		consent_source TEXT NOT NULL, consent_at DATETIME NOT NULL, created_at DATETIME NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(newTestDB(t))
}

// --- User Tests ---

func TestCreateUser(t *testing.T) {
	s := newTestStore(t)

	u, err := s.CreateUser("alice@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email = %q", u.Email)
	}
	if u.Name != "Alice" {
		t.Errorf("name = %q", u.Name)
	}
	if u.Role != "user" {
		t.Errorf("role = %q, want user", u.Role)
	}
	if u.ID == "" {
		t.Error("id is empty")
	}
	if u.PasswordHash == "" {
		t.Error("password_hash is empty")
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	s := newTestStore(t)

	s.CreateUser("dup@example.com", "pass", "Dup")
	_, err := s.CreateUser("dup@example.com", "pass2", "Dup2")
	if err == nil {
		t.Error("expected error for duplicate email")
	}
}

func TestGetUserByEmail(t *testing.T) {
	s := newTestStore(t)

	s.CreateUser("bob@example.com", "pass", "Bob")
	u, err := s.GetUserByEmail("bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Bob" {
		t.Errorf("name = %q", u.Name)
	}
}

func TestGetUserByEmailNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetUserByEmail("nonexistent@example.com")
	if err == nil {
		t.Error("expected error")
	}
}

func TestGetUserByID(t *testing.T) {
	s := newTestStore(t)

	created, _ := s.CreateUser("id@example.com", "pass", "ID")
	u, err := s.GetUserByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "id@example.com" {
		t.Errorf("email = %q", u.Email)
	}
}

func TestGetUserByIDNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetUserByID("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAuthenticateUser(t *testing.T) {
	s := newTestStore(t)

	s.CreateUser("auth@example.com", "correct", "Auth")

	u, err := s.AuthenticateUser("auth@example.com", "correct")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "auth@example.com" {
		t.Errorf("email = %q", u.Email)
	}
}

func TestAuthenticateUserWrongPassword(t *testing.T) {
	s := newTestStore(t)

	s.CreateUser("auth2@example.com", "correct", "Auth")
	_, err := s.AuthenticateUser("auth2@example.com", "wrong")
	if err == nil {
		t.Error("expected error for wrong password")
	}
}

func TestAuthenticateUserNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.AuthenticateUser("ghost@example.com", "pass")
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}

func TestSeedAdmin(t *testing.T) {
	s := newTestStore(t)

	if err := s.SeedAdmin("admin@example.com", "adminpass", "Admin"); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetUserByEmail("admin@example.com")
	if u.Role != "admin" {
		t.Errorf("role = %q, want admin", u.Role)
	}
}

func TestSeedAdminUpgradesExisting(t *testing.T) {
	s := newTestStore(t)

	s.CreateUser("upgrade@example.com", "pass", "User")
	if err := s.SeedAdmin("upgrade@example.com", "ignored", "Ignored"); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetUserByEmail("upgrade@example.com")
	if u.Role != "admin" {
		t.Errorf("role = %q, want admin", u.Role)
	}
}

func TestListUsers(t *testing.T) {
	s := newTestStore(t)

	s.CreateUser("a@example.com", "pass", "A")
	s.CreateUser("b@example.com", "pass", "B")

	users, err := s.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Errorf("got %d users, want 2", len(users))
	}
}

func TestUpdateUser(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("upd@example.com", "pass", "Old")
	updated, err := s.UpdateUser(u.ID, "New", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "New" {
		t.Errorf("name = %q, want New", updated.Name)
	}
	if updated.Role != "admin" {
		t.Errorf("role = %q, want admin", updated.Role)
	}
}

func TestUpdateUserPartial(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("part@example.com", "pass", "Original")
	updated, err := s.UpdateUser(u.ID, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Original" {
		t.Errorf("name = %q, want Original (unchanged)", updated.Name)
	}
}

func TestUpdateUserNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.UpdateUser("nonexistent", "X", "user")
	if err == nil {
		t.Error("expected error")
	}
}

func TestDeleteUser(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("del@example.com", "pass", "Del")
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetUserByID(u.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteUserNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.DeleteUser("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

// --- Client Tests ---

func TestCreateClient(t *testing.T) {
	s := newTestStore(t)

	c, secret, err := s.CreateClient("MyApp", []string{"http://localhost:3000/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "MyApp" {
		t.Errorf("name = %q", c.Name)
	}
	if secret == "" {
		t.Error("secret is empty")
	}
	if c.ID == "" {
		t.Error("id is empty")
	}
	if len(c.RedirectURIs) != 1 {
		t.Errorf("got %d redirect_uris", len(c.RedirectURIs))
	}
}

func TestGetClient(t *testing.T) {
	s := newTestStore(t)

	created, _, _ := s.CreateClient("App", []string{"http://localhost/cb"})
	got, err := s.GetClient(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "App" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestGetClientNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetClient("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestValidateClientSecret(t *testing.T) {
	s := newTestStore(t)

	c, secret, _ := s.CreateClient("App", []string{"http://localhost/cb"})
	if !s.ValidateClientSecret(c, secret) {
		t.Error("valid secret rejected")
	}
	if s.ValidateClientSecret(c, "wrong-secret") {
		t.Error("invalid secret accepted")
	}
}

func TestListClients(t *testing.T) {
	s := newTestStore(t)

	s.CreateClient("A", []string{"http://a/cb"})
	s.CreateClient("B", []string{"http://b/cb"})

	clients, err := s.ListClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Errorf("got %d clients, want 2", len(clients))
	}
}

func TestDeleteClient(t *testing.T) {
	s := newTestStore(t)

	c, _, _ := s.CreateClient("Del", []string{"http://del/cb"})
	if err := s.DeleteClient(c.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetClient(c.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteClientNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.DeleteClient("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

// --- Auth Code Tests ---

func TestCreateAndConsumeAuthCode(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("code@example.com", "pass", "Code")
	c, _, _ := s.CreateClient("App", []string{"http://app/cb"})

	code, err := s.CreateAuthCode(c.ID, u.ID, "http://app/cb", "openid", "nonce123", "challenge", "S256")
	if err != nil {
		t.Fatal(err)
	}

	ac, err := s.ConsumeAuthCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if ac.ClientID != c.ID {
		t.Errorf("client_id = %q", ac.ClientID)
	}
	if ac.UserID != u.ID {
		t.Errorf("user_id = %q", ac.UserID)
	}
	if ac.Nonce != "nonce123" {
		t.Errorf("nonce = %q", ac.Nonce)
	}
}

func TestConsumeAuthCodeAlreadyUsed(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("used@example.com", "pass", "Used")
	c, _, _ := s.CreateClient("App", []string{"http://app/cb"})
	code, _ := s.CreateAuthCode(c.ID, u.ID, "http://app/cb", "", "", "", "")

	s.ConsumeAuthCode(code)
	_, err := s.ConsumeAuthCode(code)
	if err == nil {
		t.Error("expected error for already used code")
	}
}

func TestConsumeAuthCodeInvalid(t *testing.T) {
	s := newTestStore(t)

	_, err := s.ConsumeAuthCode("invalid-code")
	if err == nil {
		t.Error("expected error for invalid code")
	}
}

// --- Refresh Token Tests ---

func TestCreateAndValidateRefreshToken(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("rt@example.com", "pass", "RT")
	token, err := s.CreateRefreshToken("client1", u.ID, "openid")
	if err != nil {
		t.Fatal(err)
	}

	rt, err := s.ValidateRefreshToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if rt.UserID != u.ID {
		t.Errorf("user_id = %q", rt.UserID)
	}
	if rt.Scope != "openid" {
		t.Errorf("scope = %q", rt.Scope)
	}
}

func TestValidateRefreshTokenInvalid(t *testing.T) {
	s := newTestStore(t)

	_, err := s.ValidateRefreshToken("invalid-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestRevokeRefreshToken(t *testing.T) {
	s := newTestStore(t)

	u, _ := s.CreateUser("rev@example.com", "pass", "Rev")
	token, _ := s.CreateRefreshToken("", u.ID, "openid")

	if err := s.RevokeRefreshToken(token); err != nil {
		t.Fatal(err)
	}
	_, err := s.ValidateRefreshToken(token)
	if err == nil {
		t.Error("expected error for revoked token")
	}
}

func TestRevokeRefreshTokenNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.RevokeRefreshToken("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

// --- RSA Key Tests ---

func TestLoadOrCreateRSAKey(t *testing.T) {
	s := newTestStore(t)

	key1, err := s.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	if key1 == nil {
		t.Fatal("key is nil")
	}

	// Loading again should return the same key
	key2, err := s.LoadOrCreateRSAKey()
	if err != nil {
		t.Fatal(err)
	}
	if key1.D.Cmp(key2.D) != 0 {
		t.Error("second load returned a different key")
	}
}

// --- DB/Close Tests ---

func TestDBReturnsUnderlyingDB(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	if s.DB() != db {
		t.Error("DB() returned different instance")
	}
}

// --- ActivateContact Tests ---

func TestActivateContact(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()

	// Insert a contact with invite_token
	db.Exec(`INSERT INTO contacts (id, email, name, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		VALUES ('c1', 'invite@example.com', 'Invite', 'unsub1', 'inv-token-123', 'api', datetime('now'), datetime('now'))`)

	user, err := s.ActivateContact("inv-token-123", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "invite@example.com" {
		t.Errorf("email = %q", user.Email)
	}
	if user.Role != "user" {
		t.Errorf("role = %q", user.Role)
	}

	// Verify contact is linked
	var userID sql.NullString
	db.QueryRow("SELECT user_id FROM contacts WHERE id = 'c1'").Scan(&userID)
	if !userID.Valid || userID.String != user.ID {
		t.Error("contact not linked to user")
	}

	// Invite token should be nulled
	var invToken sql.NullString
	db.QueryRow("SELECT invite_token FROM contacts WHERE id = 'c1'").Scan(&invToken)
	if invToken.Valid {
		t.Error("invite_token should be NULL after activation")
	}
}

func TestActivateContactInvalidToken(t *testing.T) {
	s := newTestStore(t)

	_, err := s.ActivateContact("bad-token", "password123")
	if err == nil {
		t.Error("expected error for invalid invite token")
	}
}

func TestActivateContactAlreadyActivated(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()

	// Create user first
	u, _ := s.CreateUser("existing@example.com", "pass", "Existing")

	// Insert contact that's already linked to a user
	db.Exec(`INSERT INTO contacts (id, email, name, user_id, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		VALUES ('c2', 'linked@example.com', 'Linked', ?, 'unsub2', 'inv-token-456', 'api', datetime('now'), datetime('now'))`, u.ID)

	_, err := s.ActivateContact("inv-token-456", "newpass123")
	if err == nil {
		t.Error("expected error for already activated contact")
	}
}

func TestGetContactByInviteToken(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()

	db.Exec(`INSERT INTO contacts (id, email, name, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		VALUES ('c3', 'token@example.com', 'Token', 'unsub3', 'inv-token-789', 'api', datetime('now'), datetime('now'))`)

	email, activated, err := s.GetContactByInviteToken("inv-token-789")
	if err != nil {
		t.Fatal(err)
	}
	if email != "token@example.com" {
		t.Errorf("email = %q", email)
	}
	if activated {
		t.Error("should not be activated")
	}
}

func TestGetContactByInviteTokenNotFound(t *testing.T) {
	s := newTestStore(t)

	_, _, err := s.GetContactByInviteToken("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}
