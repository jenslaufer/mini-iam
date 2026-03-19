package tenant

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/marketing"
)

// --- Config types for import ---

type ImportConfig struct {
	Slug                string           `json:"slug"`
	Name                string           `json:"name"`
	RegistrationEnabled bool             `json:"registration_enabled"`
	SMTP                *SMTPConfig      `json:"smtp,omitempty"`
	Admin               *AdminConfig     `json:"admin,omitempty"`
	Users               []UserConfig     `json:"users,omitempty"`
	Clients             []ClientConfig   `json:"clients,omitempty"`
	Segments            []SegmentConfig  `json:"segments,omitempty"`
	Contacts            []ContactConfig  `json:"contacts,omitempty"`
	Campaigns           []CampaignConfig `json:"campaigns,omitempty"`
}

type AdminConfig struct {
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
}

type UserConfig struct {
	Email        string `json:"email"`
	Password     string `json:"password,omitempty"`
	PasswordHash string `json:"password_hash,omitempty"`
	Name         string `json:"name"`
	Role         string `json:"role"`
}

type ClientConfig struct {
	ClientID     string   `json:"client_id,omitempty"`
	SecretHash   string   `json:"secret_hash,omitempty"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
}

type SegmentConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ContactConfig struct {
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	Segments      []string   `json:"segments,omitempty"`
	ConsentSource string     `json:"consent_source"`
	Unsubscribed  *bool      `json:"unsubscribed,omitempty"`
	ConsentAt     *time.Time `json:"consent_at,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	InviteToken   *string    `json:"invite_token,omitempty"`
}

type CampaignConfig struct {
	Subject    string                  `json:"subject"`
	HTMLBody   string                  `json:"html_body"`
	FromName   string                  `json:"from_name"`
	FromEmail  string                  `json:"from_email"`
	Segments   []string                `json:"segments,omitempty"`
	Status     string                  `json:"status,omitempty"`
	SentAt     *time.Time              `json:"sent_at,omitempty"`
	CreatedAt  *time.Time              `json:"created_at,omitempty"`
	Recipients []CampaignRecipientConfig `json:"recipients,omitempty"`
}

type CampaignRecipientConfig struct {
	ContactEmail string     `json:"contact_email"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"error_message,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
}

type ImportResult struct {
	TenantID string
	Skipped  bool
	Clients  []ClientImported
}

// ClientImported holds created client details for import responses.
type ClientImported struct {
	Name         string   `json:"name"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
}

var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidateSlug checks that a slug matches the required format.
func ValidateSlug(slug string) error {
	if !slugRegex.MatchString(slug) {
		return fmt.Errorf("slug must match ^[a-z0-9][a-z0-9-]{0,62}$")
	}
	return nil
}

// --- Import response types ---

type importResponse struct {
	TenantID string           `json:"tenant_id"`
	Slug     string           `json:"slug"`
	Clients  []ClientImported `json:"clients,omitempty"`
}

// --- Programmatic import ---

// ImportTenantConfig imports a full tenant configuration. If the tenant already
// exists (by slug), it returns the existing tenant ID with Skipped=true.
func ImportTenantConfig(tenantStore *Store, iamStore *iam.Store, mktStore *marketing.Store, cfg ImportConfig) (*ImportResult, error) {
	if err := ValidateSlug(cfg.Slug); err != nil {
		return nil, err
	}

	existing, err := tenantStore.GetBySlug(cfg.Slug)
	if err == nil {
		return &ImportResult{TenantID: existing.ID, Skipped: true}, nil
	}

	smtp := SMTPConfig{}
	if cfg.SMTP != nil {
		smtp = *cfg.SMTP
	}
	tn, err := tenantStore.CreateWithSMTP(cfg.Slug, cfg.Name, smtp)
	if err != nil {
		return nil, err
	}
	if cfg.RegistrationEnabled {
		tenantStore.UpdateRegistrationEnabled(tn.ID, true)
	}

	scopedIAM := iamStore.ForTenant(tn.ID)
	scopedMkt := mktStore.ForTenant(tn.ID)

	// Merge admin field into users list for unified processing.
	users := mergeAdminAndUsers(cfg.Admin, cfg.Users)

	if err := validateUsers(users); err != nil {
		return nil, err
	}

	for _, u := range users {
		if u.PasswordHash != "" {
			// Migration mode: insert with pre-hashed password.
			if _, err := scopedIAM.CreateUserWithHash(u.Email, u.PasswordHash, u.Name, u.Role); err != nil {
				return nil, err
			}
		} else if u.Role == "admin" {
			if err := scopedIAM.SeedAdmin(u.Email, u.Password, u.Name); err != nil {
				return nil, err
			}
		} else {
			created, err := scopedIAM.CreateUser(u.Email, u.Password, u.Name)
			if err != nil {
				return nil, err
			}
			if _, err := scopedIAM.UpdateUser(created.ID, "", "member"); err != nil {
				return nil, err
			}
		}
	}

	var clients []ClientImported
	for _, c := range cfg.Clients {
		if c.ClientID != "" && c.SecretHash != "" {
			// Migration mode: preserve client ID and secret hash.
			client, err := scopedIAM.CreateClientWithID(c.ClientID, c.SecretHash, c.Name, c.RedirectURIs)
			if err != nil {
				return nil, err
			}
			clients = append(clients, ClientImported{
				Name:         client.Name,
				ClientID:     client.ID,
				RedirectURIs: client.RedirectURIs,
			})
		} else {
			client, secret, err := scopedIAM.CreateClient(c.Name, c.RedirectURIs)
			if err != nil {
				return nil, err
			}
			clients = append(clients, ClientImported{
				Name:         client.Name,
				ClientID:     client.ID,
				ClientSecret: secret,
				RedirectURIs: client.RedirectURIs,
			})
		}
	}

	segmentMap := map[string]string{}
	for _, s := range cfg.Segments {
		seg, err := scopedMkt.CreateSegment(s.Name, s.Description)
		if err != nil {
			return nil, err
		}
		segmentMap[s.Name] = seg.ID
	}

	contactEmailToID := map[string]string{}
	for _, c := range cfg.Contacts {
		var contact *marketing.Contact
		var err error
		if c.Unsubscribed != nil || c.ConsentAt != nil || c.CreatedAt != nil || c.InviteToken != nil {
			// Migration mode: preserve all fields.
			unsub := false
			if c.Unsubscribed != nil {
				unsub = *c.Unsubscribed
			}
			consentAt := time.Now().UTC()
			if c.ConsentAt != nil {
				consentAt = *c.ConsentAt
			}
			createdAt := time.Now().UTC()
			if c.CreatedAt != nil {
				createdAt = *c.CreatedAt
			}
			contact, err = scopedMkt.CreateContactFull(c.Email, c.Name, c.ConsentSource, unsub, consentAt, createdAt, c.InviteToken)
		} else {
			contact, err = scopedMkt.CreateContact(c.Email, c.Name, c.ConsentSource)
		}
		if err != nil {
			return nil, err
		}
		contactEmailToID[c.Email] = contact.ID
		for _, segName := range c.Segments {
			if segID, ok := segmentMap[segName]; ok {
				if err := scopedMkt.AddContactToSegment(contact.ID, segID); err != nil {
					return nil, err
				}
			}
		}
	}

	for _, c := range cfg.Campaigns {
		var segIDs []string
		for _, segName := range c.Segments {
			if segID, ok := segmentMap[segName]; ok {
				segIDs = append(segIDs, segID)
			}
		}
		if c.Status != "" || c.SentAt != nil || c.CreatedAt != nil {
			// Migration mode: preserve status and timestamps.
			status := c.Status
			if status == "" {
				status = "draft"
			}
			createdAt := time.Now().UTC()
			if c.CreatedAt != nil {
				createdAt = *c.CreatedAt
			}
			campaign, err := scopedMkt.CreateCampaignFull(c.Subject, c.HTMLBody, c.FromName, c.FromEmail, status, c.SentAt, createdAt, segIDs)
			if err != nil {
				return nil, err
			}
			// Import recipients if present.
			for _, r := range c.Recipients {
				contactID, ok := contactEmailToID[r.ContactEmail]
				if !ok {
					// Try to look up contact by email.
					contact, err := scopedMkt.GetContactByEmail(r.ContactEmail)
					if err != nil {
						return nil, fmt.Errorf("recipient contact %q not found", r.ContactEmail)
					}
					contactID = contact.ID
				}
				if err := scopedMkt.CreateCampaignRecipient(campaign.ID, contactID, r.Status, r.ErrorMessage, r.SentAt, r.OpenedAt); err != nil {
					return nil, err
				}
			}
		} else {
			if _, err := scopedMkt.CreateCampaign(c.Subject, c.HTMLBody, c.FromName, c.FromEmail, segIDs); err != nil {
				return nil, err
			}
		}
	}

	return &ImportResult{TenantID: tn.ID, Clients: clients}, nil
}

// validRoles are the allowed values for UserConfig.Role.
var validRoles = map[string]bool{"admin": true, "member": true}

// validateUsers checks for duplicate emails and invalid roles.
func validateUsers(users []UserConfig) error {
	seen := map[string]bool{}
	for _, u := range users {
		lower := strings.ToLower(u.Email)
		if seen[lower] {
			return fmt.Errorf("duplicate email in users: %s", u.Email)
		}
		seen[lower] = true
		if !validRoles[u.Role] {
			return fmt.Errorf("invalid role %q for user %s, must be admin or member", u.Role, u.Email)
		}
	}
	return nil
}

// mergeAdminAndUsers combines the legacy admin field with the users array.
// If the admin email already appears in users, the existing entry is promoted to admin.
// Otherwise the admin is prepended with role=admin.
func mergeAdminAndUsers(admin *AdminConfig, users []UserConfig) []UserConfig {
	if admin == nil || admin.Email == "" {
		return users
	}

	lowerAdmin := strings.ToLower(admin.Email)
	for i, u := range users {
		if strings.ToLower(u.Email) == lowerAdmin {
			users[i].Role = "admin"
			if admin.Password != "" && users[i].Password == "" {
				users[i].Password = admin.Password
			}
			return users
		}
	}

	name := "Admin"
	return append([]UserConfig{{
		Email:    admin.Email,
		Password: admin.Password,
		Name:     name,
		Role:     "admin",
	}}, users...)
}

// --- HTTP handler ---

type ExportImportHandler struct {
	tenantStore      *Store
	iamStore         *iam.Store
	mktStore         *marketing.Store
	registry         *iam.TokenRegistry
	PlatformTenantID string
}

func NewExportImportHandler(tenantStore *Store, iamStore *iam.Store, mktStore *marketing.Store, registry *iam.TokenRegistry, platformTenantID string) *ExportImportHandler {
	return &ExportImportHandler{
		tenantStore:      tenantStore,
		iamStore:         iamStore,
		mktStore:         mktStore,
		registry:         registry,
		PlatformTenantID: platformTenantID,
	}
}

// Import handles POST /admin/tenants/import.
func (h *ExportImportHandler) Import(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if _, ok := iam.CheckAdminCrossTenant(h.registry, h.iamStore, h.PlatformTenantID, w, r); !ok {
		return
	}

	var cfg ImportConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if cfg.Slug == "" {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "slug required")
		return
	}

	if err := ValidateSlug(cfg.Slug); err != nil {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	merged := mergeAdminAndUsers(cfg.Admin, cfg.Users)
	if err := validateUsers(merged); err != nil {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := ImportTenantConfig(h.tenantStore, h.iamStore, h.mktStore, cfg)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			iam.WriteError(w, http.StatusConflict, "invalid_request", "tenant slug already exists")
			return
		}
		iam.WriteError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if result.Skipped {
		iam.WriteError(w, http.StatusConflict, "invalid_request", "tenant slug already exists")
		return
	}

	iam.WriteJSON(w, http.StatusCreated, importResponse{
		TenantID: result.TenantID,
		Slug:     cfg.Slug,
		Clients:  result.Clients,
	})
}

// --- Batch import types ---

type batchImportEntry struct {
	TenantID string           `json:"tenant_id,omitempty"`
	Slug     string           `json:"slug"`
	Clients  []ClientImported `json:"clients,omitempty"`
	Skipped  bool             `json:"skipped,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// ImportBatch handles POST /admin/tenants/import-batch.
func (h *ExportImportHandler) ImportBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if _, ok := iam.CheckAdminCrossTenant(h.registry, h.iamStore, h.PlatformTenantID, w, r); !ok {
		return
	}

	var configs []ImportConfig
	if err := json.NewDecoder(r.Body).Decode(&configs); err != nil {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	results := make([]batchImportEntry, 0, len(configs))
	for _, cfg := range configs {
		entry := batchImportEntry{Slug: cfg.Slug}

		result, err := ImportTenantConfig(h.tenantStore, h.iamStore, h.mktStore, cfg)
		if err != nil {
			entry.Error = err.Error()
			results = append(results, entry)
			continue
		}
		if result.Skipped {
			entry.TenantID = result.TenantID
			entry.Skipped = true
			results = append(results, entry)
			continue
		}

		entry.TenantID = result.TenantID
		entry.Clients = result.Clients
		results = append(results, entry)
	}

	iam.WriteJSON(w, http.StatusOK, results)
}

// ExportOrDelete handles /admin/tenants/{id} and /admin/tenants/{id}/export.
func (h *ExportImportHandler) ExportOrDelete(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin/tenants/")

	if strings.HasSuffix(path, "/export") {
		h.handleExport(w, r, strings.TrimSuffix(path, "/export"))
		return
	}

	h.handleTenantByID(w, r, path)
}

func (h *ExportImportHandler) handleExport(w http.ResponseWriter, r *http.Request, tenantID string) {
	if r.Method != http.MethodGet {
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if _, ok := iam.CheckAdminCrossTenant(h.registry, h.iamStore, h.PlatformTenantID, w, r); !ok {
		return
	}

	tn, err := h.tenantStore.GetByID(tenantID)
	if err != nil {
		iam.WriteError(w, http.StatusNotFound, "not_found", "tenant not found")
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "seed"
	}
	migration := mode == "migration"

	scopedIAM := h.iamStore.ForTenant(tn.ID)
	scopedMkt := h.mktStore.ForTenant(tn.ID)

	export := map[string]interface{}{
		"slug":                 tn.Slug,
		"name":                 tn.Name,
		"registration_enabled": tn.RegistrationEnabled,
	}

	// SMTP config (only if configured)
	if tn.SMTP.Host != "" {
		smtpExport := map[string]interface{}{
			"smtp_host":      tn.SMTP.Host,
			"smtp_port":      tn.SMTP.Port,
			"smtp_user":      tn.SMTP.User,
			"smtp_from":      tn.SMTP.From,
			"smtp_from_name": tn.SMTP.FromName,
			"smtp_rate_ms":   tn.SMTP.RateMS,
		}
		if migration {
			smtpExport["smtp_password"] = tn.SMTP.Password
		}
		export["smtp"] = smtpExport
	}

	// Users
	if migration {
		users, err := scopedIAM.ExportUsers()
		if err == nil && len(users) > 0 {
			var exportUsers []map[string]interface{}
			for _, u := range users {
				exportUsers = append(exportUsers, map[string]interface{}{
					"email":         u.Email,
					"name":          u.Name,
					"role":          u.Role,
					"password_hash": u.PasswordHash,
				})
				if u.Role == "admin" {
					if _, ok := export["admin"]; !ok {
						export["admin"] = map[string]string{"email": u.Email}
					}
				}
			}
			export["users"] = exportUsers
		}
	} else {
		users, err := scopedIAM.ListUsers()
		if err == nil && len(users) > 0 {
			var exportUsers []map[string]string
			for _, u := range users {
				exportUsers = append(exportUsers, map[string]string{
					"email": u.Email,
					"name":  u.Name,
					"role":  u.Role,
				})
				if u.Role == "admin" {
					if _, ok := export["admin"]; !ok {
						export["admin"] = map[string]string{"email": u.Email}
					}
				}
			}
			export["users"] = exportUsers
		}
	}

	// Clients
	if migration {
		clients, err := scopedIAM.ExportClients()
		if err == nil && len(clients) > 0 {
			var exportClients []map[string]interface{}
			for _, c := range clients {
				exportClients = append(exportClients, map[string]interface{}{
					"client_id":     c.ID,
					"secret_hash":   c.SecretHash,
					"name":          c.Name,
					"redirect_uris": c.RedirectURIs,
				})
			}
			export["clients"] = exportClients
		}
	} else {
		clients, err := scopedIAM.ListClients()
		if err == nil && len(clients) > 0 {
			var exportClients []map[string]interface{}
			for _, c := range clients {
				exportClients = append(exportClients, map[string]interface{}{
					"name":          c.Name,
					"redirect_uris": c.RedirectURIs,
				})
			}
			export["clients"] = exportClients
		}
	}

	// Segments — build ID->name map
	segments, err := scopedMkt.ListSegments()
	segNameByID := map[string]string{}
	if err == nil && len(segments) > 0 {
		var exportSegments []map[string]string
		for _, s := range segments {
			segNameByID[s.ID] = s.Name
			exportSegments = append(exportSegments, map[string]string{
				"name":        s.Name,
				"description": s.Description,
			})
		}
		export["segments"] = exportSegments
	}

	// Contacts with segment names
	contacts, err := scopedMkt.ListContactsWithSegments()
	if err == nil && len(contacts) > 0 {
		var exportContacts []map[string]interface{}
		for _, c := range contacts {
			ec := map[string]interface{}{
				"email":          c.Email,
				"name":           c.Name,
				"consent_source": c.ConsentSource,
			}
			if migration {
				ec["unsubscribed"] = c.Unsubscribed
				ec["consent_at"] = c.ConsentAt
				ec["created_at"] = c.CreatedAt
				if c.InviteToken != nil {
					ec["invite_token"] = *c.InviteToken
				}
			}
			if len(c.Segments) > 0 {
				var segNames []string
				for _, s := range c.Segments {
					segNames = append(segNames, s.Name)
				}
				ec["segments"] = segNames
			}
			exportContacts = append(exportContacts, ec)
		}
		export["contacts"] = exportContacts
	}

	// Campaigns
	campaignSummaries, err := scopedMkt.ListCampaigns()
	if err == nil && len(campaignSummaries) > 0 {
		var exportCampaigns []map[string]interface{}
		for _, cs := range campaignSummaries {
			if !migration && cs.Status != "draft" {
				continue
			}
			full, err := scopedMkt.GetCampaignByID(cs.ID)
			if err != nil {
				continue
			}
			ec := map[string]interface{}{
				"subject":    full.Subject,
				"html_body":  full.HTMLBody,
				"from_name":  full.FromName,
				"from_email": full.FromEmail,
			}
			if migration {
				ec["status"] = full.Status
				ec["created_at"] = full.CreatedAt
				if full.SentAt != nil {
					ec["sent_at"] = *full.SentAt
				}
				// Export recipients.
				recipients, err := scopedMkt.GetCampaignRecipients(full.ID)
				if err == nil && len(recipients) > 0 {
					var exportRecipients []map[string]interface{}
					for _, r := range recipients {
						er := map[string]interface{}{
							"contact_email": r.ContactEmail,
							"status":        r.Status,
						}
						if r.ErrorMessage != "" {
							er["error_message"] = r.ErrorMessage
						}
						if r.SentAt != nil {
							er["sent_at"] = *r.SentAt
						}
						if r.OpenedAt != nil {
							er["opened_at"] = *r.OpenedAt
						}
						exportRecipients = append(exportRecipients, er)
					}
					ec["recipients"] = exportRecipients
				}
			}
			if len(full.SegmentIDs) > 0 {
				var segNames []string
				for _, segID := range full.SegmentIDs {
					if name, ok := segNameByID[segID]; ok {
						segNames = append(segNames, name)
					}
				}
				if len(segNames) > 0 {
					ec["segments"] = segNames
				}
			}
			exportCampaigns = append(exportCampaigns, ec)
		}
		if len(exportCampaigns) > 0 {
			export["campaigns"] = exportCampaigns
		}
	}

	iam.WriteJSON(w, http.StatusOK, export)
}

func (h *ExportImportHandler) handleTenantByID(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := iam.CheckAdminCrossTenant(h.registry, h.iamStore, h.PlatformTenantID, w, r); !ok {
		return
	}

	if id == "" || id == "import" {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "tenant id required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		t, err := h.tenantStore.GetByID(id)
		if err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "tenant not found")
			return
		}
		// Sanitize: never expose SMTP password in API responses
		sanitized := *t
		sanitized.SMTP.Password = ""
		iam.WriteJSON(w, http.StatusOK, &sanitized)
	case http.MethodDelete:
		if err := h.tenantStore.Delete(id); err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "tenant not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}
