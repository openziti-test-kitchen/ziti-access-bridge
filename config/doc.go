// Package config loads the ZAB configuration file.
//
// ZAB runs from one YAML file, config.yml in the working directory by default, whose shape
// is File. The file opens with a standard OpenZiti identity section, the identity.Config
// from the openziti identity library, holding the key, certificate, and CA bundle that
// enrollment writes and that ZAB authenticates to the OpenZiti controllers with. The rest
// of the file is ZAB's own settings: the endpoint file that records the OpenZiti controller
// addresses, the Cloudflare Access team and AUD tags, the Cloudflare Tunnel token file, and
// the pool, dial, metrics, and logging settings.
//
// Load reads the file, applies defaults, and resolves every relative path in it against the
// directory the file lives in, so a configuration directory can be moved as a unit. The
// example in etc/example.config.yml at the repository root documents every setting.
package config
