// Package endpoints records what ZAB learns about the OpenZiti network it connects to.
//
// The endpoint file, named by the endpointFile setting in the ZAB configuration, is a YAML
// file of the File shape. Its first content is the OpenZiti controller addresses: every Edge
// Client API URL ZAB may authenticate against, so an HA controller set is reachable even
// when the controller enrollment named is down.
//
// Enrollment writes the file with the controller the enrollment JWT named. From then on the
// file is kept current by Track, which listens to a ziti.Context for the controller set the
// SDK discovers after each authentication and rewrites the file whenever a controller is
// added or removed. Load and Save read and write the file directly.
package endpoints
