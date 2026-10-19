// Package agent inspects and administers a ZAB installation from its configuration file.
//
// Status gathers a Report without needing a running ZAB: whether the identity in the
// configuration is enrolled and when its certificate expires, whether each OpenZiti
// controller in the endpoint file answers and what version it runs, what a running ZAB
// reports over its control socket when one is up, including the controller it is
// authenticated against, and which ZAB config type versions this build supports against
// which of them the OpenZiti network has registered. Every section of a Report carries its
// own error, so one unreachable part does not hide the rest.
//
// RegisterConfigTypes creates each ZAB config type the build supports on the OpenZiti
// controller, or brings its JSON schema up to date, through the Edge Management API.
//
// SetServiceConfig attaches a route.Config to an OpenZiti service as a config of the
// current ZAB config type, creating the service bare when it does not exist and creating
// or updating the config otherwise. ShowServiceConfig and ListServiceConfigs read those
// configs back. That is the whole of ZAB's service management: policies, other configs,
// and deletion stay with the OpenZiti CLI.
//
// The config type check in Status, RegisterConfigTypes, and the service functions
// authenticate with ZAB's own identity, which therefore needs admin rights for them.
// Running the bridge does not.
package agent
