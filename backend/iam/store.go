package iam

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Store struct {
	db       *sql.DB
	tenantID string
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// ForTenant returns a copy of the store scoped to the given tenant.
func (s *Store) ForTenant(tenantID string) *Store {
	return &Store{db: s.db, tenantID: tenantID}
}

// TenantID returns the tenant scope of this store.
func (s *Store) TenantID() string {
	return s.tenantID
}

// DB returns the underlying *sql.DB so the marketing package can share it.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// --- RSA Key ---

func (s *Store) LoadOrCreateRSAKey() (*rsa.PrivateKey, error) {
	var pemData string
	err := s.db.QueryRow("SELECT private_key_pem FROM keys WHERE tenant_id = ? LIMIT 1", s.tenantID).Scan(&pemData)
	if err == nil {
		block, _ := pem.Decode([]byte(pemData))
		if block == nil {
			return nil, fmt.Errorf("invalid PEM block")
		}
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	_, err = s.db.Exec(
		"INSERT INTO keys (id, tenant_id, private_key_pem, created_at) VALUES (?, ?, ?, ?)",
		uuid.NewString(), s.tenantID, string(pemBytes), time.Now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("store RSA key: %w", err)
	}

	return key, nil
}

// --- Users ---

func (s *Store) CreateUser(email, password, name string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	u := &User{
		ID:           uuid.NewString(),
		TenantID:     s.tenantID,
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		Role:         "user",
		CreatedAt:    time.Now().UTC(),
	}

	_, err = s.db.Exec(
		"INSERT INTO users (id, tenant_id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.TenantID, u.Email, u.PasswordHash, u.Name, u.Role, u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByEmail(email string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, tenant_id, email, password_hash, name, role, created_at FROM users WHERE email = ? AND tenant_id = ?", email, s.tenantID,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, tenant_id, email, password_hash, name, role, created_at FROM users WHERE id = ? AND tenant_id = ?", id, s.tenantID,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) AuthenticateUser(email, password string) (*User, error) {
	u, err := s.GetUserByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return u, nil
}

func (s *Store) SeedAdmin(email, password, name string) error {
	existing, err := s.GetUserByEmail(email)
	if err == nil {
		_, err = s.db.Exec("UPDATE users SET role = 'admin' WHERE id = ? AND tenant_id = ?", existing.ID, s.tenantID)
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"INSERT INTO users (id, tenant_id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		uuid.NewString(), s.tenantID, email, string(hash), name, "admin", time.Now().UTC(),
	)
	return err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query("SELECT id, tenant_id, email, name, role, created_at FROM users WHERE tenant_id = ? ORDER BY created_at DESC", s.tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) UpdateUser(id string, name, role string) (*User, error) {
	user, err := s.GetUserByID(id)
	if err != nil {
		return nil, err
	}
	if name != "" {
		user.Name = name
	}
	if role != "" {
		user.Role = role
	}
	_, err = s.db.Exec("UPDATE users SET name = ?, role = ? WHERE id = ? AND tenant_id = ?", user.Name, user.Role, user.ID, s.tenantID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Store) UpdateUserPassword(id, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	result, err := s.db.Exec("UPDATE users SET password_hash = ? WHERE id = ? AND tenant_id = ?", string(hash), id, s.tenantID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (s *Store) DeleteUser(id string) error {
	result, err := s.db.Exec("DELETE FROM users WHERE id = ? AND tenant_id = ?", id, s.tenantID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (s *Store) ListClients() ([]Client, error) {
	rows, err := s.db.Query("SELECT id, tenant_id, name, redirect_uris, created_at FROM clients WHERE tenant_id = ? ORDER BY created_at DESC", s.tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clients []Client
	for rows.Next() {
		var c Client
		var urisJSON string
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &urisJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(urisJSON), &c.RedirectURIs)
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func (s *Store) DeleteClient(id string) error {
	result, err := s.db.Exec("DELETE FROM clients WHERE id = ? AND tenant_id = ?", id, s.tenantID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("client not found")
	}
	return nil
}

// --- Clients ---

func (s *Store) CreateClient(name string, redirectURIs []string) (*Client, string, error) {
	secret := uuid.NewString() // raw secret, returned once
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}

	urisJSON, _ := json.Marshal(redirectURIs)

	c := &Client{
		ID:           uuid.NewString(),
		TenantID:     s.tenantID,
		SecretHash:   string(hash),
		Name:         name,
		RedirectURIs: redirectURIs,
		CreatedAt:    time.Now().UTC(),
	}

	_, err = s.db.Exec(
		"INSERT INTO clients (id, tenant_id, secret_hash, name, redirect_uris, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		c.ID, c.TenantID, c.SecretHash, c.Name, string(urisJSON), c.CreatedAt,
	)
	if err != nil {
		return nil, "", err
	}
	return c, secret, nil
}

func (s *Store) GetClient(clientID string) (*Client, error) {
	c := &Client{}
	var urisJSON string
	err := s.db.QueryRow(
		"SELECT id, tenant_id, secret_hash, name, redirect_uris, created_at FROM clients WHERE id = ? AND tenant_id = ?", clientID, s.tenantID,
	).Scan(&c.ID, &c.TenantID, &c.SecretHash, &c.Name, &urisJSON, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(urisJSON), &c.RedirectURIs)
	return c, nil
}

func (s *Store) ValidateClientSecret(client *Client, secret string) bool {
	return bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(secret)) == nil
}

// --- Auth Codes ---

func (s *Store) CreateAuthCode(clientID, userID, redirectURI, scope, nonce, codeChallenge, codeChallengeMethod string) (string, error) {
	code := uuid.NewString()
	expiresAt := time.Now().UTC().Add(10 * time.Minute)

	_, err := s.db.Exec(
		`INSERT INTO auth_codes (code, tenant_id, client_id, user_id, redirect_uri, scope, nonce, code_challenge, code_challenge_method, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		code, s.tenantID, clientID, userID, redirectURI, scope, nonce, codeChallenge, codeChallengeMethod, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return code, nil
}

func (s *Store) ConsumeAuthCode(code string) (*AuthCode, error) {
	// Atomically mark the code as used; prevents TOCTOU race between SELECT and UPDATE.
	result, err := s.db.Exec(
		`UPDATE auth_codes SET used = 1 WHERE code = ? AND tenant_id = ? AND used = 0 AND expires_at > ?`,
		code, s.tenantID, time.Now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization code")
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		// Check why: code not found, already used, or expired
		var used int
		var expiresAt time.Time
		err := s.db.QueryRow(
			`SELECT used, expires_at FROM auth_codes WHERE code = ? AND tenant_id = ?`, code, s.tenantID,
		).Scan(&used, &expiresAt)
		if err != nil {
			return nil, fmt.Errorf("invalid authorization code")
		}
		if used != 0 {
			return nil, fmt.Errorf("authorization code already used")
		}
		return nil, fmt.Errorf("authorization code expired")
	}

	// Now read the full auth code data
	ac := &AuthCode{}
	err = s.db.QueryRow(
		`SELECT code, client_id, user_id, redirect_uri, scope, nonce, code_challenge, code_challenge_method, expires_at
		 FROM auth_codes WHERE code = ? AND tenant_id = ?`, code, s.tenantID,
	).Scan(&ac.Code, &ac.ClientID, &ac.UserID, &ac.RedirectURI, &ac.Scope, &ac.Nonce,
		&ac.CodeChallenge, &ac.CodeChallengeMethod, &ac.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization code")
	}
	ac.Used = true
	return ac, nil
}

// --- Refresh Tokens ---

func (s *Store) CreateRefreshToken(clientID, userID, scope string) (string, error) {
	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)

	_, err := s.db.Exec(
		"INSERT INTO refresh_tokens (token, tenant_id, client_id, user_id, scope, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		token, s.tenantID, clientID, userID, scope, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) ValidateRefreshToken(token string) (*RefreshToken, error) {
	rt := &RefreshToken{}
	var revoked int
	err := s.db.QueryRow(
		"SELECT token, client_id, user_id, scope, expires_at, revoked FROM refresh_tokens WHERE token = ? AND tenant_id = ?", token, s.tenantID,
	).Scan(&rt.Token, &rt.ClientID, &rt.UserID, &rt.Scope, &rt.ExpiresAt, &revoked)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}
	rt.Revoked = revoked != 0

	if rt.Revoked {
		return nil, fmt.Errorf("refresh token revoked")
	}
	if time.Now().UTC().After(rt.ExpiresAt) {
		return nil, fmt.Errorf("refresh token expired")
	}
	return rt, nil
}

func (s *Store) RevokeRefreshToken(token string) error {
	result, err := s.db.Exec("UPDATE refresh_tokens SET revoked = 1 WHERE token = ? AND tenant_id = ?", token, s.tenantID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// --- Activate (cross-domain: reads contacts table, writes users table) ---

// ActivateContact looks up a contact by invite token, creates a User account,
// and links the contact to the new user in a single transaction.
func (s *Store) ActivateContact(inviteToken, password string) (*User, error) {
	contact, err := s.getContactByInviteToken(inviteToken)
	if err != nil {
		return nil, fmt.Errorf("invalid invite token")
	}
	if contact.userID != nil {
		return nil, fmt.Errorf("contact already activated")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &User{
		ID:           uuid.NewString(),
		TenantID:     s.tenantID,
		Email:        contact.email,
		PasswordHash: string(hash),
		Name:         contact.name,
		Role:         "user",
		CreatedAt:    time.Now().UTC(),
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"INSERT INTO users (id, tenant_id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		user.ID, user.TenantID, user.Email, user.PasswordHash, user.Name, user.Role, user.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(
		"UPDATE contacts SET user_id = ?, invite_token = NULL WHERE id = ? AND tenant_id = ?",
		user.ID, contact.id, s.tenantID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

// contactRow is a minimal struct for reading contacts within the iam package.
type contactRow struct {
	id     string
	email  string
	name   string
	userID *string
}

func (s *Store) getContactByInviteToken(token string) (*contactRow, error) {
	c := &contactRow{}
	var userID sql.NullString
	err := s.db.QueryRow(
		`SELECT id, email, name, user_id FROM contacts WHERE invite_token = ? AND tenant_id = ?`, token, s.tenantID,
	).Scan(&c.id, &c.email, &c.name, &userID)
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		c.userID = &userID.String
	}
	return c, nil
}

// GetContactByInviteToken returns contact email and whether it has a user_id,
// used by the Activate handler to render the activation form.
func (s *Store) GetContactByInviteToken(token string) (email string, activated bool, err error) {
	c, e := s.getContactByInviteToken(token)
	if e != nil {
		return "", false, e
	}
	return c.email, c.userID != nil, nil
}
