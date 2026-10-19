// Package cftunnel runs an embedded Cloudflare Tunnel that delivers HTTP requests to an
// http.Handler.
//
// A Cloudflare Tunnel is dialed outbound from the connector to the Cloudflare edge, so the
// connector needs no inbound port. cloudflared is the connector Cloudflare ships, and its
// Cloudflare Tunnel stack is importable. Run builds that stack the way the cloudflared daemon
// does, minus the command line, and registers one catch-all internal ingress rule whose
// origin is Options.Handler. Every HTTP request the Cloudflare edge sends down the Cloudflare
// Tunnel reaches the handler, with the Cloudflare Access JWT header intact, after any
// Options.Middleware has run. Internal ingress rules survive every remote configuration push
// from the Cloudflare dashboard. Run blocks until its context is cancelled, then drains
// in-flight requests for Options.GracePeriod.
//
// The Cloudflare Tunnel token identifies the named Cloudflare Tunnel and is a secret.
// LoadToken reads it from a file or from the TokenEnv environment variable, and ParseToken
// decodes it into the connector credentials. NewLogger builds the zerolog logger cloudflared
// writes to at a level matching ZAB's own.
//
// The handler is attached through cloudflared's exported ingress.ManagementService, the type
// cloudflared uses for its own in-process management origin. Nothing promises that type stays
// exported. If it changes, this package stops compiling rather than failing at runtime.
//
// cloudflared registers Prometheus collectors on prometheus.DefaultRegisterer, and building
// a second Cloudflare Tunnel supervisor in one process would panic on duplicate
// registration. Run therefore swaps prometheus.DefaultRegisterer for one that labels every
// collector with the Cloudflare Tunnel ID while it constructs the supervisor, and serializes
// construction across Cloudflare Tunnels. Collectors cloudflared registers in package init
// carry no such label and stay process-wide.
package cftunnel
