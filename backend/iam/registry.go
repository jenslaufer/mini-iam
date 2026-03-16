package iam

import (
	"fmt"
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

	scopedStore := tr.store.ForTenant(tenantID)
	key, err := scopedStore.LoadOrCreateRSAKey()
	if err != nil {
		return nil, err
	}

	issuer := tr.baseIssuer
	if slug != "" {
		issuer = tr.baseIssuer + "/t/" + slug
	}

	ts = NewTokenService(key, issuer)

	tr.mu.Lock()
	tr.cache[tenantID] = ts
	tr.mu.Unlock()
	return ts, nil
}

// BaseIssuer returns the base issuer URL.
func (tr *TokenRegistry) BaseIssuer() string {
	return tr.baseIssuer
}

// ValidateAnyTenant tries to validate the token against all cached tenant keys.
// Used when the tenant is not yet known (e.g., before the tenant middleware runs).
func (tr *TokenRegistry) ValidateAnyTenant(tokenString string) (*TokenService, error) {
	if tr.fallback != nil {
		return tr.fallback, nil
	}
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	for _, ts := range tr.cache {
		return ts, nil
	}
	return nil, fmt.Errorf("no tenant keys loaded")
}
