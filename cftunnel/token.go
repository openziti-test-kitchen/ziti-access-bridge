package cftunnel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cloudflare/cloudflared/connection"
)

// TokenEnv is the environment variable LoadToken reads the Cloudflare Tunnel token from when
// no file is given.
const TokenEnv = "ZAB_CLOUDFLARE_TUNNEL_TOKEN"

// LoadToken reads the Cloudflare Tunnel token from file, or from TokenEnv when file is empty.
// The token is a secret, which is why it never arrives as a command line value.
func LoadToken(file string) (string, error) {
	if file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("cloudflare tunnel token file could not be read: %w", err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	token := strings.TrimSpace(os.Getenv(TokenEnv))
	if token == "" {
		return "", errors.New("cloudflare tunnel token is required: set " + TokenEnv + " or give a token file")
	}
	return token, nil
}

// ParseToken decodes a Cloudflare Tunnel token, the base64 JSON value the Cloudflare
// dashboard issues for a named Cloudflare Tunnel, into the credentials a connector runs with.
func ParseToken(token string) (*connection.TunnelToken, error) {
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return nil, fmt.Errorf("cloudflare tunnel token is not base64: %w", err)
	}

	parsed := &connection.TunnelToken{}
	if err := json.Unmarshal(content, parsed); err != nil {
		return nil, fmt.Errorf("cloudflare tunnel token is not valid json: %w", err)
	}
	if parsed.AccountTag == "" || len(parsed.TunnelSecret) == 0 {
		return nil, fmt.Errorf("cloudflare tunnel token lacks an account tag or tunnel secret")
	}
	return parsed, nil
}
