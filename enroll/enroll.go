package enroll

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/identity"
	"github.com/openziti/sdk-golang/v2/ziti"
	sdkenroll "github.com/openziti/sdk-golang/v2/ziti/enroll"

	"github.com/openziti-test-kitchen/ziti-access-bridge/config"
	"github.com/openziti-test-kitchen/ziti-access-bridge/endpoints"
)

// KeyAlgorithm is the private key algorithm Enroll generates.
const KeyAlgorithm = "EC"

// Result reports where Enroll wrote the identity and which controllers it recorded.
type Result struct {
	KeyFile      string
	CertFile     string
	CAFile       string
	EndpointFile string
	Controllers  []string
}

// Enroll consumes the enrollment JWT at jwtPath, writes the resulting key, certificate, and
// CA bundle to the files cfg's identity section names, records the controllers in cfg's
// endpoint file, and returns where everything went.
func Enroll(jwtPath string, cfg *config.File) (*Result, error) {
	keyFile, certFile, caFile, err := identityFiles(cfg)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(jwtPath)
	if err != nil {
		return nil, fmt.Errorf("enrollment jwt could not be read: %w", err)
	}
	jwtString := strings.TrimSpace(string(raw))

	claims, token, err := sdkenroll.ParseToken(jwtString)
	if err != nil {
		return nil, fmt.Errorf("enrollment jwt could not be parsed: %w", err)
	}

	var keyAlg ziti.KeyAlgVar
	if err := keyAlg.Set(KeyAlgorithm); err != nil {
		return nil, err
	}

	enrolled, err := sdkenroll.Enroll(sdkenroll.EnrollmentFlags{
		Token:     claims,
		JwtToken:  token,
		JwtString: jwtString,
		KeyAlg:    keyAlg,
	})
	if err != nil {
		return nil, fmt.Errorf("enrollment failed: %w", err)
	}

	if err := writePEM(keyFile, enrolled.ID.Key, 0o600); err != nil {
		return nil, err
	}
	if err := writePEM(certFile, enrolled.ID.Cert, 0o644); err != nil {
		return nil, err
	}
	if err := writePEM(caFile, enrolled.ID.CA, 0o644); err != nil {
		return nil, err
	}

	controllers, err := discoverControllers(cfg.Identity, enrolled.ZtAPI, cfg.EndpointFile)
	if err != nil {
		return nil, err
	}

	return &Result{
		KeyFile:      keyFile,
		CertFile:     certFile,
		CAFile:       caFile,
		EndpointFile: cfg.EndpointFile,
		Controllers:  controllers,
	}, nil
}

func identityFiles(cfg *config.File) (keyFile, certFile, caFile string, err error) {
	if keyFile, err = config.IdentityFilePath(cfg.Identity.Key); err != nil {
		return "", "", "", fmt.Errorf("identity.key: %w", err)
	}
	if certFile, err = config.IdentityFilePath(cfg.Identity.Cert); err != nil {
		return "", "", "", fmt.Errorf("identity.cert: %w", err)
	}
	if caFile, err = config.IdentityFilePath(cfg.Identity.CA); err != nil {
		return "", "", "", fmt.Errorf("identity.ca: %w", err)
	}
	return keyFile, certFile, caFile, nil
}

func writePEM(path, addr string, mode os.FileMode) error {
	content, found := strings.CutPrefix(addr, identity.StoragePem+":")
	if !found {
		return fmt.Errorf("enrollment returned %q for %s, expected inline pem", addr, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s directory could not be created: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("%s could not be written: %w", path, err)
	}
	return nil
}

// discoverControllers writes the enrolling controller to the endpoint file, then
// authenticates with the new identity so the SDK's controller discovery fills in the rest of
// an HA controller set.
func discoverControllers(id identity.Config, ztAPI, endpointFile string) ([]string, error) {
	first, err := url.Parse(ztAPI)
	if err != nil {
		return nil, fmt.Errorf("controller url %q from the enrollment jwt could not be parsed: %w", ztAPI, err)
	}

	tracker := endpoints.NewTracker(endpointFile)
	if err := tracker.Update([]*url.URL{first}); err != nil {
		return nil, err
	}

	zctx, err := ziti.NewContext(&ziti.Config{ZtAPIs: []string{ztAPI}, ID: id})
	if err != nil {
		return nil, fmt.Errorf("openziti context could not be created with the enrolled identity: %w", err)
	}
	defer zctx.Close()

	stop := tracker.Track(zctx)
	defer stop()

	if err := zctx.Authenticate(); err != nil {
		pfxlog.Logger().WithError(err).Warn("enrolled identity could not authenticate yet, endpoint file holds the enrolling controller only")
	}

	f, err := endpoints.Load(endpointFile)
	if err != nil {
		return nil, err
	}
	if len(f.Controllers) == 0 {
		return nil, errors.New("endpoint file holds no controllers after enrollment")
	}
	return f.Controllers, nil
}
