package tenant

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jenslaufer/launch-kit/tenantctx"
)

type SMTPConfig struct {
	Host     string `json:"smtp_host,omitempty"`
	Port     string `json:"smtp_port,omitempty"`
	User     string `json:"smtp_user,omitempty"`
	Password string `json:"smtp_password,omitempty"`
	From     string `json:"smtp_from,omitempty"`
	FromName string `json:"smtp_from_name,omitempty"`
	RateMS   int    `json:"smtp_rate_ms,omitempty"`
	TLSMode  string `json:"smtp_tls_mode,omitempty"`
}

type Tenant struct {
	ID                  string     `json:"id"`
	Slug                string     `json:"slug"`
	Name                string     `json:"name"`
	RegistrationEnabled bool       `json:"registration_enabled"`
	SMTP                SMTPConfig `json:"smtp,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}


// Store manages tenant CRUD operations.
type Store struct {
	db            *sql.DB
	encryptionKey *[32]byte // nil = no encryption
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetEncryptionKey enables AES-256-GCM encryption for SMTP passwords.
func (s *Store) SetEncryptionKey(key [32]byte) {
	s.encryptionKey = &key
}

func (s *Store) encryptPassword(plaintext string) (string, error) {
	if s.encryptionKey == nil || plaintext == "" {
		return plaintext, nil
	}
	return Encrypt(plaintext, *s.encryptionKey)
}

func (s *Store) decryptPassword(stored string) (string, error) {
	if s.encryptionKey == nil || stored == "" {
		return stored, nil
	}
	if !IsEncrypted(stored) {
		return stored, nil // plaintext (pre-migration)
	}
	return Decrypt(stored, *s.encryptionKey)
}

func (s *Store) Create(slug, name string) (*Tenant, error) {
	return s.CreateWithSMTP(slug, name, SMTPConfig{})
}

func (s *Store) CreateWithSMTP(slug, name string, smtp SMTPConfig) (*Tenant, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	if smtp.TLSMode == "" && smtp.Host != "" {
		smtp.TLSMode = "required"
	}
	encPass, err := s.encryptPassword(smtp.Password)
	if err != nil {
		return nil, fmt.Errorf("encrypt smtp password: %w", err)
	}
	t := &Tenant{
		ID:        uuid.NewString(),
		Slug:      slug,
		Name:      name,
		SMTP:      smtp,
		CreatedAt: time.Now().UTC(),
	}
	_, err = s.db.Exec(
		`INSERT INTO tenants (id, slug, name, smtp_host, smtp_port, smtp_user, smtp_password, smtp_from, smtp_from_name, smtp_rate_ms, smtp_tls_mode, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Slug, t.Name, t.SMTP.Host, t.SMTP.Port, t.SMTP.User, encPass, t.SMTP.From, t.SMTP.FromName, t.SMTP.RateMS, t.SMTP.TLSMode, t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

const tenantCols = `id, slug, name, registration_enabled, smtp_host, smtp_port, smtp_user, smtp_password, smtp_from, smtp_from_name, smtp_rate_ms, smtp_tls_mode, created_at`

func (s *Store) scanTenant(row interface{ Scan(...interface{}) error }) (*Tenant, error) {
	t := &Tenant{}
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.RegistrationEnabled, &t.SMTP.Host, &t.SMTP.Port, &t.SMTP.User, &t.SMTP.Password, &t.SMTP.From, &t.SMTP.FromName, &t.SMTP.RateMS, &t.SMTP.TLSMode, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	// Decrypt SMTP password on read
	if t.SMTP.Password != "" {
		dec, err := s.decryptPassword(t.SMTP.Password)
		if err != nil {
			return nil, fmt.Errorf("decrypt smtp password: %w", err)
		}
		t.SMTP.Password = dec
	}
	return t, nil
}

func (s *Store) UpdateName(id string, name string) error {
	result, err := s.db.Exec("UPDATE tenants SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tenant not found")
	}
	return nil
}

func (s *Store) UpdateRegistrationEnabled(id string, enabled bool) error {
	_, err := s.db.Exec("UPDATE tenants SET registration_enabled = ? WHERE id = ?", enabled, id)
	return err
}

// IsRegistrationEnabled implements iam.RegistrationPolicy.
func (s *Store) IsRegistrationEnabled(tenantID string) bool {
	var enabled bool
	err := s.db.QueryRow("SELECT registration_enabled FROM tenants WHERE id = ?", tenantID).Scan(&enabled)
	if err != nil {
		return false // deny on error
	}
	return enabled
}

func (s *Store) GetBySlug(slug string) (*Tenant, error) {
	return s.scanTenant(s.db.QueryRow("SELECT "+tenantCols+" FROM tenants WHERE slug = ?", slug))
}

func (s *Store) GetByID(id string) (*Tenant, error) {
	return s.scanTenant(s.db.QueryRow("SELECT "+tenantCols+" FROM tenants WHERE id = ?", id))
}

func (s *Store) List() ([]Tenant, error) {
	rows, err := s.db.Query("SELECT " + tenantCols + " FROM tenants ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tenants []Tenant
	for rows.Next() {
		t, err := s.scanTenant(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, *t)
	}
	return tenants, rows.Err()
}

func (s *Store) UpdateSMTP(id string, smtp SMTPConfig) error {
	if smtp.TLSMode == "" {
		smtp.TLSMode = "required"
	}
	encPass, err := s.encryptPassword(smtp.Password)
	if err != nil {
		return fmt.Errorf("encrypt smtp password: %w", err)
	}
	result, err := s.db.Exec(
		`UPDATE tenants SET smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, smtp_from=?, smtp_from_name=?, smtp_rate_ms=?, smtp_tls_mode=? WHERE id=?`,
		smtp.Host, smtp.Port, smtp.User, encPass, smtp.From, smtp.FromName, smtp.RateMS, smtp.TLSMode, id,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tenant not found")
	}
	return nil
}

// MigrateEncryptPasswords encrypts any plaintext SMTP passwords in the database.
func (s *Store) MigrateEncryptPasswords() error {
	if s.encryptionKey == nil {
		return nil
	}
	// Collect all plaintext passwords first, then update in a separate step.
	// This avoids holding a query cursor open while executing updates, which
	// can cause issues with SQLite in-memory databases using connection pools.
	rows, err := s.db.Query("SELECT id, smtp_password FROM tenants WHERE smtp_password != ''")
	if err != nil {
		return err
	}
	type entry struct{ id, password string }
	var toMigrate []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.password); err != nil {
			rows.Close()
			return err
		}
		if !IsEncrypted(e.password) {
			toMigrate = append(toMigrate, e)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, e := range toMigrate {
		enc, err := Encrypt(e.password, *s.encryptionKey)
		if err != nil {
			return fmt.Errorf("encrypt password for tenant %s: %w", e.id, err)
		}
		if _, err := s.db.Exec("UPDATE tenants SET smtp_password = ? WHERE id = ?", enc, e.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete all tenant-scoped tables. Junction tables (campaign_segments,
	// contact_segments, campaign_recipients) have no tenant_id and are
	// removed automatically via ON DELETE CASCADE when their parent rows go.
	for _, table := range []string{
		"campaigns", "contacts", "segments",
		"auth_codes", "refresh_tokens", "keys", "clients", "users",
	} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE tenant_id = ?", id); err != nil {
			return err
		}
	}

	result, err := tx.Exec("DELETE FROM tenants WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tenant not found")
	}
	return tx.Commit()
}

// GetTenantSlug returns the slug for a tenant. Implements marketing.TenantProvider.
func (s *Store) GetTenantSlug(tenantID string) (string, error) {
	t, err := s.GetByID(tenantID)
	if err != nil {
		return "", err
	}
	return t.Slug, nil
}

// GetSMTPConfig returns SMTP settings for a tenant. Implements marketing.TenantProvider.
func (s *Store) GetSMTPConfig(tenantID string) (host, port, user, password, from, fromName string, rateMS int, tlsMode string, err error) {
	t, err := s.GetByID(tenantID)
	if err != nil {
		return "", "", "", "", "", "", 0, "", err
	}
	return t.SMTP.Host, t.SMTP.Port, t.SMTP.User, t.SMTP.Password, t.SMTP.From, t.SMTP.FromName, t.SMTP.RateMS, t.SMTP.TLSMode, nil
}

// Middleware resolves the tenant from either a path prefix (/t/{slug}/...) or
// the X-Tenant header. Path prefix takes precedence. The prefix is stripped
// before the request reaches downstream handlers. Falls back to the default
// tenant ID when neither source provides a slug.
func Middleware(store *Store, defaultTenantID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Try path prefix: /t/{slug}/...
			if strings.HasPrefix(r.URL.Path, "/t/") {
				rest := strings.TrimPrefix(r.URL.Path, "/t/")
				if idx := strings.Index(rest, "/"); idx > 0 {
					slug := rest[:idx]
					t, err := store.GetBySlug(slug)
					if err != nil {
						http.Error(w, `{"error":"unknown_tenant","error_description":"tenant not found"}`, http.StatusNotFound)
						return
					}
					r2 := r.Clone(r.Context())
					r2.URL.Path = rest[idx:]
					r2.URL.RawPath = ""
					ctx := tenantctx.WithSlug(tenantctx.WithID(r2.Context(), t.ID), slug)
					next.ServeHTTP(w, r2.WithContext(ctx))
					return
				}
			}

			// 2. Try X-Tenant header
			slug := r.Header.Get("X-Tenant")
			var tenantID string
			if slug != "" {
				t, err := store.GetBySlug(slug)
				if err != nil {
					http.Error(w, `{"error":"unknown_tenant","error_description":"tenant not found"}`, http.StatusNotFound)
					return
				}
				tenantID = t.ID
			} else {
				tenantID = defaultTenantID
			}

			ctx := tenantctx.WithSlug(tenantctx.WithID(r.Context(), tenantID), slug)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
