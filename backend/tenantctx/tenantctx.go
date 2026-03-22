// Package tenantctx provides tenant context helpers shared by iam, marketing,
// and tenant packages to avoid circular imports.
package tenantctx

import "context"

type idKey struct{}
type slugKey struct{}
type csrfKey struct{}

// WithID stores the tenant ID in the context.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, idKey{}, id)
}

// FromContext retrieves the tenant ID from the context.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(idKey{}).(string)
	return id
}

// WithSlug stores the tenant slug in the context.
func WithSlug(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, slugKey{}, slug)
}

// SlugFromContext retrieves the tenant slug from the context.
func SlugFromContext(ctx context.Context) string {
	s, _ := ctx.Value(slugKey{}).(string)
	return s
}

// WithCSRFToken stores the CSRF token in the context.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfKey{}, token)
}

// CSRFTokenFromContext retrieves the CSRF token from the context.
func CSRFTokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(csrfKey{}).(string)
	return t
}
