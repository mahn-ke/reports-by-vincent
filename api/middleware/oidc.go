package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// NewOIDCVerifier creates a verifier by discovering the provider OIDC configuration.
func NewOIDCVerifier(ctx context.Context, issuer, clientID string) (*oidc.IDTokenVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return provider.Verifier(&oidc.Config{ClientID: clientID}), nil
}

// OIDCMiddleware validates Bearer JWTs issued by the configured OIDC provider.
func OIDCMiddleware(verifier *oidc.IDTokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if _, err := verifier.Verify(r.Context(), strings.TrimPrefix(auth, "Bearer ")); err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
