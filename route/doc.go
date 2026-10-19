// Package route resolves an HTTP request's hostname and path to the OpenZiti service that
// serves it.
//
// Routing lives on the OpenZiti controller, not on ZAB. An operator attaches a Config, an
// OpenZiti config of the ConfigType config type, to each OpenZiti service that ZAB should
// carry HTTP requests to. ZAB config types are versioned by name, zab-v1.0.0 and so on, and
// ConfigTypes lists every version this build reads together with the JSON schema the
// OpenZiti controller enforces on it. The zab agent registers those config types with the
// controller. A Config lists the public hostnames and path prefixes the service
// serves as Routes, and how ZAB should speak to the application behind it: the scheme, TLS
// verification, a Host header override, and whether to strip the path prefix or the access
// provider JWT header. ParseConfig decodes and validates one Config from a service's config
// map. ZAB reads those configs through the requester's own OpenZiti SDK context, so a
// requester can only be routed to an OpenZiti service their OpenZiti Identity may dial.
//
// NewTable builds a Table from the OpenZiti services one requester may dial. Table.Resolve
// matches the hostname exactly and then picks the longest matching path prefix across every
// service, returning a Match that names the service, its Config, and the Route that won.
// Two services carrying the same hostname with different path prefixes is the ordinary case.
// A hostname and path with no route is ErrNoRoute, and two services carrying the same
// hostname and the same path prefix is an AmbiguousRouteError, an operator misconfiguration
// that Resolve reports rather than hides.
package route
