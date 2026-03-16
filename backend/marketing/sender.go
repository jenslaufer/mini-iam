package marketing

import (
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"time"
)

// --- Mailer interface ---

type Mailer interface {
	Send(to, subject, htmlBody string, headers map[string]string) error
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

func (m *SMTPMailer) Send(to, subject, htmlBody string, headers map[string]string) error {
	from := m.From
	if m.FromName != "" {
		from = fmt.Sprintf("%s <%s>", m.FromName, m.From)
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	auth := smtp.PlainAuth("", m.User, m.Password, m.Host)
	addr := fmt.Sprintf("%s:%s", m.Host, m.Port)
	return smtp.SendMail(addr, auth, m.From, []string{to}, []byte(msg.String()))
}

// --- Log Mailer (development fallback) ---

type LogMailer struct{}

func (m *LogMailer) Send(to, subject, htmlBody string, headers map[string]string) error {
	log.Printf("[MAIL] To: %s | Subject: %s | Headers: %v | Body length: %d", to, subject, headers, len(htmlBody))
	return nil
}

// TenantProvider looks up tenant config. Implemented by tenant.Store.
type TenantProvider interface {
	GetSMTPConfig(tenantID string) (host, port, user, password, from, fromName string, rateMS int, err error)
	GetTenantSlug(tenantID string) (string, error)
}

// --- Campaign Sender (background worker) ---

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
}

func NewCampaignSender(store *Store, mailer Mailer, issuer string, rateMS int) *CampaignSender {
	return &CampaignSender{
		store:  store,
		mailer: mailer,
		issuer: issuer,
		rateMS: rateMS,
		queue:  make(chan sendRequest, 100),
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

		if err := mailer.Send(r.ContactEmail, subject, body, headers); err != nil {
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
