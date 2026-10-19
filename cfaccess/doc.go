// Package cfaccess verifies Cloudflare Access JWTs.
//
// Cloudflare Access attaches a JWT to every HTTP request it forwards, in the
// header named by Header. The JWT is signed with a key published in the
// Cloudflare Access team's JWKS at https://<team>.cloudflareaccess.com/cdn-cgi/access/certs,
// carries the team domain as its issuer, and carries the AUD (Application
// Audience) tag of the Cloudflare Access application the request passed through
// as its audience. Cloudflare rotates the signing key, so a Verifier fetches
// the JWKS and selects the key by the JWT's kid rather than pinning one key.
//
// NewVerifier builds a Verifier for one team and the AUD tags it accepts.
// Verifier.Verify checks the signature, the issuer, the expiry, and that at
// least one audience matches an accepted AUD tag, and returns Claims: the
// issuer, subject, audience, expiry, the raw JWT, and every other claim by
// name through Claims.String. The AUD tag is what binds a JWT to one Cloudflare
// Access application: a JWT issued for another application in the same team is
// rejected with ErrAudience.
package cfaccess
