package iam

import (
	"sync"
)

// TokenRegistry manages per-tenant TokenService instances with lazy key loading.
type TokenRegistry struct {
	store      *Store
	baseIssuer string
	mu         sync.RWMutex
	cache      map[string]*TokenService
	fallback   *TokenService
}

// NewTokenRegistry creates a registry that loads RSA keys from the store per tenant.
func NewTokenRegistry(store *Store, baseIssuer string) *TokenRegistry {
	return &TokenRegistry{
		store:      store,
		baseIssuer: baseIssuer,
		cache:      make(map[string]*TokenService),
	}
}

// NewStaticTokenRegistry creates a registry that always returns the same TokenService.
// Used in tests that don't need per-tenant key isolation.
func NewStaticTokenRegistry(ts *TokenService) *TokenRegistry {
	return &TokenRegistry{
		fallback: ts,
		cache:    make(map[string]*TokenService),
	}
}

// ForTenant returns a TokenService for the given tenant. Keys are lazily loaded and cached.
// The slug is used to compute the per-tenant issuer URL (baseIssuer + "/t/" + slug).
func (tr *TokenRegistry) ForTenant(tenantID, slug string) (*TokenService, error) {
	if tr.fallback != nil {
		return tr.fallback, nil
	}

	tr.mu.RLock()
	ts, ok := tr.cache[tenantID]
	tr.mu.RUnlock()
	if ok {
		return ts, nil
	}

	// Double-checked locking: acquire write lock and check again
	tr.mu.Lock()
	ts, ok = tr.cache[tenantID]
	if ok {
		tr.mu.Unlock()
		return ts, nil
	}
	tr.mu.Unlock()

	scopedStore := tr.store.ForTenant(tenantID)

	issuer := tr.baseIssuer
	if slug != "" {
		issuer = tr.baseIssuer + "/t/" + slug
	}

	// Load all active keys for multi-key support (rotation grace period).
	keys, err := scopedStore.LoadActiveKeys(tenantID)
	if err != nil || len(keys) == 0 {
		// Fallback: load or create single key (backward compat / first run).
		key, err := scopedStore.LoadOrCreateRSAKey()
		if err != nil {
			return nil, err
		}
		ts = NewTokenService(key, issuer)
	} else {
		ts = NewTokenServiceMultiKey(keys, issuer)
	}

	tr.mu.Lock()
	tr.cache[tenantID] = ts
	tr.mu.Unlock()
	return ts, nil
}

// Invalidate removes the cached TokenService for a tenant, forcing a reload on next access.
func (tr *TokenRegistry) Invalidate(tenantID string) {
	tr.mu.Lock()
	delete(tr.cache, tenantID)
	tr.mu.Unlock()
}

// BaseIssuer returns the base issuer URL.
func (tr *TokenRegistry) BaseIssuer() string {
	return tr.baseIssuer
}
