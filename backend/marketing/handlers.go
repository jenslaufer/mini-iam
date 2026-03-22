package marketing

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/tenantctx"
)

type Handler struct {
	store            *Store
	iamStore         *iam.Store
	registry         *iam.TokenRegistry
	sender           *CampaignSender
	PlatformTenantID string
}

func NewHandler(store *Store, iamStore *iam.Store, registry *iam.TokenRegistry) *Handler {
	return &Handler{store: store, iamStore: iamStore, registry: registry}
}

func (h *Handler) SetSender(sender *CampaignSender) {
	h.sender = sender
}

// tenantStore returns a marketing store scoped to the request's tenant.
func (h *Handler) tenantStore(r *http.Request) *Store {
	return h.store.ForTenant(tenantctx.FromContext(r.Context()))
}

func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, ok := iam.CheckAdmin(h.registry, h.iamStore, h.PlatformTenantID, w, r)
	return ok
}

// --- Admin Contact Handlers ---

func (h *Handler) AdminContacts(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	store := h.tenantStore(r)

	switch r.Method {
	case http.MethodGet:
		contacts, err := store.ListContactsWithSegments()
		if err != nil {
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to list contacts")
			return
		}
		iam.WriteJSON(w, http.StatusOK, contacts)

	case http.MethodPost:
		var req struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if !iam.DecodeJSON(w, r, &req) {
			return
		}
		if len(req.Email) > 254 || !iam.EmailRegex.MatchString(req.Email) {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid email format")
			return
		}
		contact, err := store.CreateContact(req.Email, req.Name, "api")
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				iam.WriteError(w, http.StatusConflict, "invalid_request", "email already exists")
				return
			}
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to create contact")
			return
		}
		// Return invite_token in the creation response so the caller can build an
		// activation link.  The field is suppressed (json:"-") on the Contact type
		// itself to keep it out of list/detail responses.
		var inviteToken string
		if contact.InviteToken != nil {
			inviteToken = *contact.InviteToken
		}
		iam.WriteJSON(w, http.StatusCreated, map[string]any{
			"id":             contact.ID,
			"email":          contact.Email,
			"name":           contact.Name,
			"unsubscribed":   contact.Unsubscribed,
			"invite_token":   inviteToken,
			"consent_source": contact.ConsentSource,
			"consent_at":     contact.ConsentAt,
			"created_at":     contact.CreatedAt,
		})

	default:
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminContactByID(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/admin/contacts/")
	if id == "" {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "contact id required")
		return
	}
	if !iam.ValidUUID(id) {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid contact id format")
		return
	}

	store := h.tenantStore(r)

	switch r.Method {
	case http.MethodGet:
		contact, err := store.GetContactByID(id)
		if err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "contact not found")
			return
		}
		segs, _ := store.GetContactSegments(id)
		contact.Segments = segs
		iam.WriteJSON(w, http.StatusOK, contact)

	case http.MethodPut:
		var req struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if !iam.DecodeJSON(w, r, &req) {
			return
		}
		contact, err := store.UpdateContact(id, req.Name, req.Email)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				iam.WriteError(w, http.StatusConflict, "invalid_request", "email already exists")
				return
			}
			iam.WriteError(w, http.StatusNotFound, "not_found", "contact not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, contact)

	case http.MethodDelete:
		if err := store.DeleteContact(id); err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "contact not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminImportContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}

	// Accept both wrapped {"contacts":[...], "segment_ids":[...]} and bare JSON array.
	r.Body = http.MaxBytesReader(w, r.Body, iam.MaxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "failed to read body")
		return
	}

	var contacts []ContactImport
	var segmentIDs []string

	var wrapper struct {
		Contacts   []ContactImport `json:"contacts"`
		SegmentIDs []string        `json:"segment_ids"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Contacts != nil {
		contacts = wrapper.Contacts
		segmentIDs = wrapper.SegmentIDs
	} else if err := json.Unmarshal(body, &contacts); err != nil {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	store := h.tenantStore(r)
	imported, skipped, err := store.ImportContacts(contacts)
	if err != nil {
		iam.WriteError(w, http.StatusInternalServerError, "server_error", "import failed")
		return
	}

	// Assign contacts to segments specified in the wrapper
	for _, segID := range segmentIDs {
		seg, err := store.GetSegmentByID(segID)
		if err != nil || seg == nil {
			continue
		}
		for _, ci := range contacts {
			contact, err := store.GetContactByEmail(ci.Email)
			if err != nil {
				continue
			}
			store.AddContactToSegment(contact.ID, seg.ID)
		}
	}

	// Also handle per-contact segment IDs
	for _, ci := range contacts {
		if len(ci.Segments) > 0 {
			contact, err := store.GetContactByEmail(ci.Email)
			if err != nil {
				continue
			}
			for _, segID := range ci.Segments {
				seg, err := store.GetSegmentByID(segID)
				if err != nil || seg == nil {
					continue
				}
				store.AddContactToSegment(contact.ID, seg.ID)
			}
		}
	}

	iam.WriteJSON(w, http.StatusOK, map[string]int{
		"imported": imported,
		"skipped":  skipped,
	})
}

// --- Admin Segment Handlers ---

func (h *Handler) AdminSegments(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	store := h.tenantStore(r)

	switch r.Method {
	case http.MethodGet:
		segments, err := store.ListSegments()
		if err != nil {
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to list segments")
			return
		}
		iam.WriteJSON(w, http.StatusOK, segments)

	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if !iam.DecodeJSON(w, r, &req) {
			return
		}
		if req.Name == "" {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "name required")
			return
		}
		seg, err := store.CreateSegment(req.Name, req.Description)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				iam.WriteError(w, http.StatusConflict, "invalid_request", "segment name already exists")
				return
			}
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to create segment")
			return
		}
		iam.WriteJSON(w, http.StatusCreated, seg)

	default:
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminSegmentByID(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	// Path: /admin/segments/{id} or /admin/segments/{id}/contacts or /admin/segments/{id}/contacts/{contact_id}
	path := strings.TrimPrefix(r.URL.Path, "/admin/segments/")
	parts := strings.SplitN(path, "/", 3)
	segmentID := parts[0]

	if segmentID == "" {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "segment id required")
		return
	}
	if !iam.ValidUUID(segmentID) {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid segment id format")
		return
	}

	store := h.tenantStore(r)

	// /admin/segments/{id}/contacts/{contact_id}
	if len(parts) == 3 && parts[1] == "contacts" {
		contactID := parts[2]
		if !iam.ValidUUID(contactID) {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid contact id format")
			return
		}
		if r.Method != http.MethodDelete {
			iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		if err := store.RemoveContactFromSegment(contactID, segmentID); err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "contact or segment not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, map[string]string{"status": "removed"})
		return
	}

	// /admin/segments/{id}/contacts
	if len(parts) == 2 && parts[1] == "contacts" {
		if r.Method != http.MethodPost {
			iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		var req struct {
			ContactID string `json:"contact_id"`
		}
		if !iam.DecodeJSON(w, r, &req) {
			return
		}
		if req.ContactID == "" {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "contact_id required")
			return
		}
		if err := store.AddContactToSegment(req.ContactID, segmentID); err != nil {
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to add contact to segment")
			return
		}
		iam.WriteJSON(w, http.StatusCreated, map[string]string{"status": "added"})
		return
	}

	// /admin/segments/{id}
	switch r.Method {
	case http.MethodGet:
		seg, err := store.GetSegmentByID(segmentID)
		if err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "segment not found")
			return
		}
		contacts, err := store.GetSegmentContacts(segmentID)
		if err != nil {
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to get segment contacts")
			return
		}
		iam.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"segment":  seg,
			"contacts": contacts,
		})

	case http.MethodPut:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if !iam.DecodeJSON(w, r, &req) {
			return
		}
		if req.Name == "" {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "name required")
			return
		}
		seg, err := store.UpdateSegment(segmentID, req.Name, req.Description)
		if err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "segment not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, seg)

	case http.MethodDelete:
		if err := store.DeleteSegment(segmentID); err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "segment not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

// --- Admin Campaign Handlers ---

func (h *Handler) AdminCampaigns(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	store := h.tenantStore(r)

	switch r.Method {
	case http.MethodGet:
		campaigns, err := store.ListCampaigns()
		if err != nil {
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to list campaigns")
			return
		}
		iam.WriteJSON(w, http.StatusOK, campaigns)

	case http.MethodPost:
		var req struct {
			Subject       string   `json:"subject"`
			HTMLBody      string   `json:"html_body"`
			FromName      string   `json:"from_name"`
			FromEmail     string   `json:"from_email"`
			AttachmentURL string   `json:"attachment_url"`
			SegmentIDs    []string `json:"segment_ids"`
		}
		if !iam.DecodeJSON(w, r, &req) {
			return
		}
		if req.Subject == "" {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "subject required")
			return
		}
		if req.HTMLBody == "" {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "html_body required")
			return
		}
		campaign, err := store.CreateCampaign(req.Subject, req.HTMLBody, req.FromName, req.FromEmail, req.AttachmentURL, req.SegmentIDs)
		if err != nil {
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to create campaign")
			return
		}
		iam.WriteJSON(w, http.StatusCreated, campaign)

	default:
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

func (h *Handler) AdminCampaignByID(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/admin/campaigns/")
	parts := strings.SplitN(path, "/", 2)
	campaignID := parts[0]

	if campaignID == "" {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "campaign id required")
		return
	}
	if !iam.ValidUUID(campaignID) {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid campaign id format")
		return
	}

	store := h.tenantStore(r)

	// /admin/campaigns/{id}/send
	if len(parts) == 2 && parts[1] == "send" {
		if r.Method != http.MethodPost {
			iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		campaign, err := store.GetCampaignByID(campaignID)
		if err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		if campaign.Status != "draft" {
			iam.WriteError(w, http.StatusBadRequest, "invalid_request", "can only send draft campaigns")
			return
		}
		if h.sender == nil {
			iam.WriteError(w, http.StatusInternalServerError, "server_error", "email sender not configured")
			return
		}
		h.sender.Enqueue(campaignID, tenantctx.FromContext(r.Context()))
		iam.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "sending"})
		return
	}

	// /admin/campaigns/{id}/stats
	if len(parts) == 2 && parts[1] == "stats" {
		if r.Method != http.MethodGet {
			iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}
		stats, err := store.GetCampaignStats(campaignID)
		if err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, stats)
		return
	}

	// /admin/campaigns/{id}
	switch r.Method {
	case http.MethodGet:
		campaign, err := store.GetCampaignByID(campaignID)
		if err != nil {
			iam.WriteError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		stats, _ := store.GetCampaignStats(campaignID)
		iam.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"campaign": campaign,
			"stats":    stats,
		})

	case http.MethodPut:
		var req struct {
			Subject       string   `json:"subject"`
			HTMLBody      string   `json:"html_body"`
			FromName      string   `json:"from_name"`
			FromEmail     string   `json:"from_email"`
			AttachmentURL string   `json:"attachment_url"`
			SegmentIDs    []string `json:"segment_ids"`
		}
		if !iam.DecodeJSON(w, r, &req) {
			return
		}
		campaign, err := store.UpdateCampaign(campaignID, req.Subject, req.HTMLBody, req.FromName, req.FromEmail, req.AttachmentURL, req.SegmentIDs)
		if err != nil {
			if strings.Contains(err.Error(), "draft") {
				iam.WriteError(w, http.StatusBadRequest, "invalid_request", "can only update draft campaigns")
				return
			}
			iam.WriteError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, campaign)

	case http.MethodDelete:
		if err := store.DeleteCampaign(campaignID); err != nil {
			if strings.Contains(err.Error(), "draft") {
				iam.WriteError(w, http.StatusBadRequest, "invalid_request", "can only delete draft campaigns")
				return
			}
			iam.WriteError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		iam.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
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
		store := h.tenantStore(r)
		store.RecordOpen(recipientID)
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Write(trackingPixel)
}

func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/unsubscribe/")
	if token == "" {
		iam.WriteError(w, http.StatusBadRequest, "invalid_request", "token required")
		return
	}

	store := h.tenantStore(r)

	switch r.Method {
	case http.MethodGet:
		contact, err := store.GetContactByUnsubscribeToken(token)
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
<form method="POST"><input type="hidden" name="csrf_token" value="%s"><button type="submit">Unsubscribe</button></form>
</div></body></html>`, html.EscapeString(contact.Email), html.EscapeString(tenantctx.CSRFTokenFromContext(r.Context())))

	case http.MethodPost:
		if err := store.UnsubscribeContact(token); err != nil {
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
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	}
}

