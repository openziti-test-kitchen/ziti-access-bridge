// Package sdkpool keeps one OpenZiti SDK context per requester.
//
// ZAB carries no traffic as its own OpenZiti Identity. Each HTTP request is carried onto the OpenZiti overlay
// network as the OpenZiti Identity of the requester the access provider JWT was issued to,
// through an OpenZiti SDK context authenticated with that JWT via an External JWT Signer.
// Building an OpenZiti SDK context costs an authentication, OpenZiti edge router connections,
// and a dial, so a Pool keeps one per requester and reuses it across access provider JWT
// rotations.
//
// New builds a Pool from Options: the OpenZiti controllers, their CA, the config types to
// request, and the two lifetimes below. A Pool entry is keyed by the caller, normally the
// access provider JWT issuer joined with the claim that names the requester. The first
// Pool.Acquire for a key authenticates with the JWT it is given and returns ErrAuthentication
// when the OpenZiti controller rejects it. Later calls for the same key reuse the context,
// record the newest JWT's expiry, and hand the newest JWT to the context so a failed API
// Session refresh re-authenticates with a JWT that is still valid.
//
// The caller verifies every access provider JWT before calling Pool.Acquire. A Pool hit is
// never authorization.
//
// Pool.Run bounds each context's life: a context closes when no Acquire for its key has
// arrived within Options.IdleWindow, and at the latest at the newest JWT expiry it has seen
// plus Options.Grace. Options.OnEvict tells the caller which key closed, so anything the
// caller cached against that context can be released.
package sdkpool
