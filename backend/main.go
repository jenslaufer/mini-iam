package main

import (
	"log"
	"net/http"
	"os"
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

	rsaKey, err := store.LoadOrCreateRSAKey()
	if err != nil {
		log.Fatalf("Failed to load/create RSA key: %v", err)
	}

	tokenService := NewTokenService(rsaKey, issuer)
	h := NewHandler(store, tokenService, issuer)

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
