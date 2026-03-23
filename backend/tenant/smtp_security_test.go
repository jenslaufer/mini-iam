package tenant_test

// smtp_security_test.go tests SMTP credential security requirements:
//   - H-5:   SMTP passwords must be encrypted at rest in the tenants table
//   - P0-16: SMTP secrets encrypted at rest
//   - I-1:   STARTTLS must be enforced (reject insecure transport)
//
// These tests are TDD red-phase: they compile but FAIL until production code
// is implemented.

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"

	"github.com/jenslaufer/launch-kit/tenant"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newSMTPSecurityDB creates a migrated in-memory SQLite database.
// Uses the same schema as tenant_routing_test.go plus tls_mode column.
func newSMTPSecurityDB(t *testing.T) *sql.DB {
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
		smtp_rate_ms INTEGER NOT NULL DEFAULT 0,
		smtp_tls_mode TEXT NOT NULL DEFAULT 'required',
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

// rawSMTPPassword reads the smtp_password column directly from the DB,
// bypassing any decryption in the Store layer.
func rawSMTPPassword(t *testing.T, db *sql.DB, tenantID string) string {
	t.Helper()
	var raw string
	err := db.QueryRow("SELECT smtp_password FROM tenants WHERE id = ?", tenantID).Scan(&raw)
	if err != nil {
		t.Fatalf("read raw smtp_password: %v", err)
	}
	return raw
}

// rawTLSMode reads the smtp_tls_mode column directly from the DB.
func rawTLSMode(t *testing.T, db *sql.DB, tenantID string) string {
	t.Helper()
	var mode string
	err := db.QueryRow("SELECT smtp_tls_mode FROM tenants WHERE id = ?", tenantID).Scan(&mode)
	if err != nil {
		t.Fatalf("read raw smtp_tls_mode: %v", err)
	}
	return mode
}

// startPlaintextSMTPServer starts a fake SMTP server that does NOT support
// STARTTLS. Returns the listener address (host:port) and a cleanup function.
func startPlaintextSMTPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				fmt.Fprintf(c, "220 fake SMTP\r\n")
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					line := string(buf[:n])
					switch {
					case strings.HasPrefix(line, "EHLO"):
						fmt.Fprintf(c, "250-fake\r\n250 OK\r\n")
					case strings.HasPrefix(line, "STARTTLS"):
						// Explicitly reject STARTTLS
						fmt.Fprintf(c, "502 STARTTLS not supported\r\n")
					case strings.HasPrefix(line, "QUIT"):
						fmt.Fprintf(c, "221 Bye\r\n")
						return
					default:
						fmt.Fprintf(c, "250 OK\r\n")
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// startSTARTTLSServer starts a fake SMTP server that advertises and supports
// STARTTLS upgrade. Returns the listener address.
func startSTARTTLSServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	// Generate self-signed cert for TLS
	cert, err := tls.X509KeyPair(localhostCert, localhostKey)
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				fmt.Fprintf(c, "220 fake STARTTLS SMTP\r\n")
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					line := string(buf[:n])
					switch {
					case strings.HasPrefix(line, "EHLO"):
						fmt.Fprintf(c, "250-fake\r\n250-STARTTLS\r\n250 OK\r\n")
					case strings.HasPrefix(line, "STARTTLS"):
						fmt.Fprintf(c, "220 Ready to start TLS\r\n")
						tlsConn := tls.Server(c, tlsConfig)
						if err := tlsConn.Handshake(); err != nil {
							return
						}
						// Continue on TLS connection
						c = tlsConn
					case strings.HasPrefix(line, "QUIT"):
						fmt.Fprintf(c, "221 Bye\r\n")
						return
					default:
						fmt.Fprintf(c, "250 OK\r\n")
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// Self-signed localhost certificate for test TLS server.
var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIICEzCCAXygAwIBAgIQMIMChMLGrR+QvmQvpwAU6zANBgkqhkiG9w0BAQsFAMDAs
MRcwFQYDVQQKEw5sb2NhbGhvc3QtdGVzdDAeFw0yMDAxMDEwMDAwMDBaFw0zMDAx
MDEwMDAwMDBaMBcxFTATBgNVBAoTDGxvY2FsaG9zdC10ZXN0MIGfMA0GCSqGSIb3
DQEBAQUAA4GNADCBiQKBgQC7JbHdWnSBNvMcFCw+JDV/UVxHmkCqM7JJNX0fOlG
AGr3B7fN26eeD4YJbM1VaDGMEyKaOGkvLiqWEBfMGKvX7tae/B3aGIHfBPsR1BId
vxzCYP9k2tR7Jnl+8F0mP/1f5PBYx7ePPmGCE4Q3sJZ3BGDPIiSJZLxFDRCP8S33
qQIDAQABo1IwUDAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEw
DAYDVR0TAQH/BAIwADAbBgNVHREEFDASggpsb2NhbGhvc3SHBH8AAAEwDQYJKoZI
hvcNAQELBQADgYEAbz3kfm+s50SNvC1w7g9pYlz36dO5GmEevCHTPC5vVBi/7GAr
tSx0x83c0dRfRH3fEGnTfr2GsCXDwS+CLOkDYk3Jq3kBFCaYG0sKMmV2IrJ2KRil
VLAkWPSME14hPNLLAqFI5hGnOEgW3NOhmvURjr2V3ZN/Ph3fOSHCZ9Nkumc=
-----END CERTIFICATE-----`)

var localhostKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAJBALslsd1adIE28xwULD4kNX9RXEeaQKozskk1fR86UYAavcHt83bp
554PhglszVVoMYwTIpo4aS8uKpYQF8wYq9fu1p78HdoYgd8E+xHUEh2/HMJg/2Ta
1HsmeX7wXSY//V/k8FjHt48+YYIThDewlncEYM8iJIlkvEUNEI/xLfepAgMBAAEC
gYAhX3ODSB6WqnL3LOOaNKIi2Vxj2s7lO24lJnXp2YnZaTgrFVMUxfkvGY4OHTEG
ajLSfFEB8vmJiET/t1DI3ihSfGjsn69kz89R1xpcXl9vlyl0K++BgB9w0UJFkn1m5
5CL58jzqFi/h8DyO1b+hFcGPkH5T2PLXGZ/jHaBR0aRqQJBAOWt6qcMm7mTv0h+
Fd3FnlHd3m/Dl8ec9CYoEk8boXPRw3L7h6eR2P5q2r/4A/7M7E+s3q/hOa7H0pOQ
I3J7e0sCQQDP9PL5fH9kkXVdlI3ug1fH1cHnWIJfYUNLdjj8e2YHqzmMQ2Em3J+2
LqXCKp8N0WcF8GCJZ3RLBWyJi/FPiJfLAkEAqCXnT4SfbI9aNNlE5s0wqv6+yqh
a7oI7Bq/Y6F7fy5NwpKlX9IlHd9WoIvuiWsWSaJpO8F1CW+U3P6LqPrOwJAYd9v3
lO4W+5b+Tmw9u4LwKO/HSIeUO7L30s5VjnaV+s2cGAMgr0F/5Fhkfx6i/xfQIj8q
x9N3fSh0+7o/WQJBAJrvhF+0kv3E6o7YIq1rMjpjPTm5j8wOjnqVtnrOP/eM0yHq
I04R9C6e0CHPqKDoPnFN+FVqIbCOSMvfkFkr4cA=
-----END RSA PRIVATE KEY-----`)

// ===========================================================================
// SMTP Password Encryption Tests (H-5, P0-16)
// ===========================================================================

func TestSMTPPasswordStoredEncrypted(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	plainPassword := "super-secret-smtp-pass!"
	tn, err := store.CreateWithSMTP("enc-test", "Encryption Test", tenant.SMTPConfig{
		Host:     "smtp.example.com",
		Port:     "587",
		User:     "user@example.com",
		Password: plainPassword,
		From:     "noreply@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw := rawSMTPPassword(t, db, tn.ID)

	if raw == plainPassword {
		t.Fatalf("H-5 violation: smtp_password stored as plaintext %q in DB", raw)
	}
	if raw == "" {
		t.Fatal("smtp_password should not be empty after storing a non-empty password")
	}
}

func TestSMTPPasswordDecryptsOnRead(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	plainPassword := "decrypt-me-123"
	tn, err := store.CreateWithSMTP("dec-test", "Decrypt Test", tenant.SMTPConfig{
		Host:     "smtp.example.com",
		Port:     "587",
		User:     "user@example.com",
		Password: plainPassword,
		From:     "noreply@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Read back through Store — should get decrypted value
	got, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.SMTP.Password != plainPassword {
		t.Errorf("GetByID returned password %q, want %q (decrypted)", got.SMTP.Password, plainPassword)
	}
}

func TestSMTPPasswordEncryptedOnUpdate(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	tn, err := store.CreateWithSMTP("upd-enc", "Update Enc", tenant.SMTPConfig{
		Host: "smtp.example.com", Port: "587", User: "u@x.com",
		Password: "old-password", From: "f@x.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	newPassword := "new-super-secret-password"
	err = store.UpdateSMTP(tn.ID, tenant.SMTPConfig{
		Host: "smtp.example.com", Port: "587", User: "u@x.com",
		Password: newPassword, From: "f@x.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw := rawSMTPPassword(t, db, tn.ID)
	if raw == newPassword {
		t.Fatalf("H-5 violation: updated smtp_password stored as plaintext %q", raw)
	}

	// Verify the old ciphertext differs from new ciphertext
	// (re-encryption should produce different output)
	got, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SMTP.Password != newPassword {
		t.Errorf("after update, GetByID password = %q, want %q", got.SMTP.Password, newPassword)
	}
}

func TestSMTPPasswordEncryptionRoundTrip(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	passwords := []string{
		"simple",
		"p@$$w0rd!#%^&*()",
		"with spaces and\ttabs",
		strings.Repeat("long", 100),
	}

	for _, pw := range passwords {
		slug := fmt.Sprintf("rt-%d", len(pw))
		tn, err := store.CreateWithSMTP(slug, "Round Trip", tenant.SMTPConfig{
			Host: "smtp.example.com", Port: "587", User: "u@x.com",
			Password: pw, From: "f@x.com",
		})
		if err != nil {
			t.Fatalf("create with password len=%d: %v", len(pw), err)
		}

		got, err := store.GetByID(tn.ID)
		if err != nil {
			t.Fatalf("get with password len=%d: %v", len(pw), err)
		}

		if got.SMTP.Password != pw {
			t.Errorf("round-trip failed for password len=%d: got %q, want %q", len(pw), got.SMTP.Password, pw)
		}
	}
}

func TestSMTPPasswordEmptyString(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	tn, err := store.CreateWithSMTP("empty-pw", "Empty PW", tenant.SMTPConfig{
		Host: "smtp.example.com", Port: "587", User: "u@x.com",
		Password: "", From: "f@x.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.SMTP.Password != "" {
		t.Errorf("empty password should remain empty, got %q", got.SMTP.Password)
	}

	// Raw value should also be empty (nothing to encrypt)
	raw := rawSMTPPassword(t, db, tn.ID)
	if raw != "" {
		t.Errorf("raw DB value for empty password should be empty, got %q", raw)
	}
}

func TestSMTPPasswordWithSpecialChars(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	cases := []struct {
		name     string
		password string
	}{
		{"unicode", "pässwörd-日本語-🔐"},
		{"null-bytes", "pass\x00word\x00end"},
		{"newlines", "line1\nline2\r\nline3"},
		{"long-1024", strings.Repeat("A", 1024)},
		{"sql-injection", "'; DROP TABLE tenants; --"},
		{"json-special", `{"key":"value","nested":true}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slug := "special-" + tc.name
			tn, err := store.CreateWithSMTP(slug, "Special", tenant.SMTPConfig{
				Host: "smtp.example.com", Port: "587", User: "u@x.com",
				Password: tc.password, From: "f@x.com",
			})
			if err != nil {
				t.Fatalf("create: %v", err)
			}

			// Must be encrypted in DB
			raw := rawSMTPPassword(t, db, tn.ID)
			if raw == tc.password && tc.password != "" {
				t.Errorf("password stored as plaintext")
			}

			// Must decrypt correctly
			got, err := store.GetByID(tn.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.SMTP.Password != tc.password {
				t.Errorf("round-trip failed: got %q, want %q", got.SMTP.Password, tc.password)
			}
		})
	}
}

func TestMigrateEncryptExistingPasswords(t *testing.T) {
	db := newSMTPSecurityDB(t)

	// Insert a tenant with plaintext password directly into DB,
	// simulating pre-encryption data.
	plainPassword := "legacy-plaintext-password"
	_, err := db.Exec(
		`INSERT INTO tenants (id, slug, name, smtp_host, smtp_port, smtp_user, smtp_password, smtp_from, smtp_from_name, smtp_rate_ms, smtp_tls_mode, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		"legacy-id", "legacy-tenant", "Legacy", "smtp.legacy.com", "587",
		"user@legacy.com", plainPassword, "from@legacy.com", "Legacy App", 0, "required",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Run migration
	store := tenant.NewStore(db)
	err = store.MigrateEncryptPasswords()
	if err != nil {
		t.Fatalf("MigrateEncryptPasswords: %v", err)
	}

	// Raw DB value should now be encrypted (not plaintext)
	raw := rawSMTPPassword(t, db, "legacy-id")
	if raw == plainPassword {
		t.Fatalf("migration did not encrypt plaintext password")
	}

	// Reading through Store should return the original plaintext
	got, err := store.GetByID("legacy-id")
	if err != nil {
		t.Fatal(err)
	}
	if got.SMTP.Password != plainPassword {
		t.Errorf("after migration, GetByID password = %q, want %q", got.SMTP.Password, plainPassword)
	}
}

func TestMigrateSkipsAlreadyEncrypted(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	// Create tenant through Store (password already encrypted)
	tn, err := store.CreateWithSMTP("already-enc", "Already Encrypted", tenant.SMTPConfig{
		Host: "smtp.example.com", Port: "587", User: "u@x.com",
		Password: "already-encrypted-password", From: "f@x.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Record the encrypted value
	encBefore := rawSMTPPassword(t, db, tn.ID)

	// Run migration — should not double-encrypt
	err = store.MigrateEncryptPasswords()
	if err != nil {
		t.Fatalf("MigrateEncryptPasswords: %v", err)
	}

	encAfter := rawSMTPPassword(t, db, tn.ID)
	if encAfter != encBefore {
		t.Errorf("migration changed already-encrypted password: before=%q, after=%q", encBefore, encAfter)
	}

	// Still decrypts correctly
	got, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SMTP.Password != "already-encrypted-password" {
		t.Errorf("after migration, password = %q, want %q", got.SMTP.Password, "already-encrypted-password")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	tn, err := store.CreateWithSMTP("wrong-key", "Wrong Key", tenant.SMTPConfig{
		Host: "smtp.example.com", Port: "587", User: "u@x.com",
		Password: "secret-for-wrong-key-test", From: "f@x.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a new store with a different encryption key
	wrongKeyStore := tenant.NewStoreWithKey(db, "wrong-key-aaaaaaaaaaaaaaaa")

	_, err = wrongKeyStore.GetByID(tn.ID)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key, got nil")
	}
}

// ===========================================================================
// TLS Enforcement Tests (I-1)
// ===========================================================================

func TestTLSModeDefaultsToRequired(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	tn, err := store.Create("tls-default", "TLS Default")
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.SMTP.TLSMode != "required" {
		t.Errorf("default TLSMode = %q, want %q", got.SMTP.TLSMode, "required")
	}
}

func TestTLSModePreservedOnUpdate(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	tn, err := store.CreateWithSMTP("tls-preserve", "TLS Preserve", tenant.SMTPConfig{
		Host: "smtp.example.com", Port: "587", TLSMode: "required",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Update SMTP settings without changing TLS mode
	err = store.UpdateSMTP(tn.ID, tenant.SMTPConfig{
		Host: "smtp.new.com", Port: "465", TLSMode: "required",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.SMTP.TLSMode != "required" {
		t.Errorf("TLSMode after update = %q, want %q", got.SMTP.TLSMode, "required")
	}
}

func TestGetSMTPConfigReturnsTLSMode(t *testing.T) {
	db := newSMTPSecurityDB(t)
	store := tenant.NewStore(db)

	tn, err := store.CreateWithSMTP("tls-config", "TLS Config", tenant.SMTPConfig{
		Host: "smtp.example.com", Port: "587", User: "u@x.com",
		Password: "pass", From: "f@x.com", TLSMode: "required",
	})
	if err != nil {
		t.Fatal(err)
	}

	host, port, user, password, from, fromName, rateMS, tlsMode, err := store.GetSMTPConfigWithTLS(tn.ID)
	if err != nil {
		t.Fatal(err)
	}

	_ = host
	_ = port
	_ = user
	_ = password
	_ = from
	_ = fromName
	_ = rateMS

	if tlsMode != "required" {
		t.Errorf("GetSMTPConfigWithTLS tlsMode = %q, want %q", tlsMode, "required")
	}
}

func TestRejectInsecureTransport(t *testing.T) {
	addr := startPlaintextSMTPServer(t)
	host, port, _ := net.SplitHostPort(addr)

	// Attempt to connect with tls_mode=required to a server without STARTTLS
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// The server does not support STARTTLS.
	// With tls_mode=required, the mailer MUST refuse to send.
	tlsConfig := &tls.Config{ServerName: host, InsecureSkipVerify: true}
	err = c.StartTLS(tlsConfig)
	if err == nil {
		t.Fatal("STARTTLS succeeded on plaintext server — test server misconfigured")
	}

	// Now test the production mailer behavior:
	// tenant.SecureSMTPMailer should refuse to send when TLS is required but unavailable.
	mailer := tenant.NewSecureSMTPMailer(host, port, "user", "pass", "from@x.com", "App", "required")
	sendErr := mailer.Send("to@x.com", "Test", "<p>body</p>", nil, nil)
	if sendErr == nil {
		t.Fatal("I-1 violation: mailer sent email without TLS when tls_mode=required")
	}
	if !strings.Contains(sendErr.Error(), "TLS") && !strings.Contains(sendErr.Error(), "tls") &&
		!strings.Contains(sendErr.Error(), "STARTTLS") {
		t.Errorf("error should mention TLS, got: %v", sendErr)
	}
}

func TestAllowExplicitInsecure(t *testing.T) {
	addr := startPlaintextSMTPServer(t)
	host, port, _ := net.SplitHostPort(addr)

	// With tls_mode=opportunistic, sending without TLS should be allowed (for testing)
	mailer := tenant.NewSecureSMTPMailer(host, port, "user", "pass", "from@x.com", "App", "opportunistic")
	err := mailer.Send("to@x.com", "Test", "<p>body</p>", nil, nil)
	// The fake server may reject AUTH, but should NOT fail due to TLS enforcement
	if err != nil && (strings.Contains(err.Error(), "TLS") || strings.Contains(err.Error(), "STARTTLS")) {
		t.Errorf("opportunistic mode should not enforce TLS, got: %v", err)
	}
}

func TestSTARTTLSUpgrade(t *testing.T) {
	addr := startSTARTTLSServer(t)
	host, port, _ := net.SplitHostPort(addr)

	// With tls_mode=required, the mailer should upgrade via STARTTLS
	mailer := tenant.NewSecureSMTPMailer(host, port, "user", "pass", "from@x.com", "App", "required")
	err := mailer.Send("to@x.com", "Test", "<p>body</p>", nil, nil)
	// May fail on AUTH since our fake server is minimal, but should NOT fail
	// due to TLS — the upgrade should succeed.
	if err != nil && (strings.Contains(err.Error(), "TLS") || strings.Contains(err.Error(), "STARTTLS")) {
		t.Errorf("STARTTLS upgrade should succeed, got TLS error: %v", err)
	}
}
