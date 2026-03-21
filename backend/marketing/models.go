package marketing

import "time"

type Contact struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	Email            string    `json:"email"`
	Name             string    `json:"name"`
	UserID           *string   `json:"user_id,omitempty"`
	Unsubscribed     bool      `json:"unsubscribed"`
	UnsubscribeToken string    `json:"-"`
	InviteToken          *string    `json:"-"`
	InviteTokenExpiresAt *time.Time `json:"-"`
	ConsentSource        string     `json:"consent_source"`
	ConsentAt        time.Time `json:"consent_at"`
	CreatedAt        time.Time `json:"created_at"`
	Segments         []Segment `json:"segments,omitempty"`
}

type ContactImport struct {
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Segments []string `json:"segments,omitempty"`
}

type Segment struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	ContactCount int       `json:"contact_count,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Campaign struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	Subject       string     `json:"subject"`
	HTMLBody      string     `json:"html_body"`
	FromName      string     `json:"from_name"`
	FromEmail     string     `json:"from_email"`
	AttachmentURL string     `json:"attachment_url,omitempty"`
	Status        string     `json:"status"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	SegmentIDs    []string   `json:"segment_ids,omitempty"`
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
