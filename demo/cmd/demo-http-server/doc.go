// Command demo-http-server is the application at the far end of a ZAB demo.
//
// It listens on an interface and port. Behind an OpenZiti tunneler it is the target of a ZAB
// config, so the chain browser, Cloudflare Access, Cloudflare Tunnel, ZAB, OpenZiti,
// demo-http-server can be seen end to end.
//
// The root path serves a dark themed page. Without a valid Cloudflare Access JWT it shows
// an error with 401. With one it greets the requester by email with the current time in
// the browser's time zone, lists whether the JWT was found and verified, and shows the JWT
// encoded beside its decoded header and payload. /echo answers with a JSON echo of the
// method, path, host, remote address, headers, and verified claims. /healthz answers 200
// and skips verification.
//
// With --cloudflare-team-domain and --cloudflare-aud set the server verifies the Cloudflare
// Access JWT on every request with the cfaccess package. That proves the JWT crossed
// OpenZiti intact and that nothing reaches the application without passing Cloudflare
// Access. Without those flags the page decodes the JWT without verifying it and says so.
//
// --tls-cert and --tls-key serve HTTPS, so the scheme and tlsVerify settings of a ZAB
// config can be exercised.
package main
