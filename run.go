package zab

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/identity"
	"github.com/openziti/sdk-golang/v2/ziti"
	"github.com/rs/zerolog"
	"github.com/sirupsen/logrus"

	"github.com/openziti-test-kitchen/ziti-access-bridge/cfaccess"
	"github.com/openziti-test-kitchen/ziti-access-bridge/cftunnel"
	"github.com/openziti-test-kitchen/ziti-access-bridge/config"
	"github.com/openziti-test-kitchen/ziti-access-bridge/control"
	"github.com/openziti-test-kitchen/ziti-access-bridge/endpoints"
	"github.com/openziti-test-kitchen/ziti-access-bridge/metrics"
	"github.com/openziti-test-kitchen/ziti-access-bridge/route"
	"github.com/openziti-test-kitchen/ziti-access-bridge/sdkpool"
)

// RunOptions carries what Run needs beyond the configuration file.
type RunOptions struct {
	// Version is reported to the Cloudflare edge as the connector version.
	Version string

	// CloudflaredLog receives cloudflared's own log output. Nil discards it.
	CloudflaredLog *zerolog.Logger
}

// Run bridges Cloudflare Access onto OpenZiti until ctx is done, driven by cfg. It loads
// ZAB's own OpenZiti identity and the endpoint file, authenticates as that identity to keep
// the endpoint file current, builds the Cloudflare Access Verifier, the per-requester
// OpenZiti context Pool, and the Bridge, serves metrics when cfg asks for them, and runs the
// embedded Cloudflare Tunnel with the Bridge as its origin. It blocks and returns nil when
// ctx is cancelled.
func Run(ctx context.Context, cfg *config.File, opts RunOptions) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	token, err := cftunnel.LoadToken(cfg.Cloudflare.TunnelTokenFile)
	if err != nil {
		return err
	}

	id, err := identity.LoadIdentity(cfg.Identity)
	if err != nil {
		return fmt.Errorf("zab identity could not be loaded: %w", err)
	}

	known, err := endpoints.Load(cfg.EndpointFile)
	if err != nil {
		return err
	}
	if len(known.Controllers) == 0 {
		return errors.New("endpoint file names no controllers, enroll first")
	}

	tracker := endpoints.NewTracker(cfg.EndpointFile)

	verifier, err := cfaccess.NewVerifier(ctx, cfg.Cloudflare.TeamDomain, cfg.Cloudflare.AUDTags)
	if err != nil {
		return err
	}

	var bridge *Bridge
	var pool *sdkpool.Pool
	pool, err = sdkpool.New(sdkpool.Options{
		Controllers: known.Controllers,
		CAPool:      id.CA(),
		ConfigTypes: route.ConfigTypeNames(),
		IdleWindow:  cfg.Pool.IdleWindow,
		Grace:       cfg.Pool.Grace,
		OnEvict: func(key string) {
			bridge.Evict(key)
		},
		OnContext: func(zctx ziti.Context) {
			tracker.Track(zctx)
			zctx.Events().AddListener(ziti.EventControllerUrlsUpdated, func(args ...any) {
				if urls, ok := controllerURLs(args); ok {
					pool.SetControllers(urls)
				}
			})
		},
	})
	if err != nil {
		return err
	}

	bridge, err = New(Options{
		Verifier:      verifier,
		Pool:          pool,
		IdentityClaim: cfg.IdentityClaim,
		DialTimeout:   cfg.DialTimeout,
	})
	if err != nil {
		return err
	}

	own, err := ziti.NewContext(&ziti.Config{ZtAPIs: known.Controllers, ID: cfg.Identity})
	if err != nil {
		return fmt.Errorf("openziti context could not be created for the zab identity: %w", err)
	}
	defer own.Close()
	tracker.Track(own)
	own.Events().AddListener(ziti.EventControllerUrlsUpdated, func(args ...any) {
		if urls, ok := controllerURLs(args); ok {
			pool.SetControllers(urls)
		}
	})
	if err := own.Authenticate(); err != nil {
		return fmt.Errorf("zab identity could not authenticate to openziti: %w", err)
	}

	go pool.Run(ctx)

	src := &runStatus{
		version:   opts.Version,
		startedAt: time.Now(),
		cfg:       cfg,
		pool:      pool,
		own:       own,
		stop:      cancel,
	}
	go func() {
		if err := control.Serve(ctx, cfg.SocketFile, src); err != nil {
			pfxlog.Logger().WithError(err).Error("control socket stopped")
		}
	}()

	if cfg.Metrics.Address != "" {
		go func() {
			if err := metrics.Serve(ctx, cfg.Metrics.Address); err != nil {
				pfxlog.Logger().WithError(err).Error("metrics server stopped")
			}
		}()
	}

	pfxlog.Logger().WithFields(logrus.Fields{
		"controllers": known.Controllers,
		"teamDomain":  cfg.Cloudflare.TeamDomain,
	}).Info("starting ziti access bridge")

	err = cftunnel.Run(ctx, cftunnel.Options{
		Token:   token,
		Handler: bridge,
		Version: opts.Version,
		Log:     opts.CloudflaredLog,
	})
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

type runStatus struct {
	version   string
	startedAt time.Time
	cfg       *config.File
	pool      *sdkpool.Pool
	own       ziti.Context
	stop      context.CancelFunc
}

var _ control.Source = (*runStatus)(nil)

func (s *runStatus) Status() control.Status {
	known, err := endpoints.Load(s.cfg.EndpointFile)
	if err != nil {
		known = &endpoints.File{}
	}
	return control.Status{
		Version:          s.version,
		StartedAt:        s.startedAt,
		ConfigFile:       s.cfg.Path,
		Controllers:      known.Controllers,
		ActiveController: activeController(s.own),
		TeamDomain:       s.cfg.Cloudflare.TeamDomain,
		PoolSize:         s.pool.Len(),
	}
}

// activeController names the controller zctx is authenticated against, or the empty string
// when zctx is not a ContextImpl or has no active transport.
func activeController(zctx ziti.Context) (active string) {
	impl, ok := zctx.(*ziti.ContextImpl)
	if !ok || impl.CtrlClt == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			active = ""
		}
	}()
	u := impl.CtrlClt.Url()
	return u.String()
}

func (s *runStatus) Stop() {
	pfxlog.Logger().Info("stop requested over the control socket")
	s.stop()
}

func controllerURLs(args []any) ([]string, bool) {
	if len(args) < 2 {
		return nil, false
	}
	urls, ok := args[1].([]*url.URL)
	if !ok || len(urls) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if u != nil {
			out = append(out, u.String())
		}
	}
	return out, true
}
