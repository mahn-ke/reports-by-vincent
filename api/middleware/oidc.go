package middleware

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// NewOIDCVerifier creates a verifier for Keycloak access tokens by discovering the provider OIDC configuration.
// Keycloak access tokens carry "account"/"realm-management" as audience rather than the client ID,
// so the audience check is skipped here and the authorized party (azp) is checked in the middleware instead.
func NewOIDCVerifier(ctx context.Context, issuer string) (*oidc.IDTokenVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return provider.Verifier(&oidc.Config{SkipClientIDCheck: true}), nil
}

type accessTokenClaims struct {
	AuthorizedParty string `json:"azp"`
	ResourceAccess  map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
}

// OIDCMiddleware validates Bearer access tokens issued by the configured OIDC provider for clientID
// and only admits admins (realm-management/manage-users role), mirroring the web app's login check.
func OIDCMiddleware(verifier *oidc.IDTokenVerifier, clientID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			token, err := verifier.Verify(r.Context(), strings.TrimPrefix(auth, "Bearer "))
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var claims accessTokenClaims
			if err := token.Claims(&claims); err != nil || claims.AuthorizedParty != clientID {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !slices.Contains(claims.ResourceAccess["realm-management"].Roles, "manage-users") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
