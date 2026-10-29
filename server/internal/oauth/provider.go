package oauth

import (
	"errors"
	"fmt"
	"os"

	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
)

// NewProvider composes exactly the grants docs/oauth.md allows: authorization
// code (with PKCE), refresh token, and revocation. No implicit, password,
// client-credentials, OIDC, PAR or JWT assertion handlers are enabled.
func NewProvider(store *Store, globalSecret []byte, issuer string) fosite.OAuth2Provider {
	cfg := &fosite.Config{
		GlobalSecret:                   globalSecret,
		AccessTokenLifespan:            accessTokenLifetime,
		RefreshTokenLifespan:           refreshTokenLifetime,
		AuthorizeCodeLifespan:          authorizeCodeLifetime,
		EnforcePKCE:                    true,
		EnablePKCEPlainChallengeMethod: false,
		// Non-nil empty: refresh tokens are granted only for explicitly
		// consented scopes, without requiring offline/offline_access.
		RefreshTokenScopes:    []string{},
		AccessTokenIssuer:     issuer,
		TokenURL:              issuer + "/oauth/token",
		SendDebugMessagesToClients: false,
	}
	strategy := compose.NewOAuth2HMACStrategy(cfg)
	return compose.Compose(
		cfg,
		store,
		&compose.CommonStrategy{CoreStrategy: strategy},
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2PKCEFactory,
		compose.OAuth2TokenRevocationFactory,
	)
}

// LoadSecret reads the 0600 secret file and enforces 32+ random bytes.
func LoadSecret(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("oauth secret %s must be 0600 (found %o)", path, info.Mode().Perm())
	}
	secret, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(secret) < 32 {
		return nil, errors.New("oauth secret must be at least 32 bytes")
	}
	return secret, nil
}

// Fixed scopes for this project.
var AllowedScopes = []string{"health:summary", "health:sleep", "health:workouts", "health:raw"}
