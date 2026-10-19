package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SchemeHTTP and SchemeHTTPS are the values Config.Scheme accepts.
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// Config is the ZAB config attached to an OpenZiti service. It is the JSON body of an
// OpenZiti config of the ConfigType config type, and of every earlier type ConfigTypes
// lists.
type Config struct {
	// Routes lists the public hostname and path prefix pairs this OpenZiti service serves.
	// At least one is required.
	Routes []Route `json:"routes"`

	// StripPathPrefix removes the matched path prefix from the request path before the
	// request is forwarded, for applications mounted at the root.
	StripPathPrefix bool `json:"stripPathPrefix,omitempty"`

	// Scheme is what the application behind the OpenZiti service speaks, SchemeHTTP or
	// SchemeHTTPS. It defaults to SchemeHTTP.
	Scheme string `json:"scheme,omitempty"`

	// TLSVerify controls whether the application's TLS certificate is verified when Scheme
	// is SchemeHTTPS. It defaults to true.
	TLSVerify *bool `json:"tlsVerify,omitempty"`

	// HostHeader, when set, replaces the Host header on the forwarded request.
	HostHeader string `json:"hostHeader,omitempty"`

	// StripAccessProviderJWT removes the access provider JWT header before the request is
	// forwarded. By default the header is forwarded to the application.
	StripAccessProviderJWT bool `json:"stripAccessProviderJwt,omitempty"`
}

// Route maps one public hostname and path prefix to the OpenZiti service carrying the Config.
type Route struct {
	// Hostname is the public hostname, matched exactly and case-insensitively.
	Hostname string `json:"hostname"`

	// PathPrefix is the path prefix, matched on path segment boundaries. It defaults to "/".
	PathPrefix string `json:"pathPrefix,omitempty"`
}

// ParseConfig decodes a ZAB config body from an OpenZiti service's config map, applies
// defaults, and validates it.
func ParseConfig(body map[string]any) (*Config, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s config could not be re-encoded: %w", ConfigType, err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("%s config could not be decoded: %w", ConfigType, err)
	}

	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// VerifyTLS reports whether the application's TLS certificate is verified, applying the
// default when TLSVerify is unset.
func (c *Config) VerifyTLS() bool {
	return c.TLSVerify == nil || *c.TLSVerify
}

// Normalize applies defaults, lowercases hostnames, normalizes path prefixes, and reports
// the first thing that makes the Config unusable.
func (c *Config) Normalize() error {
	if len(c.Routes) == 0 {
		return errors.New("zab config has no routes")
	}

	switch c.Scheme {
	case "":
		c.Scheme = SchemeHTTP
	case SchemeHTTP, SchemeHTTPS:
	default:
		return fmt.Errorf("zab config scheme %q is not %s or %s", c.Scheme, SchemeHTTP, SchemeHTTPS)
	}

	for i := range c.Routes {
		r := &c.Routes[i]
		r.Hostname = strings.ToLower(strings.TrimSpace(r.Hostname))
		if r.Hostname == "" {
			return fmt.Errorf("zab config route %d has no hostname", i)
		}
		r.PathPrefix = normalizePrefix(r.PathPrefix)
	}
	return nil
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if len(prefix) > 1 {
		prefix = strings.TrimRight(prefix, "/")
	}
	return prefix
}
