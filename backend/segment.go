package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Models ---

type Segment struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ContactCount int    `json:"contact_count,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// --- Store methods ---

func (s *Store) CreateSegment(name, description string) (*Segment, error) {
	seg := &Segment{
		ID:          uuid.NewString(),
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}
	_, err := s.db.Exec(
		"INSERT INTO segments (id, name, description, created_at) VALUES (?, ?, ?, ?)",
		seg.ID, seg.Name, seg.Description, seg.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return seg, nil
}

func (s *Store) ListSegments() ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT s.id, s.name, s.description, s.created_at, COUNT(cs.contact_id) as contact_count
		 FROM segments s
		 LEFT JOIN contact_segments cs ON s.id = cs.segment_id
		 GROUP BY s.id
		 ORDER BY s.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []Segment
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(&seg.ID, &seg.Name, &seg.Description, &seg.CreatedAt, &seg.ContactCount); err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}
	return segments, rows.Err()
}

func (s *Store) GetSegmentByID(id string) (*Segment, error) {
	seg := &Segment{}
	err := s.db.QueryRow(
		`SELECT s.id, s.name, s.description, s.created_at, COUNT(cs.contact_id) as contact_count
		 FROM segments s
		 LEFT JOIN contact_segments cs ON s.id = cs.segment_id
		 WHERE s.id = ?
		 GROUP BY s.id`, id,
	).Scan(&seg.ID, &seg.Name, &seg.Description, &seg.CreatedAt, &seg.ContactCount)
	if err != nil {
		return nil, err
	}
	return seg, nil
}

func (s *Store) UpdateSegment(id, name, description string) (*Segment, error) {
	result, err := s.db.Exec(
		"UPDATE segments SET name = ?, description = ? WHERE id = ?",
		name, description, id,
	)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, fmt.Errorf("segment not found")
	}
	return s.GetSegmentByID(id)
}

func (s *Store) DeleteSegment(id string) error {
	result, err := s.db.Exec("DELETE FROM segments WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("segment not found")
	}
	return nil
}

func (s *Store) AddContactToSegment(contactID, segmentID string) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO contact_segments (contact_id, segment_id) VALUES (?, ?)",
		contactID, segmentID,
	)
	return err
}

func (s *Store) RemoveContactFromSegment(contactID, segmentID string) error {
	result, err := s.db.Exec(
		"DELETE FROM contact_segments WHERE contact_id = ? AND segment_id = ?",
		contactID, segmentID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("contact not in segment")
	}
	return nil
}

func (s *Store) GetSegmentContacts(segmentID string) ([]Contact, error) {
	rows, err := s.db.Query(
		`SELECT c.id, c.email, c.name, c.unsubscribed, c.consent_source, c.consent_at, c.created_at
		 FROM contacts c
		 JOIN contact_segments cs ON c.id = cs.contact_id
		 WHERE cs.segment_id = ?
		 ORDER BY c.created_at DESC`, segmentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		var unsub int
		if err := rows.Scan(&c.ID, &c.Email, &c.Name, &unsub, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Unsubscribed = unsub != 0
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *Store) GetContactSegments(contactID string) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT s.id, s.name, s.description, s.created_at
		 FROM segments s
		 JOIN contact_segments cs ON s.id = cs.segment_id
		 WHERE cs.contact_id = ?
		 ORDER BY s.name`, contactID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []Segment
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(&seg.ID, &seg.Name, &seg.Description, &seg.CreatedAt); err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}
	return segments, rows.Err()
}

// --- Handlers ---

func (h *Handler) AdminSegments(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		segments, err := h.store.ListSegments()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "failed to list segments")
			return
		}
		writeJSON(w, http.StatusOK, segments)

	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "name required")
			return
		}
		seg, err := h.store.CreateSegment(req.Name, req.Description)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				writeError(w, http.StatusConflict, "invalid_request", "segment name already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, "server_error", "failed to create segment")
			return
		}
		writeJSON(w, http.StatusCreated, seg)

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminSegmentByID(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	// Path: /admin/segments/{id} or /admin/segments/{id}/contacts or /admin/segments/{id}/contacts/{contact_id}
	path := strings.TrimPrefix(r.URL.Path, "/admin/segments/")
	parts := strings.SplitN(path, "/", 3)
	segmentID := parts[0]

	if segmentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "segment id required")
		return
	}

	// /admin/segments/{id}/contacts/{contact_id}
	if len(parts) == 3 && parts[1] == "contacts" {
		contactID := parts[2]
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		if err := h.store.RemoveContactFromSegment(contactID, segmentID); err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
		return
	}

	// /admin/segments/{id}/contacts
	if len(parts) == 2 && parts[1] == "contacts" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		var req struct {
			ContactID string `json:"contact_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if req.ContactID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "contact_id required")
			return
		}
		if err := h.store.AddContactToSegment(req.ContactID, segmentID); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "failed to add contact to segment")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
		return
	}

	// /admin/segments/{id}
	switch r.Method {
	case http.MethodGet:
		seg, err := h.store.GetSegmentByID(segmentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "segment not found")
			return
		}
		contacts, err := h.store.GetSegmentContacts(segmentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "failed to get segment contacts")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"segment":  seg,
			"contacts": contacts,
		})

	case http.MethodPut:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "name required")
			return
		}
		seg, err := h.store.UpdateSegment(segmentID, req.Name, req.Description)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, seg)

	case http.MethodDelete:
		if err := h.store.DeleteSegment(segmentID); err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}
