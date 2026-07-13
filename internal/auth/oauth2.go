package auth

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// oauthGrantType names the supported OAuth2 grant types.
type oauthGrantType string

const (
	grantClientCredentials oauthGrantType = "client_credentials"
	// grantPassword uses the Resource Owner Password Credentials grant.
	// Note: This grant type is deprecated in OAuth 2.1. Prefer client_credentials
	// or the authorization_code flow where possible.
	grantPassword oauthGrantType = "password"
)

// OAuth2Auth authenticates using the OAuth2 protocol via golang.org/x/oauth2.
// Supported grant types: client_credentials and password.
//
// The token is fetched once during Prepare and attached to every subsequent
// request by Apply. Token refresh is not handled automatically; restart the
// container to re-authenticate for long-running scenarios.
type OAuth2Auth struct {
	grant    oauthGrantType
	ccConfig *clientcredentials.Config
	pwConfig *oauth2.Config
	username string
	password string
	token    *oauth2.Token
}

// NewOAuth2ClientCredentials creates an OAuth2Auth for the client_credentials grant.
// This is the recommended grant type for machine-to-machine authentication.
func NewOAuth2ClientCredentials(clientID, clientSecret, tokenURL string, scopes []string) *OAuth2Auth {
	return &OAuth2Auth{
		grant: grantClientCredentials,
		ccConfig: &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
			Scopes:       scopes,
		},
	}
}

// NewOAuth2Password creates an OAuth2Auth for the Resource Owner Password Credentials grant.
//
// Deprecated: The password grant is deprecated in OAuth 2.1. Use client_credentials
// or preflight_bearer where possible.
func NewOAuth2Password(clientID, clientSecret, tokenURL, username, password string, scopes []string) *OAuth2Auth {
	return &OAuth2Auth{
		grant: grantPassword,
		pwConfig: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     oauth2.Endpoint{TokenURL: tokenURL},
			Scopes:       scopes,
		},
		username: username,
		password: password,
	}
}

// Prepare fetches the OAuth2 token using the configured grant type.
func (o *OAuth2Auth) Prepare(ctx context.Context) error {
	var err error
	switch o.grant {
	case grantClientCredentials:
		o.token, err = o.ccConfig.Token(ctx)
		if err != nil {
			return fmt.Errorf("oauth2: fetch client_credentials token: %w", err)
		}
	case grantPassword:
		//nolint:staticcheck // PasswordCredentialsToken is deprecated in OAuth 2.1 but still functional.
		o.token, err = o.pwConfig.PasswordCredentialsToken(ctx, o.username, o.password)
		if err != nil {
			return fmt.Errorf("oauth2: fetch password grant token: %w", err)
		}
	default:
		return fmt.Errorf("oauth2: unsupported grant type %q", o.grant)
	}
	return nil
}

// Apply attaches the OAuth2 Bearer token to the outgoing request.
// Returns an error if Prepare has not been called.
func (o *OAuth2Auth) Apply(req *http.Request) error {
	if o.token == nil {
		return fmt.Errorf("oauth2: token not available; call Prepare before Apply")
	}
	o.token.SetAuthHeader(req)
	return nil
}
