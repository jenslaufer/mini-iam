package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
)

func main() {
	port := envOr("PORT", "8080")
	issuer := envOr("ISSUER_URL", "http://localhost:8080")
	corsOrigins := envOr("CORS_ORIGINS", "*")
	adminEmail := os.Getenv("ADMIN_EMAIL")
	adminPassword := os.Getenv("ADMIN_PASSWORD")

	store, err := NewStore("mini-iam.db")
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer store.Close()

	if adminEmail != "" && adminPassword != "" {
		if err := store.SeedAdmin(adminEmail, adminPassword, "Admin"); err != nil {
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

	rsaKey, err := store.LoadOrCreateRSAKey()
	if err != nil {
		log.Fatalf("Failed to load/create RSA key: %v", err)
	}

	tokenService := NewTokenService(rsaKey, issuer)

	// Initialize mailer
	var mailer Mailer
	if smtpHost != "" {
		mailer = &SMTPMailer{
			Host:     smtpHost,
			Port:     smtpPort,
			User:     smtpUser,
			Password: smtpPassword,
			From:     smtpFrom,
			FromName: smtpFromName,
		}
		log.Printf("SMTP mailer configured: %s:%s", smtpHost, smtpPort)
	} else {
		mailer = &LogMailer{}
		log.Println("No SMTP_HOST configured, using log-only mailer")
	}

	// Start campaign sender worker
	sender := NewCampaignSender(store, mailer, issuer, smtpRateMS)
	sender.Start()

	h := NewHandler(store, tokenService, issuer)
	h.sender = sender

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/register", h.Register)
	mux.HandleFunc("/login", h.Login)
	mux.HandleFunc("/authorize", h.Authorize)
	mux.HandleFunc("/token", h.Token)
	mux.HandleFunc("/userinfo", h.UserInfo)
	mux.HandleFunc("/jwks", h.JWKS)
	mux.HandleFunc("/.well-known/openid-configuration", h.Discovery)
	mux.HandleFunc("/revoke", h.Revoke)
	mux.HandleFunc("/clients", h.CreateClient)
	mux.HandleFunc("/admin/users", h.AdminListUsers)
	mux.HandleFunc("/admin/users/", h.AdminUserByID)
	mux.HandleFunc("/admin/clients", h.AdminListClients)
	mux.HandleFunc("/admin/clients/", h.AdminDeleteClient)

	// Marketing routes (admin-protected)
	mux.HandleFunc("/admin/contacts/import", h.AdminImportContacts)
	mux.HandleFunc("/admin/contacts", h.AdminContacts)
	mux.HandleFunc("/admin/contacts/", h.AdminContactByID)
	mux.HandleFunc("/admin/segments", h.AdminSegments)
	mux.HandleFunc("/admin/segments/", h.AdminSegmentByID)
	mux.HandleFunc("/admin/campaigns", h.AdminCampaigns)
	mux.HandleFunc("/admin/campaigns/", h.AdminCampaignByID)

	// Public tracking/unsubscribe endpoints (no auth)
	mux.HandleFunc("/track/", h.TrackOpen)
	mux.HandleFunc("/unsubscribe/", h.Unsubscribe)

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
