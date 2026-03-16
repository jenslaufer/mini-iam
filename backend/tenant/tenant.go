package tenant

import (
	"context"
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
}

type Tenant struct {
	ID        string     `json:"id"`
	Slug      string     `json:"slug"`
	Name      string     `json:"name"`
	SMTP      SMTPConfig `json:"smtp,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func WithID(ctx context.Context, id string) context.Context {
	return tenantctx.WithID(ctx, id)
}

func FromContext(ctx context.Context) string {
	return tenantctx.FromContext(ctx)
}

func WithSlug(ctx context.Context, slug string) context.Context {
	return tenantctx.WithSlug(ctx, slug)
}

func SlugFromContext(ctx context.Context) string {
	return tenantctx.SlugFromContext(ctx)
}

// Store manages tenant CRUD operations.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(slug, name string) (*Tenant, error) {
	return s.CreateWithSMTP(slug, name, SMTPConfig{})
}

func (s *Store) CreateWithSMTP(slug, name string, smtp SMTPConfig) (*Tenant, error) {
	t := &Tenant{
		ID:        uuid.NewString(),
		Slug:      slug,
		Name:      name,
		SMTP:      smtp,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.Exec(
		`INSERT INTO tenants (id, slug, name, smtp_host, smtp_port, smtp_user, smtp_password, smtp_from, smtp_from_name, smtp_rate_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Slug, t.Name, t.SMTP.Host, t.SMTP.Port, t.SMTP.User, t.SMTP.Password, t.SMTP.From, t.SMTP.FromName, t.SMTP.RateMS, t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

const tenantCols = `id, slug, name, smtp_host, smtp_port, smtp_user, smtp_password, smtp_from, smtp_from_name, smtp_rate_ms, created_at`

func scanTenant(row interface{ Scan(...interface{}) error }) (*Tenant, error) {
	t := &Tenant{}
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.SMTP.Host, &t.SMTP.Port, &t.SMTP.User, &t.SMTP.Password, &t.SMTP.From, &t.SMTP.FromName, &t.SMTP.RateMS, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) GetBySlug(slug string) (*Tenant, error) {
	return scanTenant(s.db.QueryRow("SELECT "+tenantCols+" FROM tenants WHERE slug = ?", slug))
}

func (s *Store) GetByID(id string) (*Tenant, error) {
	return scanTenant(s.db.QueryRow("SELECT "+tenantCols+" FROM tenants WHERE id = ?", id))
}

func (s *Store) List() ([]Tenant, error) {
	rows, err := s.db.Query("SELECT " + tenantCols + " FROM tenants ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tenants []Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, *t)
	}
	return tenants, rows.Err()
}

func (s *Store) UpdateSMTP(id string, smtp SMTPConfig) error {
	result, err := s.db.Exec(
		`UPDATE tenants SET smtp_host=?, smtp_port=?, smtp_user=?, smtp_password=?, smtp_from=?, smtp_from_name=?, smtp_rate_ms=? WHERE id=?`,
		smtp.Host, smtp.Port, smtp.User, smtp.Password, smtp.From, smtp.FromName, smtp.RateMS, id,
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

func (s *Store) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM tenants WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tenant not found")
	}
	return nil
}

// GetSMTPConfig returns SMTP settings for a tenant. Implements marketing.TenantSMTPProvider.
func (s *Store) GetSMTPConfig(tenantID string) (host, port, user, password, from, fromName string, rateMS int, err error) {
	t, err := s.GetByID(tenantID)
	if err != nil {
		return "", "", "", "", "", "", 0, err
	}
	return t.SMTP.Host, t.SMTP.Port, t.SMTP.User, t.SMTP.Password, t.SMTP.From, t.SMTP.FromName, t.SMTP.RateMS, nil
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
					ctx := WithSlug(WithID(r2.Context(), t.ID), slug)
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

			ctx := WithSlug(WithID(r.Context(), tenantID), slug)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
