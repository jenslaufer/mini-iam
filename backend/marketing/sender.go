package marketing

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// --- Attachment ---

type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// --- Mailer interface ---

type Mailer interface {
	Send(to, subject, htmlBody string, headers map[string]string, attachments []Attachment) error
}

// --- SMTP Mailer ---

type SMTPMailer struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	FromName string
}

func (m *SMTPMailer) Send(to, subject, htmlBody string, headers map[string]string, attachments []Attachment) error {
	msg := buildMIMEMessage(m.From, m.FromName, to, subject, htmlBody, headers, attachments)
	auth := smtp.PlainAuth("", m.User, m.Password, m.Host)
	addr := fmt.Sprintf("%s:%s", m.Host, m.Port)
	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}

// buildMIMEMessage constructs a complete email message. If attachments are
// present it builds a multipart/mixed message; otherwise a simple text/html.
func buildMIMEMessage(from, fromName, to, subject, htmlBody string, headers map[string]string, attachments []Attachment) []byte {
	fromHeader := from
	if fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", fromName, from)
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", fromHeader))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	if len(attachments) == 0 {
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(htmlBody)
		return []byte(msg.String())
	}

	b := make([]byte, 16)
	rand.Read(b)
	boundary := fmt.Sprintf("==BOUNDARY_%x==", b)
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// HTML part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n")

	// Attachment parts
	for _, att := range attachments {
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", att.ContentType, att.Filename))
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", att.Filename))
		msg.WriteString("\r\n")
		msg.WriteString(base64.StdEncoding.EncodeToString(att.Data))
		msg.WriteString("\r\n")
	}

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return []byte(msg.String())
}

// --- Log Mailer (development fallback) ---

type LogMailer struct{}

func (m *LogMailer) Send(to, subject, htmlBody string, headers map[string]string, attachments []Attachment) error {
	attInfo := ""
	for _, a := range attachments {
		attInfo += fmt.Sprintf(" [%s %d bytes]", a.Filename, len(a.Data))
	}
	log.Printf("[MAIL] To: %s | Subject: %s | Headers: %v | Body length: %d%s", to, subject, headers, len(htmlBody), attInfo)
	return nil
}

// TenantProvider looks up tenant config. Implemented by tenant.Store.
type TenantProvider interface {
	GetSMTPConfig(tenantID string) (host, port, user, password, from, fromName string, rateMS int, err error)
	GetTenantSlug(tenantID string) (string, error)
}

// --- Campaign Sender (background worker) ---

const maxAttachmentSize = 10 * 1024 * 1024 // 10 MB

type sendRequest struct {
	campaignID string
	tenantID   string
}

type CampaignSender struct {
	store          *Store
	mailer         Mailer
	tenantProvider TenantProvider
	issuer         string
	rateMS         int
	queue          chan sendRequest
	syncMode       bool
	httpClient     *http.Client
}

func NewCampaignSender(store *Store, mailer Mailer, issuer string, rateMS int) *CampaignSender {
	return &CampaignSender{
		store:  store,
		mailer: mailer,
		issuer: issuer,
		rateMS: rateMS,
		queue:  make(chan sendRequest, 100),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetTenantProvider sets the tenant provider for per-tenant mailer selection and slug lookup.
func (cs *CampaignSender) SetTenantProvider(p TenantProvider) {
	cs.tenantProvider = p
}

// mailerForTenant returns a tenant-specific SMTPMailer if configured, otherwise the global mailer.
func (cs *CampaignSender) mailerForTenant(tenantID string) (Mailer, int) {
	if cs.tenantProvider != nil {
		host, port, user, pass, from, fromName, rateMS, err := cs.tenantProvider.GetSMTPConfig(tenantID)
		if err == nil && host != "" {
			return &SMTPMailer{Host: host, Port: port, User: user, Password: pass, From: from, FromName: fromName}, rateMS
		}
	}
	return cs.mailer, cs.rateMS
}

// tenantBaseURL returns the issuer URL with tenant path prefix if applicable.
func (cs *CampaignSender) tenantBaseURL(tenantID string) string {
	if cs.tenantProvider != nil {
		slug, err := cs.tenantProvider.GetTenantSlug(tenantID)
		if err == nil && slug != "" {
			return cs.issuer + "/t/" + slug
		}
	}
	return cs.issuer
}

func (cs *CampaignSender) Start() {
	go func() {
		for req := range cs.queue {
			cs.processCampaign(req.campaignID, req.tenantID)
		}
	}()
}

// StartSync configures the sender to process campaigns synchronously inside
// Enqueue, which avoids goroutine leaks and timing issues in tests.
func (cs *CampaignSender) StartSync() {
	cs.syncMode = true
}

func (cs *CampaignSender) Enqueue(campaignID string, tenantID string) {
	if cs.syncMode {
		cs.processCampaign(campaignID, tenantID)
		return
	}
	cs.queue <- sendRequest{campaignID: campaignID, tenantID: tenantID}
}

// safeFilename strips everything except alphanumerics, dots, hyphens, and underscores.
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// fetchAttachment downloads the PDF from the given URL and returns an Attachment.
func (cs *CampaignSender) fetchAttachment(rawURL string) (*Attachment, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid attachment URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("attachment URL must use http or https scheme")
	}

	resp, err := cs.httpClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("download attachment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download attachment: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAttachmentSize+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if len(data) > maxAttachmentSize {
		return nil, fmt.Errorf("attachment exceeds maximum size of %d bytes", maxAttachmentSize)
	}

	filename := safeFilenameRe.ReplaceAllString(path.Base(parsed.Path), "_")
	if filename == "" || filename == "." || filename == "/" {
		filename = "document.pdf"
	}

	return &Attachment{
		Filename:    filename,
		ContentType: "application/pdf",
		Data:        data,
	}, nil
}

func (cs *CampaignSender) processCampaign(campaignID string, tenantID string) {
	store := cs.store.ForTenant(tenantID)

	campaign, err := store.GetCampaignByID(campaignID)
	if err != nil {
		log.Printf("Campaign %s: failed to load: %v", campaignID, err)
		return
	}

	if campaign.Status != "draft" {
		log.Printf("Campaign %s: not in draft status, skipping", campaignID)
		return
	}

	// Set status to sending
	if err := store.SetCampaignStatus(campaignID, "sending"); err != nil {
		log.Printf("Campaign %s: failed to set sending status: %v", campaignID, err)
		return
	}

	// Fetch attachment once for all recipients
	var attachments []Attachment
	if campaign.AttachmentURL != "" {
		att, err := cs.fetchAttachment(campaign.AttachmentURL)
		if err != nil {
			log.Printf("Campaign %s: failed to fetch attachment: %v", campaignID, err)
			store.SetCampaignStatus(campaignID, "failed")
			return
		}
		attachments = []Attachment{*att}
	}

	// Prepare recipients (deduplicate, skip unsubscribed)
	count, err := store.PrepareCampaignRecipients(campaignID)
	if err != nil {
		log.Printf("Campaign %s: failed to prepare recipients: %v", campaignID, err)
		store.SetCampaignStatus(campaignID, "failed")
		return
	}
	log.Printf("Campaign %s: prepared %d recipients", campaignID, count)

	// Get recipients
	recipients, err := store.GetCampaignRecipients(campaignID)
	if err != nil {
		log.Printf("Campaign %s: failed to get recipients: %v", campaignID, err)
		store.SetCampaignStatus(campaignID, "failed")
		return
	}

	mailer, rateMS := cs.mailerForTenant(tenantID)
	baseURL := cs.tenantBaseURL(tenantID)

	allSuccess := true
	for _, r := range recipients {
		// Template substitution — URLs include tenant path prefix
		unsubscribeURL := fmt.Sprintf("%s/unsubscribe/%s", baseURL, cs.getUnsubscribeToken(store, r.ContactID))
		trackingURL := fmt.Sprintf("%s/track/%s", baseURL, r.ID)
		inviteURL := fmt.Sprintf("%s/activate/%s", baseURL, cs.getInviteToken(store, r.ContactID))

		body := campaign.HTMLBody
		body = strings.ReplaceAll(body, "{{.Name}}", r.ContactName)
		body = strings.ReplaceAll(body, "{{.Email}}", r.ContactEmail)
		body = strings.ReplaceAll(body, "{{.UnsubscribeURL}}", unsubscribeURL)
		body = strings.ReplaceAll(body, "{{.TrackingPixelURL}}", trackingURL)
		body = strings.ReplaceAll(body, "{{.InviteURL}}", inviteURL)

		// Append tracking pixel
		body += fmt.Sprintf(`<img src="%s" width="1" height="1" alt="" style="display:none">`, trackingURL)

		headers := map[string]string{
			"List-Unsubscribe": fmt.Sprintf("<%s>", unsubscribeURL),
		}

		subject := campaign.Subject

		if err := mailer.Send(r.ContactEmail, subject, body, headers, attachments); err != nil {
			log.Printf("Campaign %s: failed to send to %s: %v", campaignID, r.ContactEmail, err)
			store.UpdateRecipientStatus(r.ID, "failed", err.Error())
			allSuccess = false
		} else {
			store.UpdateRecipientStatus(r.ID, "sent", "")
		}

		// Rate limiting (use tenant-specific rate or global fallback)
		if rateMS > 0 {
			time.Sleep(time.Duration(rateMS) * time.Millisecond)
		}
	}

	if allSuccess {
		store.SetCampaignStatus(campaignID, "sent")
	} else {
		store.SetCampaignStatus(campaignID, "failed")
	}
	log.Printf("Campaign %s: sending complete", campaignID)
}

func (cs *CampaignSender) getUnsubscribeToken(store *Store, contactID string) string {
	contact, err := store.GetContactByID(contactID)
	if err != nil {
		return ""
	}
	return contact.UnsubscribeToken
}

func (cs *CampaignSender) getInviteToken(store *Store, contactID string) string {
	contact, err := store.GetContactByID(contactID)
	if err != nil || contact.InviteToken == nil {
		return ""
	}
	return *contact.InviteToken
}
