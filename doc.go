// Package zab bridges access provider traffic onto an OpenZiti overlay network.
//
// An access provider such as Cloudflare Access fronts a public hostname, sends
// each requester through an identity provider login, and forwards every HTTP
// request to the HTTP service behind it with a signed access provider JWT
// attached. ZAB (Ziti Access Bridge) is that HTTP service. It verifies the
// access provider JWT, authenticates the requester to OpenZiti with that same
// JWT through an External JWT Signer, resolves the request's hostname and path
// to an OpenZiti service through the requester's own OpenZiti SDK context, and
// proxies the request over the OpenZiti overlay network.
//
// ZAB carries no traffic as itself. Its own OpenZiti Identity, enrolled with
// zab enroll, serves only to trust and discover the OpenZiti controllers and to
// administer the ZAB config types. Every OpenZiti SDK context that dials a
// service belongs to the requester the access provider JWT was issued to, so
// OpenZiti Service Policy applies per requester.
//
// Bridge is the http.Handler that performs that flow for one request. It is
// built with New from Options, which carry the parts the subpackages supply:
//
//   - cfaccess verifies Cloudflare Access JWTs against the team's JWKS
//   - sdkpool keeps one OpenZiti SDK context per requester
//   - route reads the ZAB config attached to OpenZiti services and resolves a
//     hostname and path to the service that serves it
//   - cftunnel runs an embedded Cloudflare Tunnel that delivers HTTP requests
//     to a Bridge
//   - metrics serves the Prometheus endpoint
//
// Run composes all of them from a config.File and blocks until its context is
// cancelled. It is the whole program: cmd/zab only loads the configuration file
// and calls Run. An embedder that wants ZAB inside another process calls Run the
// same way, or builds a Bridge directly and mounts it on its own listener.
package zab
