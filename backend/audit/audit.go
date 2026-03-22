package audit

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Entry represents a single audit log record.
type Entry struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenant_id"`
	ActorID    string          `json:"actor_id"`
	ActorType  string          `json:"actor_type"` // "user" or "service"
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Details    json.RawMessage `json:"details,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
}

// Store provides CRUD operations for audit log entries.
type Store struct {
	db       *sql.DB
	tenantID string
}

// NewStore creates a new audit store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// ForTenant returns a copy scoped to the given tenant.
func (s *Store) ForTenant(tenantID string) *Store {
	return &Store{db: s.db, tenantID: tenantID}
}

// Record inserts a new audit log entry.
func (s *Store) Record(actorID, actorType, action, targetType, targetID string, details interface{}) error {
	id := uuid.NewString()
	now := time.Now().UTC()

	var detailsJSON []byte
	if details != nil {
		var err error
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			return err
		}
	}

	_, err := s.db.Exec(
		`INSERT INTO audit_log (id, tenant_id, actor_id, actor_type, action, target_type, target_id, details, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, s.tenantID, actorID, actorType, action, targetType, targetID, string(detailsJSON), now,
	)
	return err
}

// Filter defines query parameters for listing audit entries.
type Filter struct {
	Action     string
	ActorID    string
	TargetType string
	TargetID   string
	Since      *time.Time
	Until      *time.Time
}

// List returns audit entries matching the filter, ordered by timestamp desc.
func (s *Store) List(f Filter) ([]Entry, error) {
	query := `SELECT id, tenant_id, actor_id, actor_type, action, target_type, target_id, details, timestamp
	          FROM audit_log WHERE tenant_id = ?`
	args := []interface{}{s.tenantID}

	if f.Action != "" {
		query += " AND action = ?"
		args = append(args, f.Action)
	}
	if f.ActorID != "" {
		query += " AND actor_id = ?"
		args = append(args, f.ActorID)
	}
	if f.TargetType != "" {
		query += " AND target_type = ?"
		args = append(args, f.TargetType)
	}
	if f.TargetID != "" {
		query += " AND target_id = ?"
		args = append(args, f.TargetID)
	}
	if f.Since != nil {
		query += " AND timestamp >= ?"
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		query += " AND timestamp <= ?"
		args = append(args, *f.Until)
	}

	query += " ORDER BY timestamp DESC LIMIT 1000"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var details sql.NullString
		if err := rows.Scan(&e.ID, &e.TenantID, &e.ActorID, &e.ActorType, &e.Action,
			&e.TargetType, &e.TargetID, &details, &e.Timestamp); err != nil {
			return nil, err
		}
		if details.Valid && details.String != "" {
			e.Details = json.RawMessage(details.String)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
