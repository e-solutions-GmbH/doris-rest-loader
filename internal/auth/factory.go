package auth

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// NewAdapter constructs the appropriate Adapter for the given AuthConfig.
// tlsSkipVerify is forwarded to any HTTP client created internally (e.g. for
// the preflight request). It should match the source endpoint's TLS setting.
func NewAdapter(cfg config.AuthConfig, tlsSkipVerify bool) (Adapter, error) {
	switch cfg.Type {
	case "noauth", "":
		return &NoAuth{}, nil

	case "basic":
		return &BasicAuth{Username: cfg.Username, Password: cfg.Password}, nil

	case "bearer":
		return &BearerAuth{Token: cfg.Token}, nil

	case "preflight_bearer":
		client := newHTTPClient(tlsSkipVerify)
		return NewPreflightBearer(cfg.URL, cfg.Username, cfg.Password, client), nil

	case "oauth2":
		grant := cfg.GrantType
		if grant == "" {
			grant = "client_credentials"
		}
		switch grant {
		case "client_credentials":
			return NewOAuth2ClientCredentials(cfg.ClientID, cfg.ClientSecret, cfg.TokenURL, cfg.Scopes), nil
		case "password":
			return NewOAuth2Password(
				cfg.ClientID, cfg.ClientSecret, cfg.TokenURL,
				cfg.Username, cfg.Password, cfg.Scopes,
			), nil
		default:
			return nil, fmt.Errorf(
				"auth: unsupported oauth2 grant_type %q (supported: client_credentials, password)",
				grant,
			)
		}

	default:
		return nil, fmt.Errorf(
			"auth: unknown type %q (supported: noauth, basic, bearer, preflight_bearer, oauth2)",
			cfg.Type,
		)
	}
}

// newHTTPClient returns a plain HTTP client with optional TLS verification bypass.
func newHTTPClient(tlsSkipVerify bool) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: tlsSkipVerify}, //nolint:gosec
		},
	}
}
