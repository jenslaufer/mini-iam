// Package audit provides audit logging for security-relevant operations.
// STUB: minimal types and interfaces for test compilation (red phase TDD).
package audit

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// Event represents a single audit log entry.
type Event struct {
	ID        string          `json:"id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Details   json.RawMessage `json:"details,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Filter controls which events are returned by ListEvents.
type Filter struct {
	Action    string
	Actor     string
	Target    string
	StartTime *time.Time
	EndTime   *time.Time
	Offset    int
	Limit     int
}

// Store persists and queries audit events.
type Store struct {
	db *sql.DB
}

// NewStore creates an audit store backed by the given database.
// STUB: does not create schema or implement any operations yet.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// RecordEvent persists an audit event.
// STUB: not implemented.
func (s *Store) RecordEvent(actor, action, target string, details json.RawMessage) (*Event, error) {
	return nil, nil
}

// ListEvents returns audit events matching the filter.
// STUB: not implemented.
func (s *Store) ListEvents(f Filter) ([]Event, error) {
	return nil, nil
}

// HandleListEvents is an HTTP handler that returns audit events as JSON.
// STUB: not implemented.
func (s *Store) HandleListEvents(w http.ResponseWriter, r *http.Request) {
}
