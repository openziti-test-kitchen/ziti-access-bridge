// Package metrics serves ZAB's Prometheus endpoint.
//
// The embedded cloudflared registers its collectors on prometheus.DefaultRegisterer, with a
// tunnel label per Cloudflare Tunnel when cftunnel builds the supervisor. Serve exposes
// everything on that registry at /metrics on one listener, so a process carrying several
// Cloudflare Tunnels is scraped once and split by the tunnel label on the Prometheus side.
package metrics
