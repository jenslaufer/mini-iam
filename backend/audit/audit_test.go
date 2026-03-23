package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// --- Test helpers ---

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	PRAGMA foreign_keys = ON;
	CREATE TABLE audit_events (
		id TEXT PRIMARY KEY,
		actor TEXT NOT NULL,
		action TEXT NOT NULL,
		target TEXT NOT NULL DEFAULT '',
		details TEXT,
		timestamp DATETIME NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(newTestDB(t))
}

// --- Core Audit Log ---

func TestRecordEvent(t *testing.T) {
	s := newTestStore(t)

	ev, err := s.RecordEvent("user-1", "role.change", "user-2", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.ID == "" {
		t.Error("event ID is empty")
	}
	if ev.Actor != "user-1" {
		t.Errorf("actor = %q, want user-1", ev.Actor)
	}
	if ev.Action != "role.change" {
		t.Errorf("action = %q, want role.change", ev.Action)
	}
	if ev.Target != "user-2" {
		t.Errorf("target = %q, want user-2", ev.Target)
	}
	if ev.Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}
	if time.Since(ev.Timestamp) > 5*time.Second {
		t.Error("timestamp too far in the past")
	}
}

func TestRecordEventWithDetails(t *testing.T) {
	s := newTestStore(t)

	details := json.RawMessage(`{"old_role":"user","new_role":"admin"}`)
	ev, err := s.RecordEvent("admin-1", "role.change", "user-5", details)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Details == nil {
		t.Fatal("details is nil")
	}

	var parsed map[string]string
	if err := json.Unmarshal(ev.Details, &parsed); err != nil {
		t.Fatalf("cannot parse details: %v", err)
	}
	if parsed["old_role"] != "user" {
		t.Errorf("old_role = %q", parsed["old_role"])
	}
	if parsed["new_role"] != "admin" {
		t.Errorf("new_role = %q", parsed["new_role"])
	}
}

func TestListEvents(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		_, err := s.RecordEvent("actor", fmt.Sprintf("action.%d", i), "target", nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	events, err := s.ListEvents(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Errorf("got %d events, want 5", len(events))
	}
}

func TestListEventsDescending(t *testing.T) {
	s := newTestStore(t)

	s.RecordEvent("a", "first", "", nil)
	time.Sleep(10 * time.Millisecond)
	s.RecordEvent("a", "second", "", nil)
	time.Sleep(10 * time.Millisecond)
	s.RecordEvent("a", "third", "", nil)

	events, err := s.ListEvents(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Action != "third" {
		t.Errorf("first event action = %q, want third (most recent)", events[0].Action)
	}
	if events[2].Action != "first" {
		t.Errorf("last event action = %q, want first (oldest)", events[2].Action)
	}
}

func TestListEventsPagination(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 10; i++ {
		s.RecordEvent("a", fmt.Sprintf("action.%d", i), "", nil)
	}

	page1, err := s.ListEvents(Filter{Offset: 0, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 3 {
		t.Errorf("page1 len = %d, want 3", len(page1))
	}

	page2, err := s.ListEvents(Filter{Offset: 3, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 3 {
		t.Errorf("page2 len = %d, want 3", len(page2))
	}

	if len(page1) > 0 && len(page2) > 0 && page1[0].ID == page2[0].ID {
		t.Error("page1 and page2 have same first event")
	}
}

func TestListEventsFilterByAction(t *testing.T) {
	s := newTestStore(t)

	s.RecordEvent("a", "role.change", "", nil)
	s.RecordEvent("a", "client.create", "", nil)
	s.RecordEvent("a", "role.change", "", nil)

	events, err := s.ListEvents(Filter{Action: "role.change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
	for _, ev := range events {
		if ev.Action != "role.change" {
			t.Errorf("unexpected action %q", ev.Action)
		}
	}
}

func TestListEventsFilterByActor(t *testing.T) {
	s := newTestStore(t)

	s.RecordEvent("admin-1", "role.change", "", nil)
	s.RecordEvent("admin-2", "role.change", "", nil)
	s.RecordEvent("admin-1", "client.create", "", nil)

	events, err := s.ListEvents(Filter{Actor: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
	for _, ev := range events {
		if ev.Actor != "admin-1" {
			t.Errorf("unexpected actor %q", ev.Actor)
		}
	}
}

func TestListEventsFilterByTarget(t *testing.T) {
	s := newTestStore(t)

	s.RecordEvent("a", "role.change", "user-1", nil)
	s.RecordEvent("a", "role.change", "user-2", nil)
	s.RecordEvent("a", "role.change", "user-1", nil)

	events, err := s.ListEvents(Filter{Target: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
}

func TestListEventsFilterByDateRange(t *testing.T) {
	s := newTestStore(t)

	before := time.Now().Add(-1 * time.Hour)
	s.RecordEvent("a", "old.action", "", nil)
	time.Sleep(10 * time.Millisecond)
	start := time.Now()
	s.RecordEvent("a", "recent.action", "", nil)
	time.Sleep(10 * time.Millisecond)
	end := time.Now()

	_ = before
	events, err := s.ListEvents(Filter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
	if len(events) > 0 && events[0].Action != "recent.action" {
		t.Errorf("action = %q, want recent.action", events[0].Action)
	}
}

func TestListEventsMultipleFilters(t *testing.T) {
	s := newTestStore(t)

	s.RecordEvent("admin-1", "role.change", "user-1", nil)
	s.RecordEvent("admin-1", "client.create", "client-1", nil)
	s.RecordEvent("admin-2", "role.change", "user-2", nil)
	s.RecordEvent("admin-1", "role.change", "user-3", nil)

	events, err := s.ListEvents(Filter{Actor: "admin-1", Action: "role.change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2 (admin-1 + role.change)", len(events))
	}
}

// --- Specific Actions Audited ---

func TestAuditOnUserRoleChange(t *testing.T) {
	s := newTestStore(t)

	details := json.RawMessage(`{"old_role":"user","new_role":"admin"}`)
	ev, err := s.RecordEvent("admin-1", "role.change", "user-42", details)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}

	events, err := s.ListEvents(Filter{Action: "role.change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Target != "user-42" {
		t.Errorf("target = %q, want user-42", events[0].Target)
	}

	var d map[string]string
	if err := json.Unmarshal(events[0].Details, &d); err != nil {
		t.Fatalf("parse details: %v", err)
	}
	if d["old_role"] != "user" || d["new_role"] != "admin" {
		t.Errorf("details = %v, want old_role=user, new_role=admin", d)
	}
}

func TestAuditOnClientCreate(t *testing.T) {
	s := newTestStore(t)

	details := json.RawMessage(`{"client_name":"MyApp"}`)
	ev, err := s.RecordEvent("admin-1", "client.create", "client-99", details)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}

	events, err := s.ListEvents(Filter{Action: "client.create"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Target != "client-99" {
		t.Errorf("target = %q", events[0].Target)
	}
}

func TestAuditOnClientDelete(t *testing.T) {
	s := newTestStore(t)

	details := json.RawMessage(`{"client_name":"OldApp"}`)
	ev, err := s.RecordEvent("admin-1", "client.delete", "client-88", details)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}

	events, err := s.ListEvents(Filter{Action: "client.delete"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestAuditOnTenantImport(t *testing.T) {
	s := newTestStore(t)

	details := json.RawMessage(`{"tenant_id":"tenant-1","source":"backup.json"}`)
	ev, err := s.RecordEvent("admin-1", "tenant.import", "tenant-1", details)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}

	events, err := s.ListEvents(Filter{Action: "tenant.import"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestAuditOnTenantExport(t *testing.T) {
	s := newTestStore(t)

	ev, err := s.RecordEvent("admin-1", "tenant.export", "tenant-2", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}

	events, err := s.ListEvents(Filter{Action: "tenant.export"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestAuditOnTenantDelete(t *testing.T) {
	s := newTestStore(t)

	ev, err := s.RecordEvent("admin-1", "tenant.delete", "tenant-3", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}

	events, err := s.ListEvents(Filter{Action: "tenant.delete"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Actor != "admin-1" {
		t.Errorf("actor = %q", events[0].Actor)
	}
}

// --- API Endpoint ---

func TestAuditLogAPIRequiresAdmin(t *testing.T) {
	s := newTestStore(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	// No admin role in context — should get 403
	w := httptest.NewRecorder()
	s.HandleListEvents(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestAuditLogAPIReturnsJSON(t *testing.T) {
	s := newTestStore(t)

	s.RecordEvent("admin-1", "role.change", "user-1", nil)
	s.RecordEvent("admin-1", "client.create", "client-1", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	// Simulate admin context
	req.Header.Set("X-Test-Role", "admin")
	w := httptest.NewRecorder()
	s.HandleListEvents(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var events []Event
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
}

func TestAuditLogAPIFiltering(t *testing.T) {
	s := newTestStore(t)

	s.RecordEvent("admin-1", "role.change", "user-1", nil)
	s.RecordEvent("admin-1", "client.create", "client-1", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/audit?action=role.change", nil)
	req.Header.Set("X-Test-Role", "admin")
	w := httptest.NewRecorder()
	s.HandleListEvents(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var events []Event
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
	if len(events) > 0 && events[0].Action != "role.change" {
		t.Errorf("action = %q", events[0].Action)
	}
}

func TestAuditLogAPIPagination(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 10; i++ {
		s.RecordEvent("a", fmt.Sprintf("action.%d", i), "", nil)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/audit?offset=0&limit=3", nil)
	req.Header.Set("X-Test-Role", "admin")
	w := httptest.NewRecorder()
	s.HandleListEvents(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var events []Event
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("got %d events, want 3", len(events))
	}
}

// --- Edge Cases ---

func TestAuditLogImmutable(t *testing.T) {
	s := newTestStore(t)

	ev, err := s.RecordEvent("admin-1", "role.change", "user-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}

	// Attempt direct SQL update — should fail or have no effect in production.
	// For now we verify the store exposes no Update/Delete methods by checking
	// that the DB row remains unchanged after a direct UPDATE attempt.
	db := s.db
	_, err = db.Exec("UPDATE audit_events SET action = 'hacked' WHERE id = ?", ev.ID)
	// The schema should ideally prevent this, but at minimum the store should
	// never expose mutation methods. We verify the record via ListEvents.
	events, err := s.ListEvents(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	// If immutability is enforced at DB level, action should still be role.change
	if events[0].Action != "role.change" {
		t.Errorf("action = %q, want role.change (immutability violated)", events[0].Action)
	}
}

func TestAuditLogActorNotNull(t *testing.T) {
	s := newTestStore(t)

	_, err := s.RecordEvent("", "role.change", "user-1", nil)
	if err == nil {
		t.Error("expected error when actor is empty")
	}
}

func TestAuditLogActionNotNull(t *testing.T) {
	s := newTestStore(t)

	_, err := s.RecordEvent("admin-1", "", "user-1", nil)
	if err == nil {
		t.Error("expected error when action is empty")
	}
}
