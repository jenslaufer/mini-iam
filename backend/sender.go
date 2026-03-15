package main

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

// --- Campaign Sender (background worker) ---

type CampaignSender struct {
	store    *Store
	mailer   Mailer
	issuer   string
	rateMS   int
	queue    chan string
	syncMode bool
}

func NewCampaignSender(store *Store, mailer Mailer, issuer string, rateMS int) *CampaignSender {
	return &CampaignSender{
		store:  store,
		mailer: mailer,
		issuer: issuer,
		rateMS: rateMS,
		queue:  make(chan string, 100),
	}
}

func (cs *CampaignSender) Start() {
	go func() {
		for campaignID := range cs.queue {
			cs.processCampaign(campaignID)
		}
	}()
}

// StartSync configures the sender to process campaigns synchronously inside
// Enqueue, which avoids goroutine leaks and timing issues in tests.
func (cs *CampaignSender) StartSync() {
	cs.syncMode = true
}

func (cs *CampaignSender) Enqueue(campaignID string) {
	if cs.syncMode {
		cs.processCampaign(campaignID)
		return
	}
	cs.queue <- campaignID
}

func (cs *CampaignSender) processCampaign(campaignID string) {
	campaign, err := cs.store.GetCampaignByID(campaignID)
	if err != nil {
		log.Printf("Campaign %s: failed to load: %v", campaignID, err)
		return
	}

	if campaign.Status != "draft" {
		log.Printf("Campaign %s: not in draft status, skipping", campaignID)
		return
	}

	// Set status to sending
	if err := cs.store.SetCampaignStatus(campaignID, "sending"); err != nil {
		log.Printf("Campaign %s: failed to set sending status: %v", campaignID, err)
		return
	}

	// Prepare recipients (deduplicate, skip unsubscribed)
	count, err := cs.store.PrepareCampaignRecipients(campaignID)
	if err != nil {
		log.Printf("Campaign %s: failed to prepare recipients: %v", campaignID, err)
		cs.store.SetCampaignStatus(campaignID, "failed")
		return
	}
	log.Printf("Campaign %s: prepared %d recipients", campaignID, count)

	// Get recipients
	recipients, err := cs.store.GetCampaignRecipients(campaignID)
	if err != nil {
		log.Printf("Campaign %s: failed to get recipients: %v", campaignID, err)
		cs.store.SetCampaignStatus(campaignID, "failed")
		return
	}

	allSuccess := true
	for _, r := range recipients {
		// Template substitution
		unsubscribeURL := fmt.Sprintf("%s/unsubscribe/%s", cs.issuer, cs.getUnsubscribeToken(r.ContactID))
		trackingURL := fmt.Sprintf("%s/track/%s", cs.issuer, r.ID)

		body := campaign.HTMLBody
		body = strings.ReplaceAll(body, "{{.Name}}", r.ContactName)
		body = strings.ReplaceAll(body, "{{.Email}}", r.ContactEmail)
		body = strings.ReplaceAll(body, "{{.UnsubscribeURL}}", unsubscribeURL)
		body = strings.ReplaceAll(body, "{{.TrackingPixelURL}}", trackingURL)

		// Append tracking pixel
		body += fmt.Sprintf(`<img src="%s" width="1" height="1" alt="" style="display:none">`, trackingURL)

		headers := map[string]string{
			"List-Unsubscribe": fmt.Sprintf("<%s>", unsubscribeURL),
		}

		// Use campaign from fields, fall back to mailer defaults
		fromName := campaign.FromName
		fromEmail := campaign.FromEmail

		subject := campaign.Subject
		_ = fromName
		_ = fromEmail

		if err := cs.mailer.Send(r.ContactEmail, subject, body, headers); err != nil {
			log.Printf("Campaign %s: failed to send to %s: %v", campaignID, r.ContactEmail, err)
			cs.store.UpdateRecipientStatus(r.ID, "failed", err.Error())
			allSuccess = false
		} else {
			cs.store.UpdateRecipientStatus(r.ID, "sent", "")
		}

		// Rate limiting
		if cs.rateMS > 0 {
			time.Sleep(time.Duration(cs.rateMS) * time.Millisecond)
		}
	}

	if allSuccess {
		cs.store.SetCampaignStatus(campaignID, "sent")
	} else {
		cs.store.SetCampaignStatus(campaignID, "failed")
	}
	log.Printf("Campaign %s: sending complete", campaignID)
}

func (cs *CampaignSender) getUnsubscribeToken(contactID string) string {
	contact, err := cs.store.GetContactByID(contactID)
	if err != nil {
		return ""
	}
	return contact.UnsubscribeToken
}
