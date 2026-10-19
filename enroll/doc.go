// Package enroll enrolls ZAB's OpenZiti identity from an enrollment JWT.
//
// An OpenZiti administrator creates an identity for ZAB and hands over its one-time
// enrollment JWT. Enroll consumes that JWT: it generates a private key, builds a CSR
// (certificate signing request), presents the CSR with the JWT to the controller the JWT
// names, and receives the identity certificate and the controller CA bundle. The key,
// certificate, and CA bundle are written to the files the configuration's identity section
// names, and the controller address is written to the endpoint file. Enroll then
// authenticates once with the new identity so the full controller set is discovered and the
// endpoint file reflects an HA controller set from the start.
//
// The configuration file itself is not rewritten. It already says where the identity files
// live, and Enroll fills those slots.
package enroll
