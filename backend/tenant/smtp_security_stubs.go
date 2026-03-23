package tenant

// smtp_security_stubs.go — STUBS for TDD red phase.
// These exist ONLY so smtp_security_test.go compiles.
// Replace with real implementations for H-5, P0-16, I-1.

import (
	"database/sql"
	"fmt"
)

// NewStoreWithKey creates a Store that uses the given encryption key.
// STUB: returns a normal Store (ignoring the key), so decryption-with-wrong-key
// tests will fail as expected.
func NewStoreWithKey(db *sql.DB, key string) *Store {
	// STUB — does not actually use the key
	return &Store{db: db}
}

// MigrateEncryptPasswords encrypts any plaintext SMTP passwords in the DB.
// STUB: not implemented — migration tests will fail.
func (s *Store) MigrateEncryptPasswords() error {
	return fmt.Errorf("MigrateEncryptPasswords not implemented")
}

// GetSMTPConfigWithTLS returns SMTP config including the TLS mode.
// STUB: not implemented — TLS mode tests will fail.
func (s *Store) GetSMTPConfigWithTLS(tenantID string) (host, port, user, password, from, fromName string, rateMS int, tlsMode string, err error) {
	return "", "", "", "", "", "", 0, "", fmt.Errorf("GetSMTPConfigWithTLS not implemented")
}

// SecureSMTPMailer sends email with TLS enforcement.
// STUB: all methods fail.
type SecureSMTPMailer struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	FromName string
	TLSMode  string
}

// NewSecureSMTPMailer creates a mailer that enforces TLS policy.
// STUB: returns a mailer whose Send always fails.
func NewSecureSMTPMailer(host, port, user, password, from, fromName, tlsMode string) *SecureSMTPMailer {
	return &SecureSMTPMailer{
		Host: host, Port: port, User: user, Password: password,
		From: from, FromName: fromName, TLSMode: tlsMode,
	}
}

// Send sends an email respecting TLS mode.
// STUB: not implemented.
func (m *SecureSMTPMailer) Send(to, subject, htmlBody string, headers map[string]string, attachments interface{}) error {
	return fmt.Errorf("SecureSMTPMailer.Send not implemented")
}
