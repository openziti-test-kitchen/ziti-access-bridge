// Command zab is the command line expression of Ziti Access Bridge.
//
// Every command loads the configuration file, config.yml in the working directory unless
// --config names another, and hands it to the root zab package or one of its subpackages.
// Nothing else lives here. Every reusable capability belongs to those packages.
//
//	zab run                       run the bridge
//	zab enroll <enroll.jwt>       enroll ZAB's OpenZiti identity and record the controllers
//	zab agent status              enrollment, controller liveness, the running zab, config types
//	zab agent register-configs    register or update the zab config types on the controller
//	zab agent service set         create or replace the zab config on a service
//	zab agent service show        print the zab config on a service
//	zab agent service list        list the services that carry a zab config
//	zab agent stop                ask a running zab to shut down
//
// The files are main.go for the entry point and command registration, run.go, enroll.go,
// agent.go, and service.go for each command's definition.
//
// The Cloudflare Tunnel token is a secret. It is read from the file the configuration names
// or from the ZAB_CLOUDFLARE_TUNNEL_TOKEN environment variable, never from a flag.
package main
