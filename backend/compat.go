package main

// compat.go provides type aliases and wrapper functions so that main_test.go
// (which stays in package main) can reference types and functions without
// modification. This is the coupling bridge between the flat test naming and
// the new sub-package structure.

import (
	"crypto/rsa"
	"net/http"

	"github.com/jenslaufer/launch-kit/iam"
	"github.com/jenslaufer/launch-kit/marketing"
)

// --- Type aliases for simple response types ---

type TokenService = iam.TokenService
type TokenRegistry = iam.TokenRegistry
type Mailer = marketing.Mailer
type TokenResponse = iam.TokenResponse
type ErrorResponse = iam.ErrorResponse
type UserInfoResponse = iam.UserInfoResponse
type ClientCreateResponse = iam.ClientCreateResponse
type OIDCDiscovery = iam.OIDCDiscovery
type LogMailer = marketing.LogMailer
type CampaignSender = marketing.CampaignSender

// --- Store is a unified facade over iam.Store and marketing.Store ---
// Tests use the same Store variable for both IAM methods (SeedAdmin) and
// marketing methods (GetContactByEmail, GetCampaignRecipients).

type Store struct {
	*iam.Store
	mkt *marketing.Store
}

// NewStore creates a Store backed by a migrated database, exposing both
// IAM and marketing methods through a unified facade.
func NewStore(dbPath string) (*Store, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	iamStore := iam.NewStore(db)
	mktStore := marketing.NewStore(db)
	return &Store{Store: iamStore, mkt: mktStore}, nil
}

// ForTenant returns a Store scoped to the given tenant.
func (s *Store) ForTenant(tenantID string) *Store {
	return &Store{Store: s.Store.ForTenant(tenantID), mkt: s.mkt.ForTenant(tenantID)}
}

// Marketing methods delegated to the marketing store
func (s *Store) GetContactByEmail(email string) (*marketing.Contact, error) {
	return s.mkt.GetContactByEmail(email)
}

func (s *Store) GetCampaignRecipients(campaignID string) ([]marketing.CampaignRecipient, error) {
	return s.mkt.GetCampaignRecipients(campaignID)
}

// --- NewTokenService wraps iam.NewTokenService ---

func NewTokenService(key *rsa.PrivateKey, issuer string) *iam.TokenService {
	return iam.NewTokenService(key, issuer)
}

// --- Handler is a facade combining iam.Handler and marketing.Handler ---
//
// The test accesses h.sender directly (e.g. h.sender = sender), so sender
// is a public field. Campaign methods call syncSender() before delegating to
// ensure the marketing handler always has the current sender.

type Handler struct {
	iam      *iam.Handler
	mkt      *marketing.Handler
	sender   *marketing.CampaignSender
	registry *iam.TokenRegistry
}

func NewHandler(store *Store, tokens *iam.TokenService, issuer string) *Handler {
	// Create a static registry that always returns this token service,
	// preserving backwards compatibility with tests that use a single key.
	registry := iam.NewStaticTokenRegistry(tokens)
	iamH := iam.NewHandler(store.Store, registry, issuer)
	mktH := marketing.NewHandler(store.mkt, store.Store, registry)
	return &Handler{
		iam:      iamH,
		mkt:      mktH,
		registry: registry,
	}
}

// syncSender propagates h.sender to the marketing handler before any call that
// needs it. This handles the test pattern: h.sender = sender (direct assignment).
func (h *Handler) syncSender() {
	if h.sender != nil {
		h.mkt.SetSender(h.sender)
	}
}

// IAM handler methods
func (h *Handler) Health(w http.ResponseWriter, r *http.Request)     { h.iam.Health(w, r) }
func (h *Handler) Register(w http.ResponseWriter, r *http.Request)   { h.iam.Register(w, r) }
func (h *Handler) Login(w http.ResponseWriter, r *http.Request)      { h.iam.Login(w, r) }
func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request)  { h.iam.Authorize(w, r) }
func (h *Handler) Token(w http.ResponseWriter, r *http.Request)      { h.iam.Token(w, r) }
func (h *Handler) UserInfo(w http.ResponseWriter, r *http.Request)   { h.iam.UserInfo(w, r) }
func (h *Handler) JWKS(w http.ResponseWriter, r *http.Request)       { h.iam.JWKS(w, r) }
func (h *Handler) Discovery(w http.ResponseWriter, r *http.Request)  { h.iam.Discovery(w, r) }
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request)     { h.iam.Revoke(w, r) }
func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) { h.iam.CreateClient(w, r) }
func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request)  { h.iam.AdminListUsers(w, r) }
func (h *Handler) AdminUserByID(w http.ResponseWriter, r *http.Request)   { h.iam.AdminUserByID(w, r) }
func (h *Handler) AdminListClients(w http.ResponseWriter, r *http.Request) { h.iam.AdminListClients(w, r) }
func (h *Handler) AdminDeleteClient(w http.ResponseWriter, r *http.Request) { h.iam.AdminDeleteClient(w, r) }
func (h *Handler) Activate(w http.ResponseWriter, r *http.Request)   { h.iam.Activate(w, r) }

// Marketing handler methods
func (h *Handler) AdminContacts(w http.ResponseWriter, r *http.Request) {
	h.mkt.AdminContacts(w, r)
}
func (h *Handler) AdminContactByID(w http.ResponseWriter, r *http.Request) {
	h.mkt.AdminContactByID(w, r)
}
func (h *Handler) AdminImportContacts(w http.ResponseWriter, r *http.Request) {
	h.mkt.AdminImportContacts(w, r)
}
func (h *Handler) AdminSegments(w http.ResponseWriter, r *http.Request) {
	h.mkt.AdminSegments(w, r)
}
func (h *Handler) AdminSegmentByID(w http.ResponseWriter, r *http.Request) {
	h.mkt.AdminSegmentByID(w, r)
}
func (h *Handler) AdminCampaigns(w http.ResponseWriter, r *http.Request) {
	h.syncSender()
	h.mkt.AdminCampaigns(w, r)
}
func (h *Handler) AdminCampaignByID(w http.ResponseWriter, r *http.Request) {
	h.syncSender()
	h.mkt.AdminCampaignByID(w, r)
}
func (h *Handler) TrackOpen(w http.ResponseWriter, r *http.Request) {
	h.mkt.TrackOpen(w, r)
}
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	h.mkt.Unsubscribe(w, r)
}

// --- NewCampaignSender wraps marketing.NewCampaignSender ---
// The store parameter is *iam.Store (as tests pass the shared store);
// we create a marketing.Store from the same DB.

func NewCampaignSender(store *Store, mailer marketing.Mailer, issuer string, rateMS int) *marketing.CampaignSender {
	return marketing.NewCampaignSender(store.mkt, mailer, issuer, rateMS)
}
