package zab

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/sdk-golang/v2/ziti"
	"github.com/sirupsen/logrus"

	"github.com/openziti-test-kitchen/ziti-access-bridge/cfaccess"
	"github.com/openziti-test-kitchen/ziti-access-bridge/route"
	"github.com/openziti-test-kitchen/ziti-access-bridge/sdkpool"
)

// DefaultIdentityClaim and DefaultDialTimeout are the Options defaults.
const (
	DefaultIdentityClaim = "email"
	DefaultDialTimeout   = 10 * time.Second
)

// Options configures a Bridge.
type Options struct {
	// Verifier verifies the Cloudflare Access JWT on every request. Required.
	Verifier *cfaccess.Verifier

	// Pool supplies the OpenZiti SDK context for each requester. Required.
	Pool *sdkpool.Pool

	// IdentityClaim names the Cloudflare Access JWT claim that identifies the requester. It
	// is the claim the External JWT Signer on the OpenZiti controller maps to an OpenZiti
	// Identity, and it keys the Pool. It defaults to DefaultIdentityClaim.
	IdentityClaim string

	// DialTimeout bounds each OpenZiti service dial. It defaults to DefaultDialTimeout.
	DialTimeout time.Duration
}

// Bridge is the http.Handler that carries access provider requests onto the OpenZiti overlay
// network. For each request it verifies the Cloudflare Access JWT, acquires the requester's
// OpenZiti SDK context, resolves the hostname and path to an OpenZiti service through the
// ZAB configs that context can see, and proxies the request over a dial of that service.
type Bridge struct {
	opts       Options
	mu         sync.Mutex
	transports map[string]*http.Transport
}

var _ http.Handler = (*Bridge)(nil)

// New builds a Bridge. It returns an error when opts lacks a Verifier or a Pool.
func New(opts Options) (*Bridge, error) {
	if opts.Verifier == nil {
		return nil, errors.New("a cloudflare access verifier is required")
	}
	if opts.Pool == nil {
		return nil, errors.New("an openziti context pool is required")
	}
	if opts.IdentityClaim == "" {
		opts.IdentityClaim = DefaultIdentityClaim
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = DefaultDialTimeout
	}
	return &Bridge{
		opts:       opts,
		transports: map[string]*http.Transport{},
	}, nil
}

// Evict drops the cached transports for a Pool key. Wire it to sdkpool.Options.OnEvict so
// idle connections over a closed OpenZiti SDK context are released.
func (b *Bridge) Evict(key string) {
	prefix := key + "|"

	b.mu.Lock()
	var dropped []*http.Transport
	for k, t := range b.transports {
		if strings.HasPrefix(k, prefix) {
			dropped = append(dropped, t)
			delete(b.transports, k)
		}
	}
	b.mu.Unlock()

	for _, t := range dropped {
		t.CloseIdleConnections()
	}
}

// ServeHTTP implements http.Handler.
func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := pfxlog.Logger().WithFields(logrus.Fields{
		"host": r.Host,
		"path": r.URL.Path,
	})

	raw := r.Header.Get(cfaccess.Header)
	if raw == "" {
		log.Warn("request carries no cloudflare access jwt")
		http.Error(w, "missing cloudflare access jwt", http.StatusUnauthorized)
		return
	}

	claims, err := b.opts.Verifier.Verify(r.Context(), raw)
	if err != nil {
		log.WithError(err).Warn("cloudflare access jwt rejected")
		http.Error(w, "invalid cloudflare access jwt", http.StatusUnauthorized)
		return
	}

	requester := claims.String(b.opts.IdentityClaim)
	if requester == "" {
		log.WithField("claim", b.opts.IdentityClaim).Warn("cloudflare access jwt has no identity claim")
		http.Error(w, "cloudflare access jwt names no requester", http.StatusUnauthorized)
		return
	}
	log = log.WithField("requester", requester)

	key := claims.Issuer + "|" + requester
	zctx, err := b.opts.Pool.Acquire(key, raw, claims.Expiry)
	if err != nil {
		if errors.Is(err, sdkpool.ErrAuthentication) {
			log.WithError(err).Warn("requester has no openziti identity")
			http.Error(w, "requester is not known to openziti", http.StatusForbidden)
			return
		}
		log.WithError(err).Error("openziti context unavailable")
		http.Error(w, "openziti unavailable", http.StatusBadGateway)
		return
	}

	services, err := zctx.GetServices()
	if err != nil {
		log.WithError(err).Error("openziti services could not be listed")
		http.Error(w, "openziti unavailable", http.StatusBadGateway)
		return
	}

	table, err := route.NewTable(services)
	if err != nil {
		log.WithError(err).Warn("some zab configs could not be parsed")
	}

	match, err := table.Resolve(r.Host, r.URL.Path)
	if err != nil {
		var ambiguous *route.AmbiguousRouteError
		switch {
		case errors.As(err, &ambiguous):
			log.WithField("services", ambiguous.Services).Error("zab route is ambiguous")
			http.Error(w, "route is ambiguous", http.StatusBadGateway)
		default:
			log.Warn("no openziti service routes this hostname and path for the requester")
			http.Error(w, "no route for requester", http.StatusForbidden)
		}
		return
	}
	log = log.WithField("service", match.Service)

	proxy := &httputil.ReverseProxy{
		Rewrite:   rewrite(match),
		Transport: b.transport(key, zctx, match),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.WithError(err).Error("proxy to openziti service failed")
			http.Error(w, "openziti service unreachable", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func rewrite(match *route.Match) func(*httputil.ProxyRequest) {
	cfg := match.Config
	prefix := match.Route.PathPrefix

	return func(pr *httputil.ProxyRequest) {
		pr.SetXForwarded()
		pr.Out.URL.Scheme = cfg.Scheme
		pr.Out.URL.Host = match.Service

		if cfg.HostHeader != "" {
			pr.Out.Host = cfg.HostHeader
		} else {
			pr.Out.Host = pr.In.Host
		}

		if cfg.StripPathPrefix && prefix != "/" {
			pr.Out.URL.Path = stripPrefix(pr.Out.URL.Path, prefix)
			pr.Out.URL.RawPath = stripPrefix(pr.Out.URL.RawPath, prefix)
		}

		if cfg.StripAccessProviderJWT {
			pr.Out.Header.Del(cfaccess.Header)
		}
	}
}

func stripPrefix(path, prefix string) string {
	if path == "" {
		return path
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		return "/"
	}
	return rest
}

func (b *Bridge) transport(key string, zctx ziti.Context, match *route.Match) *http.Transport {
	cfg := match.Config
	serverName := cfg.HostHeader
	if serverName == "" {
		serverName = match.Route.Hostname
	}
	if h, _, err := net.SplitHostPort(serverName); err == nil {
		serverName = h
	}

	cacheKey := strings.Join([]string{key, match.Service, cfg.Scheme, serverName, boolString(cfg.VerifyTLS())}, "|")

	b.mu.Lock()
	defer b.mu.Unlock()

	if t, ok := b.transports[cacheKey]; ok {
		return t
	}

	service := match.Service
	timeout := b.opts.DialTimeout
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return zctx.DialContextWithOptions(ctx, service, &ziti.DialOptions{ConnectTimeout: timeout})
		},
		TLSClientConfig: &tls.Config{
			ServerName:         serverName,
			InsecureSkipVerify: !cfg.VerifyTLS(),
		},
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: time.Second,
	}
	b.transports[cacheKey] = t
	return t
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
