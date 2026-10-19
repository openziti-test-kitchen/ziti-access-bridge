package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openziti/identity"
	"gopkg.in/yaml.v3"
)

// DefaultPath is the configuration file ZAB loads when none is named: config.yml in the
// working directory.
const DefaultPath = "config.yml"

// DefaultEndpointFile is the endpoint file used when the configuration names none, relative
// to the configuration file.
const DefaultEndpointFile = "endpoints.yml"

// DefaultSocketFile is the control socket used when the configuration names none, relative
// to the configuration file.
const DefaultSocketFile = "zab.sock"

// File is the ZAB configuration file.
type File struct {
	// Identity is the standard OpenZiti identity section: the key, certificate, and CA
	// bundle ZAB authenticates to the OpenZiti controllers with. Enrollment writes them.
	Identity identity.Config `yaml:"identity"`

	// EndpointFile records what ZAB learns about the OpenZiti network it connects to, the
	// controller addresses first. Enrollment writes it and every connection keeps it current.
	EndpointFile string `yaml:"endpointFile"`

	// SocketFile is the unix socket a running ZAB listens on for the zab agent command.
	SocketFile string `yaml:"socketFile"`

	// Cloudflare holds the Cloudflare Access and Cloudflare Tunnel settings.
	Cloudflare Cloudflare `yaml:"cloudflare"`

	// IdentityClaim names the Cloudflare Access JWT claim the External JWT Signer maps to an
	// OpenZiti Identity. It defaults to email.
	IdentityClaim string `yaml:"identityClaim"`

	// Pool bounds each requester's OpenZiti SDK context.
	Pool Pool `yaml:"pool"`

	// DialTimeout bounds each OpenZiti service dial.
	DialTimeout time.Duration `yaml:"dialTimeout"`

	// Metrics configures the Prometheus endpoint.
	Metrics Metrics `yaml:"metrics"`

	// LogLevel is debug, info, warn, or error. It defaults to info.
	LogLevel string `yaml:"logLevel"`

	// Path is the absolute path the File was loaded from. Load sets it.
	Path string `yaml:"-"`
}

// Cloudflare holds the Cloudflare Access and Cloudflare Tunnel settings.
type Cloudflare struct {
	// TeamDomain is the Cloudflare Access team domain, such as example.cloudflareaccess.com.
	TeamDomain string `yaml:"teamDomain"`

	// AUDTags lists the Cloudflare Access application AUD tags to accept.
	AUDTags []string `yaml:"audTags"`

	// TunnelTokenFile holds the Cloudflare Tunnel token. When empty the token is read from
	// the ZAB_CLOUDFLARE_TUNNEL_TOKEN environment variable.
	TunnelTokenFile string `yaml:"tunnelTokenFile"`
}

// Pool bounds each requester's OpenZiti SDK context.
type Pool struct {
	// IdleWindow closes a requester's context after this long without a request.
	IdleWindow time.Duration `yaml:"idleWindow"`

	// Grace closes a requester's context this long after the newest Cloudflare Access JWT
	// expiry seen.
	Grace time.Duration `yaml:"grace"`
}

// Metrics configures the Prometheus endpoint.
type Metrics struct {
	// Address is the listen address. Empty disables the endpoint.
	Address string `yaml:"address"`
}

// Load reads the configuration file at path, applies defaults, and resolves relative paths
// against the file's directory.
func Load(path string) (*File, error) {
	if path == "" {
		path = DefaultPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("configuration path could not be resolved: %w", err)
	}

	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("configuration file could not be read: %w", err)
	}

	f := &File{}
	if err := yaml.Unmarshal(content, f); err != nil {
		return nil, fmt.Errorf("configuration file %s is not valid yaml: %w", abs, err)
	}
	f.Path = abs
	f.applyDefaults()
	f.resolvePaths()
	return f, nil
}

// Dir returns the directory the File was loaded from, which relative paths resolve against.
func (f *File) Dir() string {
	return filepath.Dir(f.Path)
}

// Validate reports the first setting that is missing or unusable for running ZAB.
func (f *File) Validate() error {
	if err := f.Identity.ValidateForClient(); err != nil {
		return fmt.Errorf("identity is incomplete, enroll first: %w", err)
	}
	if f.Cloudflare.TeamDomain == "" {
		return errors.New("cloudflare.teamDomain is required")
	}
	if len(f.Cloudflare.AUDTags) == 0 {
		return errors.New("cloudflare.audTags needs at least one aud tag")
	}
	return nil
}

func (f *File) applyDefaults() {
	if f.EndpointFile == "" {
		f.EndpointFile = DefaultEndpointFile
	}
	if f.SocketFile == "" {
		f.SocketFile = DefaultSocketFile
	}
	if f.LogLevel == "" {
		f.LogLevel = "info"
	}
}

func (f *File) resolvePaths() {
	dir := f.Dir()
	f.EndpointFile = resolve(dir, f.EndpointFile)
	f.SocketFile = resolve(dir, f.SocketFile)
	f.Cloudflare.TunnelTokenFile = resolve(dir, f.Cloudflare.TunnelTokenFile)
	f.Identity.Key = resolveIdentity(dir, f.Identity.Key)
	f.Identity.Cert = resolveIdentity(dir, f.Identity.Cert)
	f.Identity.CA = resolveIdentity(dir, f.Identity.CA)
	f.Identity.ServerCert = resolveIdentity(dir, f.Identity.ServerCert)
	f.Identity.ServerKey = resolveIdentity(dir, f.Identity.ServerKey)
}

func resolve(dir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// resolveIdentity resolves an identity library address. A plain path or a file: address is
// a file and resolves against dir. A pem: address or an engine address is left alone.
func resolveIdentity(dir, addr string) string {
	switch {
	case addr == "":
		return addr
	case strings.HasPrefix(addr, identity.StorageFile+":"):
		return identity.StorageFile + ":" + resolve(dir, strings.TrimPrefix(addr, identity.StorageFile+":"))
	case strings.HasPrefix(addr, identity.StoragePem+":"):
		return addr
	case filepath.IsAbs(addr):
		return addr
	case strings.Contains(addr, ":"):
		return addr
	default:
		return filepath.Join(dir, addr)
	}
}

// IdentityFilePath returns the file an identity library address names, or an error when the
// address is not a file, such as a pem: address or a hardware engine address.
func IdentityFilePath(addr string) (string, error) {
	switch {
	case addr == "":
		return "", errors.New("identity address is empty")
	case strings.HasPrefix(addr, identity.StorageFile+":"):
		return strings.TrimPrefix(addr, identity.StorageFile+":"), nil
	case strings.HasPrefix(addr, identity.StoragePem+":"):
		return "", errors.New("identity address is inline pem, not a file")
	case filepath.IsAbs(addr):
		return addr, nil
	case strings.Contains(addr, ":"):
		return "", fmt.Errorf("identity address %q is not a file", addr)
	default:
		return addr, nil
	}
}
