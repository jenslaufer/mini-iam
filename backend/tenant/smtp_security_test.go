package tenant_test

import (
	"database/sql"
	"testing"

	"github.com/jenslaufer/launch-kit/tenant"
)

// queryRawPassword reads the smtp_password column directly from the DB (bypasses decryption).
func queryRawPassword(t *testing.T, db *sql.DB, tenantID string) string {
	t.Helper()
	var raw string
	err := db.QueryRow("SELECT smtp_password FROM tenants WHERE id = ?", tenantID).Scan(&raw)
	if err != nil {
		t.Fatalf("query raw password: %v", err)
	}
	return raw
}

func TestSMTPPasswordStoredEncrypted(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)
	key := tenant.DeriveKey("test-encryption-key")
	store.SetEncryptionKey(key)

	plainPassword := "super-secret-password"
	tn, err := store.CreateWithSMTP("enc-test", "Enc Test", tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587", User: "user@test.com",
		Password: plainPassword, From: "from@test.com",
	})
	if err != nil {
		t.Fatalf("CreateWithSMTP: %v", err)
	}

	raw := queryRawPassword(t, db, tn.ID)
	if raw == plainPassword {
		t.Fatal("password stored in plaintext — expected encrypted")
	}
	if !tenant.IsEncrypted(raw) {
		t.Fatalf("stored value does not look encrypted: %q", raw)
	}
}

func TestSMTPPasswordDecryptsOnRead(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)
	key := tenant.DeriveKey("test-encryption-key")
	store.SetEncryptionKey(key)

	plainPassword := "my-smtp-pass"
	tn, err := store.CreateWithSMTP("dec-test", "Dec Test", tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587", User: "user@test.com",
		Password: plainPassword, From: "from@test.com",
	})
	if err != nil {
		t.Fatalf("CreateWithSMTP: %v", err)
	}

	fetched, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.SMTP.Password != plainPassword {
		t.Fatalf("decrypted password = %q, want %q", fetched.SMTP.Password, plainPassword)
	}
}

func TestSMTPPasswordEncryptedOnUpdate(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)
	key := tenant.DeriveKey("test-encryption-key")
	store.SetEncryptionKey(key)

	tn, err := store.Create("upd-enc", "Upd Enc")
	if err != nil {
		t.Fatal(err)
	}

	newPass := "updated-secret"
	if err := store.UpdateSMTP(tn.ID, tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587", User: "user@test.com",
		Password: newPass, From: "from@test.com",
	}); err != nil {
		t.Fatal(err)
	}

	raw := queryRawPassword(t, db, tn.ID)
	if raw == newPass {
		t.Fatal("updated password stored in plaintext")
	}

	fetched, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.SMTP.Password != newPass {
		t.Fatalf("decrypted = %q, want %q", fetched.SMTP.Password, newPass)
	}
}

func TestMigrateEncryptPasswords(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)

	// Create tenant with plaintext password (no encryption key yet)
	tn, err := store.CreateWithSMTP("migrate-test", "Migrate Test", tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587", User: "user@test.com",
		Password: "plaintext-pass", From: "from@test.com", FromName: "Test",
	})
	if err != nil {
		t.Fatalf("CreateWithSMTP: %v", err)
	}

	// Verify it's plaintext in DB
	raw := queryRawPassword(t, db, tn.ID)
	if raw != "plaintext-pass" {
		t.Fatalf("expected plaintext, got %q", raw)
	}

	// Now enable encryption and migrate
	key := tenant.DeriveKey("migration-key")
	store.SetEncryptionKey(key)

	if err := store.MigrateEncryptPasswords(); err != nil {
		t.Fatalf("MigrateEncryptPasswords: %v", err)
	}

	// Raw value should now be encrypted
	raw = queryRawPassword(t, db, tn.ID)
	if raw == "plaintext-pass" {
		t.Fatal("password still plaintext after migration")
	}
	if !tenant.IsEncrypted(raw) {
		t.Fatalf("migrated value does not look encrypted: %q", raw)
	}

	// Reading through store should decrypt
	fetched, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.SMTP.Password != "plaintext-pass" {
		t.Fatalf("decrypted = %q, want %q", fetched.SMTP.Password, "plaintext-pass")
	}
}

func TestMigrateSkipsAlreadyEncrypted(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)
	key := tenant.DeriveKey("skip-key")
	store.SetEncryptionKey(key)

	// Create with encryption
	tn, err := store.CreateWithSMTP("skip-test", "Skip Test", tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587", Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	rawBefore := queryRawPassword(t, db, tn.ID)

	// Run migration again — should skip already encrypted
	if err := store.MigrateEncryptPasswords(); err != nil {
		t.Fatal(err)
	}

	rawAfter := queryRawPassword(t, db, tn.ID)
	if rawBefore != rawAfter {
		t.Fatal("migration re-encrypted an already encrypted password")
	}
}

func TestTLSModeDefaultsToRequired(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)

	tn, err := store.CreateWithSMTP("tls-default", "TLS Default", tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587",
	})
	if err != nil {
		t.Fatal(err)
	}

	fetched, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.SMTP.TLSMode != "required" {
		t.Fatalf("TLSMode = %q, want %q", fetched.SMTP.TLSMode, "required")
	}
}

func TestTLSModePreservedOnUpdate(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)

	tn, err := store.Create("tls-upd", "TLS Update")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateSMTP(tn.ID, tenant.SMTPConfig{
		Host: "mail.test.com", TLSMode: "opportunistic",
	}); err != nil {
		t.Fatal(err)
	}

	fetched, err := store.GetByID(tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.SMTP.TLSMode != "opportunistic" {
		t.Fatalf("TLSMode = %q, want %q", fetched.SMTP.TLSMode, "opportunistic")
	}
}

func TestGetSMTPConfigReturnsTLSMode(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db)

	tn, err := store.CreateWithSMTP("tls-config", "TLS Config", tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587", TLSMode: "none",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, _, _, _, _, tlsMode, err := store.GetSMTPConfig(tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tlsMode != "none" {
		t.Fatalf("tlsMode = %q, want %q", tlsMode, "none")
	}
}

func TestNoEncryptionKeyStoresPlaintext(t *testing.T) {
	db := newRoutingDB(t)
	store := tenant.NewStore(db) // no encryption key

	tn, err := store.CreateWithSMTP("no-enc", "No Enc", tenant.SMTPConfig{
		Host: "mail.test.com", Port: "587", Password: "plain-pass",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw := queryRawPassword(t, db, tn.ID)
	if raw != "plain-pass" {
		t.Fatalf("without encryption key, password should be plaintext, got %q", raw)
	}
}
