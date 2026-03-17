package tenant

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

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
	Clients             []ClientConfig   `json:"clients,omitempty"`
	Segments            []SegmentConfig  `json:"segments,omitempty"`
	Contacts            []ContactConfig  `json:"contacts,omitempty"`
	Campaigns           []CampaignConfig `json:"campaigns,omitempty"`
}

type AdminConfig struct {
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
}

type ClientConfig struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
}

type SegmentConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ContactConfig struct {
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	Segments      []string `json:"segments,omitempty"`
	ConsentSource string   `json:"consent_source"`
}

type CampaignConfig struct {
	Subject   string   `json:"subject"`
	HTMLBody  string   `json:"html_body"`
	FromName  string   `json:"from_name"`
	FromEmail string   `json:"from_email"`
	Segments  []string `json:"segments,omitempty"`
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

	if cfg.Admin != nil && cfg.Admin.Email != "" {
		if err := scopedIAM.SeedAdmin(cfg.Admin.Email, cfg.Admin.Password, "Admin"); err != nil {
			return nil, err
		}
	}

	var clients []ClientImported
	for _, c := range cfg.Clients {
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

	segmentMap := map[string]string{}
	for _, s := range cfg.Segments {
		seg, err := scopedMkt.CreateSegment(s.Name, s.Description)
		if err != nil {
			return nil, err
		}
		segmentMap[s.Name] = seg.ID
	}

	for _, c := range cfg.Contacts {
		contact, err := scopedMkt.CreateContact(c.Email, c.Name, c.ConsentSource)
		if err != nil {
			return nil, err
		}
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
		if _, err := scopedMkt.CreateCampaign(c.Subject, c.HTMLBody, c.FromName, c.FromEmail, segIDs); err != nil {
			return nil, err
		}
	}

	return &ImportResult{TenantID: tn.ID, Clients: clients}, nil
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

	scopedIAM := h.iamStore.ForTenant(tn.ID)
	scopedMkt := h.mktStore.ForTenant(tn.ID)

	export := map[string]interface{}{
		"slug":                 tn.Slug,
		"name":                 tn.Name,
		"registration_enabled": tn.RegistrationEnabled,
	}

	// SMTP config (only if configured); password is never exported
	if tn.SMTP.Host != "" {
		export["smtp"] = map[string]interface{}{
			"smtp_host":      tn.SMTP.Host,
			"smtp_port":      tn.SMTP.Port,
			"smtp_user":      tn.SMTP.User,
			"smtp_from":      tn.SMTP.From,
			"smtp_from_name": tn.SMTP.FromName,
			"smtp_rate_ms":   tn.SMTP.RateMS,
		}
	}

	// Admin — first admin user, email only
	users, err := scopedIAM.ListUsers()
	if err == nil {
		for _, u := range users {
			if u.Role == "admin" {
				export["admin"] = map[string]string{"email": u.Email}
				break
			}
		}
	}

	// Clients — name + redirect_uris only
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

	// Draft campaigns with segment names
	campaignSummaries, err := scopedMkt.ListCampaigns()
	if err == nil && len(campaignSummaries) > 0 {
		var exportCampaigns []map[string]interface{}
		for _, cs := range campaignSummaries {
			if cs.Status != "draft" {
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
