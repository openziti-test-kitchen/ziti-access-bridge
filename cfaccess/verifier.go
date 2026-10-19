package cfaccess

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Header is the HTTP request header Cloudflare Access attaches the Cloudflare Access JWT to.
const Header = "Cf-Access-Jwt-Assertion"

// ErrAudience is returned by Verifier.Verify when no audience in the Cloudflare Access JWT
// matches an AUD tag the Verifier accepts.
var ErrAudience = errors.New("cloudflare access jwt audience matches no accepted aud tag")

// Claims is the verified content of a Cloudflare Access JWT.
type Claims struct {
	// Raw is the compact serialization of the JWT as it arrived.
	Raw string

	// Issuer is the iss claim, the Cloudflare Access team URL.
	Issuer string

	// Subject is the sub claim.
	Subject string

	// Audience is the aud claim, the AUD tags of the Cloudflare Access applications the JWT is valid for.
	Audience []string

	// Expiry is the exp claim.
	Expiry time.Time

	// All holds every claim in the JWT payload, keyed by claim name, for claims Claims does not name.
	All map[string]any
}

// String returns the claim named name as a string, or the empty string when the claim is
// absent or not a string.
func (c *Claims) String(name string) string {
	v, _ := c.All[name].(string)
	return v
}

// Verifier verifies Cloudflare Access JWTs for one Cloudflare Access team and a set of
// Cloudflare Access application AUD tags.
type Verifier struct {
	issuer   string
	audTags  map[string]struct{}
	verifier *oidc.IDTokenVerifier
}

// NewVerifier builds a Verifier for the Cloudflare Access team at teamDomain, such as
// example.cloudflareaccess.com, accepting JWTs whose audience includes one of audTags.
// The JWKS is fetched lazily and cached, and ctx bounds every JWKS fetch.
func NewVerifier(ctx context.Context, teamDomain string, audTags []string) (*Verifier, error) {
	if teamDomain == "" {
		return nil, errors.New("cloudflare access team domain is required")
	}
	if len(audTags) == 0 {
		return nil, errors.New("at least one cloudflare access aud tag is required")
	}

	issuer := "https://" + teamDomain
	keySet := oidc.NewRemoteKeySet(ctx, issuer+"/cdn-cgi/access/certs")
	verifier := oidc.NewVerifier(issuer, keySet, &oidc.Config{SkipClientIDCheck: true})

	accepted := make(map[string]struct{}, len(audTags))
	for _, tag := range audTags {
		accepted[tag] = struct{}{}
	}

	return &Verifier{
		issuer:   issuer,
		audTags:  accepted,
		verifier: verifier,
	}, nil
}

// Issuer returns the issuer the Verifier requires, the Cloudflare Access team URL.
func (v *Verifier) Issuer() string {
	return v.issuer
}

// Verify checks raw's signature against the team JWKS, its issuer, its expiry, and its
// audience, and returns the verified Claims.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("cloudflare access jwt verification failed: %w", err)
	}

	accepted := false
	for _, aud := range token.Audience {
		if _, ok := v.audTags[aud]; ok {
			accepted = true
			break
		}
	}
	if !accepted {
		return nil, ErrAudience
	}

	all := map[string]any{}
	if err := token.Claims(&all); err != nil {
		return nil, fmt.Errorf("cloudflare access jwt claims could not be decoded: %w", err)
	}

	return &Claims{
		Raw:      raw,
		Issuer:   token.Issuer,
		Subject:  token.Subject,
		Audience: token.Audience,
		Expiry:   token.Expiry,
		All:      all,
	}, nil
}
