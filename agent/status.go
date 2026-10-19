package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openziti/identity"
	edgeapis "github.com/openziti/sdk-golang/v2/edge-apis"

	"github.com/openziti-test-kitchen/ziti-access-bridge/config"
	"github.com/openziti-test-kitchen/ziti-access-bridge/control"
	"github.com/openziti-test-kitchen/ziti-access-bridge/endpoints"
	"github.com/openziti-test-kitchen/ziti-access-bridge/route"
)

const probeTimeout = 5 * time.Second

// Report is everything Status learns about a ZAB installation.
type Report struct {
	Enrollment  Enrollment
	Controllers []Controller
	Running     *control.Status
	RunningErr  string
	ConfigTypes []ConfigTypeState
	ConfigErr   string
}

// Enrollment describes the identity the configuration names.
type Enrollment struct {
	Enrolled bool
	Subject  string
	NotAfter time.Time
	Err      string
}

// Controller is one OpenZiti controller from the endpoint file and whether it answered.
type Controller struct {
	URL     string
	Live    bool
	Version string
	Active  bool
	Err     string
}

// ConfigTypeState pairs a ZAB config type this build supports with what the OpenZiti
// network holds for it.
type ConfigTypeState struct {
	Name          string
	Registered    bool
	SchemaCurrent bool
	ID            string
}

// Status gathers a Report for cfg. It never returns an error for a section that could not
// be inspected; that section's error field says why.
func Status(ctx context.Context, cfg *config.File) *Report {
	r := &Report{}

	id, err := identity.LoadIdentity(cfg.Identity)
	if err != nil {
		r.Enrollment.Err = err.Error()
	} else {
		r.Enrollment = describeEnrollment(id)
	}

	r.Running, err = runningStatus(ctx, cfg)
	if err != nil {
		r.RunningErr = err.Error()
	}

	known, err := endpoints.Load(cfg.EndpointFile)
	if err != nil {
		r.ConfigErr = err.Error()
		return r
	}

	var caPool *x509.CertPool
	if id != nil {
		caPool = id.CA()
	}
	active := ""
	if r.Running != nil {
		active = r.Running.ActiveController
	}
	for _, u := range known.Controllers {
		r.Controllers = append(r.Controllers, probeController(ctx, u, caPool, active))
	}

	if id == nil {
		r.ConfigErr = "identity is not enrolled"
		return r
	}
	r.ConfigTypes, err = configTypeStates(ctx, cfg, known.Controllers, caPool)
	if err != nil {
		r.ConfigErr = err.Error()
	}
	return r
}

func describeEnrollment(id identity.Identity) Enrollment {
	cert := id.Cert()
	if cert == nil || cert.Leaf == nil {
		return Enrollment{Err: "identity has no client certificate"}
	}
	return Enrollment{
		Enrolled: true,
		Subject:  cert.Leaf.Subject.String(),
		NotAfter: cert.Leaf.NotAfter,
	}
}

func runningStatus(ctx context.Context, cfg *config.File) (*control.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return control.NewClient(cfg.SocketFile).Status(ctx)
}

type versionResponse struct {
	Data struct {
		Version string `json:"version"`
	} `json:"data"`
}

func probeController(ctx context.Context, apiURL string, caPool *x509.CertPool, active string) Controller {
	c := Controller{URL: apiURL, Active: apiURL == active}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiURL, "/")+"/version", nil)
	if err != nil {
		c.Err = err.Error()
		return c
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: caPool},
			Proxy:           http.ProxyFromEnvironment,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		c.Err = err.Error()
		return c
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.Err = resp.Status
		return c
	}

	var body versionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		c.Err = fmt.Sprintf("version response could not be decoded: %v", err)
		return c
	}
	c.Live = true
	c.Version = body.Data.Version
	return c
}

func configTypeStates(ctx context.Context, cfg *config.File, controllers []string, caPool *x509.CertPool) ([]ConfigTypeState, error) {
	mgmt, err := managementClient(cfg, controllers, caPool)
	if err != nil {
		return nil, err
	}

	var states []ConfigTypeState
	for _, spec := range route.ConfigTypes() {
		state := ConfigTypeState{Name: spec.Name}
		existing, err := findConfigType(ctx, mgmt, spec.Name)
		if err != nil {
			return states, err
		}
		if existing != nil {
			state.Registered = true
			state.ID = *existing.ID
			state.SchemaCurrent = schemasEqual(existing.Schema, spec.Schema)
		}
		states = append(states, state)
	}
	return states, nil
}

// managementClient authenticates to the Edge Management API with ZAB's own identity.
func managementClient(cfg *config.File, controllers []string, caPool *x509.CertPool) (*edgeapis.ManagementApiClient, error) {
	if len(controllers) == 0 {
		return nil, errors.New("endpoint file names no controllers")
	}

	var urls []*url.URL
	for _, c := range controllers {
		u, err := url.Parse(strings.Replace(c, edgeapis.ClientApiPath, edgeapis.ManagementApiPath, 1))
		if err != nil {
			return nil, fmt.Errorf("controller url %q could not be parsed: %w", c, err)
		}
		urls = append(urls, u)
	}

	client := edgeapis.NewManagementApiClient(urls, caPool, nil)
	creds := edgeapis.NewIdentityCredentialsFromConfig(cfg.Identity)
	if _, err := client.Authenticate(creds, nil); err != nil {
		return nil, fmt.Errorf("zab identity could not authenticate to the management api: %w", err)
	}
	return client, nil
}

func schemasEqual(a, b any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var an, bn any
	if json.Unmarshal(aj, &an) != nil || json.Unmarshal(bj, &bn) != nil {
		return false
	}
	ac, _ := json.Marshal(an)
	bc, _ := json.Marshal(bn)
	return string(ac) == string(bc)
}
