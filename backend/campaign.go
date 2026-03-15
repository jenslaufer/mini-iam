package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Models ---

type Campaign struct {
	ID         string     `json:"id"`
	Subject    string     `json:"subject"`
	HTMLBody   string     `json:"html_body"`
	FromName   string     `json:"from_name"`
	FromEmail  string     `json:"from_email"`
	Status     string     `json:"status"`
	SentAt     *time.Time `json:"sent_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	SegmentIDs []string   `json:"segment_ids,omitempty"`
}

type CampaignSummary struct {
	ID        string     `json:"id"`
	Subject   string     `json:"subject"`
	FromName  string     `json:"from_name"`
	FromEmail string     `json:"from_email"`
	Status    string     `json:"status"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	Total     int        `json:"total"`
	Queued    int        `json:"queued"`
	Sent      int        `json:"sent"`
	Failed    int        `json:"failed"`
	Opened    int        `json:"opened"`
}

type CampaignStats struct {
	Total  int `json:"total"`
	Queued int `json:"queued"`
	Sent   int `json:"sent"`
	Failed int `json:"failed"`
	Opened int `json:"opened"`
}

type CampaignRecipient struct {
	ID           string     `json:"id"`
	CampaignID   string     `json:"campaign_id"`
	ContactID    string     `json:"contact_id"`
	ContactEmail string     `json:"contact_email"`
	ContactName  string     `json:"contact_name"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"error_message,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
}

// --- Store methods ---

func (s *Store) CreateCampaign(subject, htmlBody, fromName, fromEmail string, segmentIDs []string) (*Campaign, error) {
	c := &Campaign{
		ID:        uuid.NewString(),
		Subject:   subject,
		HTMLBody:  htmlBody,
		FromName:  fromName,
		FromEmail: fromEmail,
		Status:    "draft",
		CreatedAt: time.Now().UTC(),
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO campaigns (id, subject, html_body, from_name, from_email, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Subject, c.HTMLBody, c.FromName, c.FromEmail, c.Status, c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	for _, segID := range segmentIDs {
		_, err = tx.Exec(
			"INSERT INTO campaign_segments (campaign_id, segment_id) VALUES (?, ?)",
			c.ID, segID,
		)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	c.SegmentIDs = segmentIDs
	return c, nil
}

func (s *Store) ListCampaigns() ([]CampaignSummary, error) {
	rows, err := s.db.Query(
		`SELECT c.id, c.subject, c.from_name, c.from_email, c.status, c.sent_at, c.created_at,
		        COALESCE(SUM(1), 0) as total,
		        COALESCE(SUM(CASE WHEN cr.status = 'queued' THEN 1 ELSE 0 END), 0) as queued,
		        COALESCE(SUM(CASE WHEN cr.status = 'sent' THEN 1 ELSE 0 END), 0) as sent,
		        COALESCE(SUM(CASE WHEN cr.status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
		        COALESCE(SUM(CASE WHEN cr.opened_at IS NOT NULL THEN 1 ELSE 0 END), 0) as opened
		 FROM campaigns c
		 LEFT JOIN campaign_recipients cr ON c.id = cr.campaign_id
		 GROUP BY c.id
		 ORDER BY c.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []CampaignSummary
	for rows.Next() {
		var cs CampaignSummary
		var sentAt sql.NullTime
		if err := rows.Scan(&cs.ID, &cs.Subject, &cs.FromName, &cs.FromEmail, &cs.Status, &sentAt, &cs.CreatedAt,
			&cs.Total, &cs.Queued, &cs.Sent, &cs.Failed, &cs.Opened); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			cs.SentAt = &sentAt.Time
		}
		// If no recipients, total should be 0, not 1 from COUNT
		if cs.Queued == 0 && cs.Sent == 0 && cs.Failed == 0 {
			cs.Total = 0
		}
		campaigns = append(campaigns, cs)
	}
	return campaigns, rows.Err()
}

func (s *Store) GetCampaignByID(id string) (*Campaign, error) {
	c := &Campaign{}
	var sentAt sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, subject, html_body, from_name, from_email, status, sent_at, created_at
		 FROM campaigns WHERE id = ?`, id,
	).Scan(&c.ID, &c.Subject, &c.HTMLBody, &c.FromName, &c.FromEmail, &c.Status, &sentAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	if sentAt.Valid {
		c.SentAt = &sentAt.Time
	}

	// Load segment IDs
	rows, err := s.db.Query("SELECT segment_id FROM campaign_segments WHERE campaign_id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var segID string
		if err := rows.Scan(&segID); err != nil {
			return nil, err
		}
		c.SegmentIDs = append(c.SegmentIDs, segID)
	}

	return c, rows.Err()
}

func (s *Store) UpdateCampaign(id, subject, htmlBody, fromName, fromEmail string, segmentIDs []string) (*Campaign, error) {
	c, err := s.GetCampaignByID(id)
	if err != nil {
		return nil, err
	}
	if c.Status != "draft" {
		return nil, fmt.Errorf("can only update draft campaigns")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`UPDATE campaigns SET subject = ?, html_body = ?, from_name = ?, from_email = ? WHERE id = ?`,
		subject, htmlBody, fromName, fromEmail, id,
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec("DELETE FROM campaign_segments WHERE campaign_id = ?", id)
	if err != nil {
		return nil, err
	}

	for _, segID := range segmentIDs {
		_, err = tx.Exec(
			"INSERT INTO campaign_segments (campaign_id, segment_id) VALUES (?, ?)",
			id, segID,
		)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	c.Subject = subject
	c.HTMLBody = htmlBody
	c.FromName = fromName
	c.FromEmail = fromEmail
	c.SegmentIDs = segmentIDs
	return c, nil
}

func (s *Store) DeleteCampaign(id string) error {
	c, err := s.GetCampaignByID(id)
	if err != nil {
		return fmt.Errorf("campaign not found")
	}
	if c.Status != "draft" {
		return fmt.Errorf("can only delete draft campaigns")
	}
	_, err = s.db.Exec("DELETE FROM campaigns WHERE id = ?", id)
	return err
}

func (s *Store) GetCampaignStats(id string) (*CampaignStats, error) {
	stats := &CampaignStats{}
	err := s.db.QueryRow(
		`SELECT COUNT(*) as total,
		        COALESCE(SUM(CASE WHEN status = 'queued' THEN 1 ELSE 0 END), 0) as queued,
		        COALESCE(SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END), 0) as sent,
		        COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
		        COALESCE(SUM(CASE WHEN opened_at IS NOT NULL THEN 1 ELSE 0 END), 0) as opened
		 FROM campaign_recipients WHERE campaign_id = ?`, id,
	).Scan(&stats.Total, &stats.Queued, &stats.Sent, &stats.Failed, &stats.Opened)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (s *Store) GetCampaignRecipients(campaignID string) ([]CampaignRecipient, error) {
	rows, err := s.db.Query(
		`SELECT cr.id, cr.campaign_id, cr.contact_id, c.email, c.name, cr.status, cr.error_message, cr.sent_at, cr.opened_at
		 FROM campaign_recipients cr
		 JOIN contacts c ON cr.contact_id = c.id
		 WHERE cr.campaign_id = ?
		 ORDER BY c.email`, campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recipients []CampaignRecipient
	for rows.Next() {
		var r CampaignRecipient
		var sentAt, openedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.CampaignID, &r.ContactID, &r.ContactEmail, &r.ContactName,
			&r.Status, &r.ErrorMessage, &sentAt, &openedAt); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			r.SentAt = &sentAt.Time
		}
		if openedAt.Valid {
			r.OpenedAt = &openedAt.Time
		}
		recipients = append(recipients, r)
	}
	return recipients, rows.Err()
}

func (s *Store) PrepareCampaignRecipients(campaignID string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Get deduplicated, non-unsubscribed contacts for this campaign's segments
	rows, err := tx.Query(
		`SELECT DISTINCT c.id
		 FROM contacts c
		 JOIN contact_segments cs ON c.id = cs.contact_id
		 JOIN campaign_segments csg ON cs.segment_id = csg.segment_id
		 WHERE csg.campaign_id = ? AND c.unsubscribed = 0`, campaignID,
	)
	if err != nil {
		return 0, err
	}

	var contactIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		contactIDs = append(contactIDs, id)
	}
	rows.Close()

	count := 0
	for _, contactID := range contactIDs {
		recipientID := uuid.NewString()
		result, err := tx.Exec(
			`INSERT OR IGNORE INTO campaign_recipients (id, campaign_id, contact_id, status)
			 VALUES (?, ?, ?, 'queued')`,
			recipientID, campaignID, contactID,
		)
		if err != nil {
			return 0, err
		}
		affected, _ := result.RowsAffected()
		count += int(affected)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) UpdateRecipientStatus(recipientID, status, errorMsg string) error {
	var sentAt interface{}
	if status == "sent" {
		now := time.Now().UTC()
		sentAt = now
	}
	_, err := s.db.Exec(
		"UPDATE campaign_recipients SET status = ?, error_message = ?, sent_at = ? WHERE id = ?",
		status, errorMsg, sentAt, recipientID,
	)
	return err
}

func (s *Store) RecordOpen(recipientID string) error {
	_, err := s.db.Exec(
		"UPDATE campaign_recipients SET opened_at = ? WHERE id = ? AND opened_at IS NULL",
		time.Now().UTC(), recipientID,
	)
	return err
}

func (s *Store) SetCampaignStatus(id, status string) error {
	args := []interface{}{status}
	query := "UPDATE campaigns SET status = ?"
	if status == "sent" || status == "failed" {
		query += ", sent_at = ?"
		args = append(args, time.Now().UTC())
	}
	query += " WHERE id = ?"
	args = append(args, id)
	_, err := s.db.Exec(query, args...)
	return err
}

// --- Handlers ---

func (h *Handler) AdminCampaigns(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		campaigns, err := h.store.ListCampaigns()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "failed to list campaigns")
			return
		}
		writeJSON(w, http.StatusOK, campaigns)

	case http.MethodPost:
		var req struct {
			Subject    string   `json:"subject"`
			HTMLBody   string   `json:"html_body"`
			FromName   string   `json:"from_name"`
			FromEmail  string   `json:"from_email"`
			SegmentIDs []string `json:"segment_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if req.Subject == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "subject required")
			return
		}
		if req.HTMLBody == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "html_body required")
			return
		}
		campaign, err := h.store.CreateCampaign(req.Subject, req.HTMLBody, req.FromName, req.FromEmail, req.SegmentIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "failed to create campaign")
			return
		}
		writeJSON(w, http.StatusCreated, campaign)

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminCampaignByID(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/admin/campaigns/")
	parts := strings.SplitN(path, "/", 2)
	campaignID := parts[0]

	if campaignID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "campaign id required")
		return
	}

	// /admin/campaigns/{id}/send
	if len(parts) == 2 && parts[1] == "send" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		campaign, err := h.store.GetCampaignByID(campaignID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		if campaign.Status != "draft" {
			writeError(w, http.StatusBadRequest, "invalid_request", "can only send draft campaigns")
			return
		}
		if h.sender == nil {
			writeError(w, http.StatusInternalServerError, "server_error", "email sender not configured")
			return
		}
		h.sender.Enqueue(campaignID)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "sending"})
		return
	}

	// /admin/campaigns/{id}/stats
	if len(parts) == 2 && parts[1] == "stats" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		stats, err := h.store.GetCampaignStats(campaignID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		writeJSON(w, http.StatusOK, stats)
		return
	}

	// /admin/campaigns/{id}
	switch r.Method {
	case http.MethodGet:
		campaign, err := h.store.GetCampaignByID(campaignID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		stats, _ := h.store.GetCampaignStats(campaignID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"campaign": campaign,
			"stats":    stats,
		})

	case http.MethodPut:
		var req struct {
			Subject    string   `json:"subject"`
			HTMLBody   string   `json:"html_body"`
			FromName   string   `json:"from_name"`
			FromEmail  string   `json:"from_email"`
			SegmentIDs []string `json:"segment_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		campaign, err := h.store.UpdateCampaign(campaignID, req.Subject, req.HTMLBody, req.FromName, req.FromEmail, req.SegmentIDs)
		if err != nil {
			if strings.Contains(err.Error(), "draft") {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			writeError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		writeJSON(w, http.StatusOK, campaign)

	case http.MethodDelete:
		if err := h.store.DeleteCampaign(campaignID); err != nil {
			if strings.Contains(err.Error(), "draft") {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminContacts(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		contacts, err := h.store.ListContactsWithSegments()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "failed to list contacts")
			return
		}
		writeJSON(w, http.StatusOK, contacts)

	case http.MethodPost:
		var req struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if !emailRegex.MatchString(req.Email) {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid email format")
			return
		}
		contact, err := h.store.CreateContact(req.Email, req.Name, "api")
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				writeError(w, http.StatusConflict, "invalid_request", "email already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, "server_error", "failed to create contact")
			return
		}
		writeJSON(w, http.StatusCreated, contact)

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminContactByID(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/admin/contacts/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "contact id required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		contact, err := h.store.GetContactByID(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "contact not found")
			return
		}
		segs, _ := h.store.GetContactSegments(id)
		contact.Segments = segs
		writeJSON(w, http.StatusOK, contact)

	case http.MethodDelete:
		if err := h.store.DeleteContact(id); err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminImportContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	var contacts []ContactImport
	if err := json.NewDecoder(r.Body).Decode(&contacts); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body, expected array of {email, name}")
		return
	}

	imported, skipped, err := h.store.ImportContacts(contacts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "import failed")
		return
	}

	// Add contacts to segments if specified
	for _, ci := range contacts {
		if len(ci.Segments) > 0 {
			contact, err := h.store.GetContactByEmail(ci.Email)
			if err != nil {
				continue
			}
			for _, segName := range ci.Segments {
				// Find segment by name, create if not exists
				segs, _ := h.store.ListSegments()
				for _, seg := range segs {
					if seg.Name == segName {
						h.store.AddContactToSegment(contact.ID, seg.ID)
						break
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]int{
		"imported": imported,
		"skipped":  skipped,
	})
}

// --- Public endpoints ---

// 1x1 transparent GIF pixel
var trackingPixel = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00,
	0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21,
	0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00,
	0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44,
	0x01, 0x00, 0x3b,
}

func (h *Handler) TrackOpen(w http.ResponseWriter, r *http.Request) {
	recipientID := strings.TrimPrefix(r.URL.Path, "/track/")
	if recipientID != "" {
		h.store.RecordOpen(recipientID)
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Write(trackingPixel)
}

func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/unsubscribe/")
	if token == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		contact, err := h.store.GetContactByUnsubscribeToken(token)
		if err != nil {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`<!DOCTYPE html><html><body><p>Invalid unsubscribe link.</p></body></html>`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Unsubscribe</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}.card{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);text-align:center;max-width:400px}button{padding:0.75rem 2rem;background:#dc2626;color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:1rem}button:hover{background:#b91c1c}</style>
</head><body><div class="card">
<h2>Unsubscribe</h2>
<p>Unsubscribe <strong>%s</strong> from future emails?</p>
<form method="POST"><button type="submit">Unsubscribe</button></form>
</div></body></html>`, escapeHTML(contact.Email))

	case http.MethodPost:
		if err := h.store.UnsubscribeContact(token); err != nil {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`<!DOCTYPE html><html><body><p>Invalid unsubscribe link.</p></body></html>`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Unsubscribed</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}.card{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.1);text-align:center;max-width:400px}</style>
</head><body><div class="card">
<h2>You have been unsubscribed</h2>
<p>You will no longer receive emails from us.</p>
</div></body></html>`))

	default:
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}
