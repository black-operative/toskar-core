package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ErrAPIKeyRequired means a non-loopback listener has no API key configured.
var ErrAPIKeyRequired = contracts.NewError("API_KEY_REQUIRED", nil, errors.New("create an API key before listening beyond loopback"))

// ErrAPIKeyInURL means the caller put a credential in the request URL.
var ErrAPIKeyInURL = contracts.NewError("API_KEY_IN_URL", nil, errors.New("send the API key as Authorization: Bearer, not in the URL"))

// BearerToken reads an API key from the Authorization header.
// Keys in the query string are rejected and are not accepted as credentials.
func BearerToken(r *http.Request) (string, error) {
	q := r.URL.Query()
	for _, name := range []string{"api_key", "access_token", "token", "key"} {
		if q.Get(name) != "" {
			return "", ErrAPIKeyInURL
		}
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("authorization required")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", fmt.Errorf("authorization required")
	}
	return token, nil
}
