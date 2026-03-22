package audit

import (
	"net/http"
	"time"

	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/tenantctx"
)

// Handler serves the audit log API.
type Handler struct {
	store            *Store
	iamStore         *iam.Store
	registry         *iam.TokenRegistry
	PlatformTenantID string
}

// NewHandler creates a new audit handler.
func NewHandler(store *Store, iamStore *iam.Store, registry *iam.TokenRegistry, platformTenantID string) *Handler {
	return &Handler{
		store:            store,
		iamStore:         iamStore,
		registry:         registry,
		PlatformTenantID: platformTenantID,
	}
}

// ListAuditLog handles GET /admin/audit-log.
func (h *Handler) ListAuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		iam.WriteError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if _, ok := iam.CheckAdmin(h.registry, h.iamStore, h.PlatformTenantID, w, r); !ok {
		return
	}

	tenantID := tenantctx.FromContext(r.Context())
	scoped := h.store.ForTenant(tenantID)

	q := r.URL.Query()
	f := Filter{
		Action:     q.Get("action"),
		ActorID:    q.Get("actor_id"),
		TargetType: q.Get("target_type"),
		TargetID:   q.Get("target_id"),
	}

	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = &t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = &t
		}
	}

	entries, err := scoped.List(f)
	if err != nil {
		iam.WriteError(w, http.StatusInternalServerError, "server_error", "failed to list audit log")
		return
	}
	if entries == nil {
		entries = []Entry{}
	}
	iam.WriteJSON(w, http.StatusOK, entries)
}
