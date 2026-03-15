package main

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
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Enable WAL mode and foreign keys
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("pragma %s: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS clients (
		id TEXT PRIMARY KEY,
		secret_hash TEXT NOT NULL,
		name TEXT NOT NULL,
		redirect_uris TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS auth_codes (
		code TEXT PRIMARY KEY,
		client_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		redirect_uri TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT '',
		nonce TEXT NOT NULL DEFAULT '',
		code_challenge TEXT NOT NULL DEFAULT '',
		code_challenge_method TEXT NOT NULL DEFAULT '',
		expires_at DATETIME NOT NULL,
		used INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS refresh_tokens (
		token TEXT PRIMARY KEY,
		client_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT '',
		expires_at DATETIME NOT NULL,
		revoked INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS keys (
		id TEXT PRIMARY KEY,
		private_key_pem TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS contacts (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		user_id TEXT REFERENCES users(id),
		unsubscribed INTEGER NOT NULL DEFAULT 0,
		unsubscribe_token TEXT UNIQUE NOT NULL,
		invite_token TEXT UNIQUE,
		consent_source TEXT NOT NULL,
		consent_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS segments (
		id TEXT PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS contact_segments (
		contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (contact_id, segment_id)
	);

	CREATE TABLE IF NOT EXISTS campaigns (
		id TEXT PRIMARY KEY,
		subject TEXT NOT NULL,
		html_body TEXT NOT NULL,
		from_name TEXT NOT NULL DEFAULT '',
		from_email TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'draft',
		sent_at DATETIME,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS campaign_segments (
		campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (campaign_id, segment_id)
	);

	CREATE TABLE IF NOT EXISTS campaign_recipients (
		id TEXT PRIMARY KEY,
		campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		contact_id TEXT NOT NULL REFERENCES contacts(id),
		status TEXT NOT NULL DEFAULT 'queued',
		error_message TEXT NOT NULL DEFAULT '',
		sent_at DATETIME,
		opened_at DATETIME,
		UNIQUE(campaign_id, contact_id)
	);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Add role column if missing (existing databases)
	s.db.Exec("ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'")

	// Add invite_token column if missing (existing databases)
	s.db.Exec("ALTER TABLE contacts ADD COLUMN invite_token TEXT UNIQUE")

	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- RSA Key ---

func (s *Store) LoadOrCreateRSAKey() (*rsa.PrivateKey, error) {
	var pemData string
	err := s.db.QueryRow("SELECT private_key_pem FROM keys LIMIT 1").Scan(&pemData)
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
		"INSERT INTO keys (id, private_key_pem, created_at) VALUES (?, ?, ?)",
		uuid.NewString(), string(pemBytes), time.Now().UTC(),
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
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		Role:         "user",
		CreatedAt:    time.Now().UTC(),
	}

	_, err = s.db.Exec(
		"INSERT INTO users (id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		u.ID, u.Email, u.PasswordHash, u.Name, u.Role, u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByEmail(email string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, email, password_hash, name, role, created_at FROM users WHERE email = ?", email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, email, password_hash, name, role, created_at FROM users WHERE id = ?", id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CreatedAt)
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
		_, err = s.db.Exec("UPDATE users SET role = 'admin' WHERE id = ?", existing.ID)
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"INSERT INTO users (id, email, password_hash, name, role, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.NewString(), email, string(hash), name, "admin", time.Now().UTC(),
	)
	return err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query("SELECT id, email, name, role, created_at FROM users ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt); err != nil {
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
	_, err = s.db.Exec("UPDATE users SET name = ?, role = ? WHERE id = ?", user.Name, user.Role, user.ID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Store) DeleteUser(id string) error {
	result, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
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
	rows, err := s.db.Query("SELECT id, name, redirect_uris, created_at FROM clients ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clients []Client
	for rows.Next() {
		var c Client
		var urisJSON string
		if err := rows.Scan(&c.ID, &c.Name, &urisJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(urisJSON), &c.RedirectURIs)
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func (s *Store) DeleteClient(id string) error {
	result, err := s.db.Exec("DELETE FROM clients WHERE id = ?", id)
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
		SecretHash:   string(hash),
		Name:         name,
		RedirectURIs: redirectURIs,
		CreatedAt:    time.Now().UTC(),
	}

	_, err = s.db.Exec(
		"INSERT INTO clients (id, secret_hash, name, redirect_uris, created_at) VALUES (?, ?, ?, ?, ?)",
		c.ID, c.SecretHash, c.Name, string(urisJSON), c.CreatedAt,
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
		"SELECT id, secret_hash, name, redirect_uris, created_at FROM clients WHERE id = ?", clientID,
	).Scan(&c.ID, &c.SecretHash, &c.Name, &urisJSON, &c.CreatedAt)
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
		`INSERT INTO auth_codes (code, client_id, user_id, redirect_uri, scope, nonce, code_challenge, code_challenge_method, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		code, clientID, userID, redirectURI, scope, nonce, codeChallenge, codeChallengeMethod, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return code, nil
}

func (s *Store) ConsumeAuthCode(code string) (*AuthCode, error) {
	ac := &AuthCode{}
	var used int
	err := s.db.QueryRow(
		`SELECT code, client_id, user_id, redirect_uri, scope, nonce, code_challenge, code_challenge_method, expires_at, used
		 FROM auth_codes WHERE code = ?`, code,
	).Scan(&ac.Code, &ac.ClientID, &ac.UserID, &ac.RedirectURI, &ac.Scope, &ac.Nonce,
		&ac.CodeChallenge, &ac.CodeChallengeMethod, &ac.ExpiresAt, &used)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization code")
	}
	ac.Used = used != 0

	if ac.Used {
		return nil, fmt.Errorf("authorization code already used")
	}
	if time.Now().UTC().After(ac.ExpiresAt) {
		return nil, fmt.Errorf("authorization code expired")
	}

	_, err = s.db.Exec("UPDATE auth_codes SET used = 1 WHERE code = ?", code)
	if err != nil {
		return nil, err
	}
	return ac, nil
}

// --- Refresh Tokens ---

func (s *Store) CreateRefreshToken(clientID, userID, scope string) (string, error) {
	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)

	_, err := s.db.Exec(
		"INSERT INTO refresh_tokens (token, client_id, user_id, scope, expires_at) VALUES (?, ?, ?, ?, ?)",
		token, clientID, userID, scope, expiresAt,
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
		"SELECT token, client_id, user_id, scope, expires_at, revoked FROM refresh_tokens WHERE token = ?", token,
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
	result, err := s.db.Exec("UPDATE refresh_tokens SET revoked = 1 WHERE token = ?", token)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}
