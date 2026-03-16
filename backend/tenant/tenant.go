package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Tenant struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type contextKey struct{}

func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// Store manages tenant CRUD operations.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(slug, name string) (*Tenant, error) {
	t := &Tenant{
		ID:        uuid.NewString(),
		Slug:      slug,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.Exec(
		"INSERT INTO tenants (id, slug, name, created_at) VALUES (?, ?, ?, ?)",
		t.ID, t.Slug, t.Name, t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) GetBySlug(slug string) (*Tenant, error) {
	t := &Tenant{}
	err := s.db.QueryRow(
		"SELECT id, slug, name, created_at FROM tenants WHERE slug = ?", slug,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) GetByID(id string) (*Tenant, error) {
	t := &Tenant{}
	err := s.db.QueryRow(
		"SELECT id, slug, name, created_at FROM tenants WHERE id = ?", id,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) List() ([]Tenant, error) {
	rows, err := s.db.Query("SELECT id, slug, name, created_at FROM tenants ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tenants []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
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

// Middleware resolves the tenant from the X-Tenant header and stores its ID in
// the request context. If no header is present, the default tenant ID is used.
// Returns 404 if the header is set but the tenant slug is unknown.
func Middleware(store *Store, defaultTenantID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slug := r.Header.Get("X-Tenant")
			if slug == "" {
				// Try extracting from subdomain: "acme.example.com" → "acme"
				host := r.Host
				if idx := strings.Index(host, ":"); idx != -1 {
					host = host[:idx]
				}
				parts := strings.SplitN(host, ".", 3)
				if len(parts) >= 3 {
					slug = parts[0]
				}
			}

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

			ctx := WithID(r.Context(), tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
