package marketing

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	db       *sql.DB
	tenantID string
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// ForTenant returns a copy of the store scoped to the given tenant.
func (s *Store) ForTenant(tenantID string) *Store {
	return &Store{db: s.db, tenantID: tenantID}
}

// TenantID returns the tenant scope of this store.
func (s *Store) TenantID() string {
	return s.tenantID
}

// --- Contacts ---

func (s *Store) CreateContact(email, name, consentSource string) (*Contact, error) {
	inviteToken := uuid.NewString()
	c := &Contact{
		ID:               uuid.NewString(),
		TenantID:         s.tenantID,
		Email:            email,
		Name:             name,
		UnsubscribeToken: uuid.NewString(),
		InviteToken:      &inviteToken,
		ConsentSource:    consentSource,
		ConsentAt:        time.Now().UTC(),
		CreatedAt:        time.Now().UTC(),
	}

	_, err := s.db.Exec(
		`INSERT INTO contacts (id, tenant_id, email, name, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.TenantID, c.Email, c.Name, c.UnsubscribeToken, c.InviteToken, c.ConsentSource, c.ConsentAt, c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) GetContactByID(id string) (*Contact, error) {
	c := &Contact{}
	var userID, inviteToken sql.NullString
	var unsub int
	err := s.db.QueryRow(
		`SELECT id, tenant_id, email, name, user_id, unsubscribed, unsubscribe_token, invite_token, consent_source, consent_at, created_at
		 FROM contacts WHERE id = ? AND tenant_id = ?`, id, s.tenantID,
	).Scan(&c.ID, &c.TenantID, &c.Email, &c.Name, &userID, &unsub, &c.UnsubscribeToken, &inviteToken, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Unsubscribed = unsub != 0
	if userID.Valid {
		c.UserID = &userID.String
	}
	if inviteToken.Valid {
		c.InviteToken = &inviteToken.String
	}
	return c, nil
}

func (s *Store) GetContactByEmail(email string) (*Contact, error) {
	c := &Contact{}
	var userID, inviteToken sql.NullString
	var unsub int
	err := s.db.QueryRow(
		`SELECT id, tenant_id, email, name, user_id, unsubscribed, unsubscribe_token, invite_token, consent_source, consent_at, created_at
		 FROM contacts WHERE email = ? AND tenant_id = ?`, email, s.tenantID,
	).Scan(&c.ID, &c.TenantID, &c.Email, &c.Name, &userID, &unsub, &c.UnsubscribeToken, &inviteToken, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Unsubscribed = unsub != 0
	if userID.Valid {
		c.UserID = &userID.String
	}
	if inviteToken.Valid {
		c.InviteToken = &inviteToken.String
	}
	return c, nil
}

func (s *Store) ListContacts() ([]Contact, error) {
	rows, err := s.db.Query(
		`SELECT id, tenant_id, email, name, user_id, unsubscribed, consent_source, consent_at, created_at
		 FROM contacts WHERE tenant_id = ? ORDER BY created_at DESC`, s.tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		var userID sql.NullString
		var unsub int
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Email, &c.Name, &userID, &unsub, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Unsubscribed = unsub != 0
		if userID.Valid {
			c.UserID = &userID.String
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *Store) DeleteContact(id string) error {
	result, err := s.db.Exec("DELETE FROM contacts WHERE id = ? AND tenant_id = ?", id, s.tenantID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("contact not found")
	}
	return nil
}

func (s *Store) UnsubscribeContact(token string) error {
	result, err := s.db.Exec("UPDATE contacts SET unsubscribed = 1 WHERE unsubscribe_token = ? AND tenant_id = ?", token, s.tenantID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("invalid unsubscribe token")
	}
	return nil
}

func (s *Store) ImportContacts(contacts []ContactImport) (imported int, skipped int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO contacts (id, tenant_id, email, name, unsubscribe_token, invite_token, consent_source, consent_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'import', ?, ?)`,
	)
	if err != nil {
		return 0, 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, c := range contacts {
		result, err := stmt.Exec(uuid.NewString(), s.tenantID, c.Email, c.Name, uuid.NewString(), uuid.NewString(), now, now)
		if err != nil {
			return imported, skipped, err
		}
		rows, _ := result.RowsAffected()
		if rows > 0 {
			imported++
		} else {
			skipped++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return imported, skipped, nil
}

func (s *Store) GetContactByUnsubscribeToken(token string) (*Contact, error) {
	c := &Contact{}
	var userID, inviteToken sql.NullString
	var unsub int
	err := s.db.QueryRow(
		`SELECT id, tenant_id, email, name, user_id, unsubscribed, unsubscribe_token, invite_token, consent_source, consent_at, created_at
		 FROM contacts WHERE unsubscribe_token = ? AND tenant_id = ?`, token, s.tenantID,
	).Scan(&c.ID, &c.TenantID, &c.Email, &c.Name, &userID, &unsub, &c.UnsubscribeToken, &inviteToken, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Unsubscribed = unsub != 0
	if userID.Valid {
		c.UserID = &userID.String
	}
	if inviteToken.Valid {
		c.InviteToken = &inviteToken.String
	}
	return c, nil
}

// ListContactsWithSegments returns all contacts with their segments populated.
func (s *Store) ListContactsWithSegments() ([]Contact, error) {
	contacts, err := s.ListContacts()
	if err != nil {
		return nil, err
	}
	for i := range contacts {
		segs, err := s.GetContactSegments(contacts[i].ID)
		if err != nil {
			return nil, err
		}
		contacts[i].Segments = segs
	}
	return contacts, nil
}

// --- Segments ---

func (s *Store) CreateSegment(name, description string) (*Segment, error) {
	seg := &Segment{
		ID:          uuid.NewString(),
		TenantID:    s.tenantID,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}
	_, err := s.db.Exec(
		"INSERT INTO segments (id, tenant_id, name, description, created_at) VALUES (?, ?, ?, ?, ?)",
		seg.ID, seg.TenantID, seg.Name, seg.Description, seg.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return seg, nil
}

func (s *Store) ListSegments() ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT s.id, s.tenant_id, s.name, s.description, s.created_at, COUNT(cs.contact_id) as contact_count
		 FROM segments s
		 LEFT JOIN contact_segments cs ON s.id = cs.segment_id
		 WHERE s.tenant_id = ?
		 GROUP BY s.id
		 ORDER BY s.created_at DESC`, s.tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []Segment
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(&seg.ID, &seg.TenantID, &seg.Name, &seg.Description, &seg.CreatedAt, &seg.ContactCount); err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}
	return segments, rows.Err()
}

func (s *Store) GetSegmentByID(id string) (*Segment, error) {
	seg := &Segment{}
	err := s.db.QueryRow(
		`SELECT s.id, s.tenant_id, s.name, s.description, s.created_at, COUNT(cs.contact_id) as contact_count
		 FROM segments s
		 LEFT JOIN contact_segments cs ON s.id = cs.segment_id
		 WHERE s.id = ? AND s.tenant_id = ?
		 GROUP BY s.id`, id, s.tenantID,
	).Scan(&seg.ID, &seg.TenantID, &seg.Name, &seg.Description, &seg.CreatedAt, &seg.ContactCount)
	if err != nil {
		return nil, err
	}
	return seg, nil
}

func (s *Store) UpdateSegment(id, name, description string) (*Segment, error) {
	result, err := s.db.Exec(
		"UPDATE segments SET name = ?, description = ? WHERE id = ? AND tenant_id = ?",
		name, description, id, s.tenantID,
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
	result, err := s.db.Exec("DELETE FROM segments WHERE id = ? AND tenant_id = ?", id, s.tenantID)
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
		 WHERE cs.segment_id = ? AND c.tenant_id = ?
		 ORDER BY c.created_at DESC`, segmentID, s.tenantID,
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
		 WHERE cs.contact_id = ? AND s.tenant_id = ?
		 ORDER BY s.name`, contactID, s.tenantID,
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

// --- Campaigns ---

func (s *Store) CreateCampaign(subject, htmlBody, fromName, fromEmail string, segmentIDs []string) (*Campaign, error) {
	c := &Campaign{
		ID:        uuid.NewString(),
		TenantID:  s.tenantID,
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
		`INSERT INTO campaigns (id, tenant_id, subject, html_body, from_name, from_email, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.TenantID, c.Subject, c.HTMLBody, c.FromName, c.FromEmail, c.Status, c.CreatedAt,
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
		 WHERE c.tenant_id = ?
		 GROUP BY c.id
		 ORDER BY c.created_at DESC`, s.tenantID,
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
		`SELECT id, tenant_id, subject, html_body, from_name, from_email, status, sent_at, created_at
		 FROM campaigns WHERE id = ? AND tenant_id = ?`, id, s.tenantID,
	).Scan(&c.ID, &c.TenantID, &c.Subject, &c.HTMLBody, &c.FromName, &c.FromEmail, &c.Status, &sentAt, &c.CreatedAt)
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
		`UPDATE campaigns SET subject = ?, html_body = ?, from_name = ?, from_email = ? WHERE id = ? AND tenant_id = ?`,
		subject, htmlBody, fromName, fromEmail, id, s.tenantID,
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
	_, err = s.db.Exec("DELETE FROM campaigns WHERE id = ? AND tenant_id = ?", id, s.tenantID)
	return err
}

func (s *Store) GetCampaignStats(id string) (*CampaignStats, error) {
	stats := &CampaignStats{}
	err := s.db.QueryRow(
		`SELECT COUNT(*) as total,
		        COALESCE(SUM(CASE WHEN cr.status = 'queued' THEN 1 ELSE 0 END), 0) as queued,
		        COALESCE(SUM(CASE WHEN cr.status = 'sent' THEN 1 ELSE 0 END), 0) as sent,
		        COALESCE(SUM(CASE WHEN cr.status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
		        COALESCE(SUM(CASE WHEN cr.opened_at IS NOT NULL THEN 1 ELSE 0 END), 0) as opened
		 FROM campaign_recipients cr
		 JOIN campaigns c ON cr.campaign_id = c.id
		 WHERE cr.campaign_id = ? AND c.tenant_id = ?`, id, s.tenantID,
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
		 JOIN campaigns camp ON cr.campaign_id = camp.id
		 WHERE cr.campaign_id = ? AND camp.tenant_id = ?
		 ORDER BY c.email`, campaignID, s.tenantID,
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
		 WHERE csg.campaign_id = ? AND c.unsubscribed = 0 AND c.tenant_id = ?`, campaignID, s.tenantID,
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

// UpdateRecipientStatus updates a recipient's send status. Called from processCampaign
// which already loaded the campaign with tenant scope, so tenant isolation is guaranteed
// by the caller. UUID recipient IDs make cross-tenant guessing impractical.
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

// RecordOpen records the first open time for a recipient. Called from a public
// tracking endpoint. UUID recipient IDs make cross-tenant guessing impractical.
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
	query += " WHERE id = ? AND tenant_id = ?"
	args = append(args, id, s.tenantID)
	_, err := s.db.Exec(query, args...)
	return err
}
