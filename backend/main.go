package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/jenslaufer/mini-iam/iam"
	"github.com/jenslaufer/mini-iam/marketing"
	_ "modernc.org/sqlite"
)

func main() {
	port := envOr("PORT", "8080")
	issuer := envOr("ISSUER_URL", "http://localhost:8080")
	corsOrigins := envOr("CORS_ORIGINS", "*")
	adminEmail := os.Getenv("ADMIN_EMAIL")
	adminPassword := os.Getenv("ADMIN_PASSWORD")

	db, err := openDB("mini-iam.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	iamStore := iam.NewStore(db)
	marketingStore := marketing.NewStore(db)

	if adminEmail != "" && adminPassword != "" {
		if err := iamStore.SeedAdmin(adminEmail, adminPassword, "Admin"); err != nil {
			log.Fatalf("Failed to seed admin: %v", err)
		}
		log.Printf("Admin account seeded: %s", adminEmail)
	}

	// SMTP configuration
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := envOr("SMTP_PORT", "587")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPassword := os.Getenv("SMTP_PASSWORD")
	smtpFrom := os.Getenv("SMTP_FROM")
	smtpFromName := envOr("SMTP_FROM_NAME", "mini-iam")
	smtpRateMS, _ := strconv.Atoi(envOr("SMTP_RATE_MS", "100"))

	rsaKey, err := iamStore.LoadOrCreateRSAKey()
	if err != nil {
		log.Fatalf("Failed to load/create RSA key: %v", err)
	}

	tokenService := iam.NewTokenService(rsaKey, issuer)

	// Initialize mailer
	var mailer marketing.Mailer
	if smtpHost != "" {
		mailer = &marketing.SMTPMailer{
			Host:     smtpHost,
			Port:     smtpPort,
			User:     smtpUser,
			Password: smtpPassword,
			From:     smtpFrom,
			FromName: smtpFromName,
		}
		log.Printf("SMTP mailer configured: %s:%s", smtpHost, smtpPort)
	} else {
		mailer = &marketing.LogMailer{}
		log.Println("No SMTP_HOST configured, using log-only mailer")
	}

	// Start campaign sender worker
	sender := marketing.NewCampaignSender(marketingStore, mailer, issuer, smtpRateMS)
	sender.Start()

	iamHandler := iam.NewHandler(iamStore, tokenService, issuer)
	marketingHandler := marketing.NewHandler(marketingStore, iamStore, tokenService)
	marketingHandler.SetSender(sender)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", iamHandler.Health)
	mux.HandleFunc("/register", iamHandler.Register)
	mux.HandleFunc("/login", iamHandler.Login)
	mux.HandleFunc("/authorize", iamHandler.Authorize)
	mux.HandleFunc("/token", iamHandler.Token)
	mux.HandleFunc("/userinfo", iamHandler.UserInfo)
	mux.HandleFunc("/jwks", iamHandler.JWKS)
	mux.HandleFunc("/.well-known/openid-configuration", iamHandler.Discovery)
	mux.HandleFunc("/revoke", iamHandler.Revoke)
	mux.HandleFunc("/clients", iamHandler.CreateClient)
	mux.HandleFunc("/admin/users", iamHandler.AdminListUsers)
	mux.HandleFunc("/admin/users/", iamHandler.AdminUserByID)
	mux.HandleFunc("/admin/clients", iamHandler.AdminListClients)
	mux.HandleFunc("/admin/clients/", iamHandler.AdminDeleteClient)

	// Marketing routes (admin-protected)
	mux.HandleFunc("/admin/contacts/import", marketingHandler.AdminImportContacts)
	mux.HandleFunc("/admin/contacts", marketingHandler.AdminContacts)
	mux.HandleFunc("/admin/contacts/", marketingHandler.AdminContactByID)
	mux.HandleFunc("/admin/segments", marketingHandler.AdminSegments)
	mux.HandleFunc("/admin/segments/", marketingHandler.AdminSegmentByID)
	mux.HandleFunc("/admin/campaigns", marketingHandler.AdminCampaigns)
	mux.HandleFunc("/admin/campaigns/", marketingHandler.AdminCampaignByID)

	// Public endpoints (no auth)
	mux.HandleFunc("/activate/", iamHandler.Activate)
	mux.HandleFunc("/track/", marketingHandler.TrackOpen)
	mux.HandleFunc("/unsubscribe/", marketingHandler.Unsubscribe)

	handler := CORSMiddleware(corsOrigins)(mux)

	log.Printf("mini-iam starting on :%s (issuer: %s)", port, issuer)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func openDB(dbPath string) (*sql.DB, error) {
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

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
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
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Add role column if missing (existing databases)
	db.Exec("ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'")

	// Add invite_token column if missing (existing databases)
	db.Exec("ALTER TABLE contacts ADD COLUMN invite_token TEXT UNIQUE")

	return nil
}
