package cftunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"runtime"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/ingress/middleware"
	"github.com/cloudflare/cloudflared/ingress/origins"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// Defaults for Options, matching the cloudflared daemon's own flag defaults.
const (
	DefaultGracePeriod   = 30 * time.Second
	DefaultHAConnections = 4
	DefaultVersion       = "zab"

	defaultRetries                 = 5
	defaultMaxEdgeAddrRetries      = 8
	defaultRPCTimeout              = 5 * time.Second
	defaultQUICConnFlowControl     = 30 * (1 << 20)
	defaultQUICStreamFlowControl   = 6 * (1 << 20)
	defaultEdgeTLSConfigsPerProtos = 2
)

// Options configures a Cloudflare Tunnel run.
type Options struct {
	// Token is the Cloudflare Tunnel token for the named Cloudflare Tunnel. Required.
	Token string

	// Handler receives every HTTP request the Cloudflare edge delivers down the Cloudflare
	// Tunnel, for every hostname routed to it. Required.
	Handler http.Handler

	// Middleware runs before Handler on every request and can reject a request before Handler
	// sees it.
	Middleware []middleware.Handler

	// Version is reported to the Cloudflare edge as the connector version. It defaults to
	// DefaultVersion.
	Version string

	// GracePeriod is how long in-flight requests get to finish after ctx is cancelled before
	// the Cloudflare Tunnel connections are torn down. It defaults to DefaultGracePeriod.
	GracePeriod time.Duration

	// HAConnections is the number of connections kept to the Cloudflare edge. It defaults to
	// DefaultHAConnections.
	HAConnections int

	// Log receives cloudflared's own log output. Nil discards it.
	Log *zerolog.Logger
}

var supervisorMu sync.Mutex

// Run runs one Cloudflare Tunnel until ctx is done. It blocks. When ctx is cancelled it
// stops accepting requests, waits up to Options.GracePeriod for in-flight requests, then
// returns.
func Run(ctx context.Context, opts Options) error {
	if opts.Handler == nil {
		return errors.New("a handler is required")
	}
	if opts.Version == "" {
		opts.Version = DefaultVersion
	}
	if opts.GracePeriod <= 0 {
		opts.GracePeriod = DefaultGracePeriod
	}
	if opts.GracePeriod > connection.MaxGracePeriod {
		return fmt.Errorf("grace period %s exceeds the cloudflared maximum %s", opts.GracePeriod, connection.MaxGracePeriod)
	}
	if opts.HAConnections <= 0 {
		opts.HAConnections = DefaultHAConnections
	}
	log := opts.Log
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	token, err := ParseToken(opts.Token)
	if err != nil {
		return err
	}
	credentials := token.Credentials()
	tunnelID := credentials.TunnelID.String()
	log = ptr(log.With().Str("tunnel", tunnelID).Logger())

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	featureSelector, err := features.NewFeatureSelector(runCtx, credentials.AccountTag, nil, false, log)
	if err != nil {
		return fmt.Errorf("cloudflared feature selector could not be created: %w", err)
	}

	clientConfig, err := client.NewConfig(opts.Version, runtime.GOOS+"_"+runtime.GOARCH, featureSelector)
	if err != nil {
		return fmt.Errorf("cloudflared client config could not be created: %w", err)
	}
	tags := []pogs.Tag{{Name: "ID", Value: clientConfig.ConnectorID.String()}}

	protocolSelector, err := connection.NewProtocolSelector(connection.AutoSelectFlag, log)
	if err != nil {
		return err
	}

	edgeTLSConfigs := make(map[connection.Protocol]*tls.Config, defaultEdgeTLSConfigsPerProtos)
	for _, p := range connection.ProtocolList {
		settings := p.TLSSettings()
		if settings == nil {
			return fmt.Errorf("cloudflared protocol %s has no tls settings", p)
		}
		cfg, err := tlsconfig.CreateTunnelConfig("", settings.ServerName)
		if err != nil {
			return fmt.Errorf("cloudflared edge tls config could not be created: %w", err)
		}
		if len(settings.NextProtos) > 0 {
			cfg.NextProtos = settings.NextProtos
		}
		edgeTLSConfigs[p] = cfg
	}

	warpRouting := ingress.NewWarpRoutingConfig(&config.WarpRoutingConfig{})
	originDialer := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer: ingress.NewDialer(warpRouting),
	}, log)

	rule := ingress.Rule{
		Hostname: "*",
		Service:  &ingress.ManagementService{HTTPLocalProxy: opts.Handler},
		Handlers: opts.Middleware,
	}
	orchestratorConfig := &orchestration.Config{
		Ingress:             &ingress.Ingress{},
		WarpRouting:         warpRouting,
		OriginDialerService: originDialer,
	}
	orchestrator, err := orchestration.NewOrchestrator(runCtx, orchestratorConfig, tags, []ingress.Rule{rule}, log)
	if err != nil {
		return fmt.Errorf("cloudflared orchestrator could not be created: %w", err)
	}

	graceShutdownC := make(chan struct{})
	tunnelConfig := &supervisor.TunnelConfig{
		ClientConfig:                        clientConfig,
		GracePeriod:                         opts.GracePeriod,
		Region:                              credentials.Endpoint,
		EdgeIPVersion:                       allregions.Auto,
		HAConnections:                       opts.HAConnections,
		Tags:                                tags,
		Log:                                 log,
		Observer:                            connection.NewObserver(log),
		ReportedVersion:                     opts.Version,
		Retries:                             defaultRetries,
		MaxEdgeAddrRetries:                  defaultMaxEdgeAddrRetries,
		NoPrechecks:                         true,
		NamedTunnel:                         &connection.TunnelProperties{Credentials: credentials},
		ProtocolSelector:                    protocolSelector,
		EdgeTLSConfigs:                      edgeTLSConfigs,
		OriginDialerService:                 originDialer,
		RPCTimeout:                          defaultRPCTimeout,
		QUICConnectionLevelFlowControlLimit: defaultQUICConnFlowControl,
		QUICStreamLevelFlowControlLimit:     defaultQUICStreamFlowControl,
	}

	s, err := newSupervisor(tunnelID, log, tunnelConfig, orchestrator, originDialer, graceShutdownC)
	if err != nil {
		return err
	}

	errC := make(chan error, 1)
	go func() {
		errC <- s.Run(runCtx, signal.New(make(chan struct{})))
	}()

	select {
	case err := <-errC:
		return err
	case <-ctx.Done():
	}

	close(graceShutdownC)
	select {
	case err := <-errC:
		return err
	case <-time.After(opts.GracePeriod):
		cancel()
		return <-errC
	}
}

// newSupervisor builds the Cloudflare Tunnel supervisor with prometheus.DefaultRegisterer
// swapped for one labelling every collector with the Cloudflare Tunnel ID, so more than one
// Cloudflare Tunnel can run in one process. The swap mutates a process-wide global, so
// construction is serialized across Cloudflare Tunnels.
func newSupervisor(
	tunnelID string,
	log *zerolog.Logger,
	tunnelConfig *supervisor.TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	originDialer *ingress.OriginDialerService,
	graceShutdownC <-chan struct{},
) (*supervisor.Supervisor, error) {
	supervisorMu.Lock()
	defer supervisorMu.Unlock()

	shared := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = prometheus.WrapRegistererWith(prometheus.Labels{"tunnel": tunnelID}, shared)
	defer func() { prometheus.DefaultRegisterer = shared }()

	dnsService := origins.NewDNSResolverService(origins.NewDNSDialer(), log, origins.NewMetrics(prometheus.DefaultRegisterer))
	originDialer.AddReservedService(dnsService, []netip.AddrPort{origins.VirtualDNSServiceAddr})
	tunnelConfig.OriginDNSService = dnsService

	s, err := supervisor.NewSupervisor(tunnelConfig, orchestrator, graceShutdownC)
	if err != nil {
		return nil, fmt.Errorf("cloudflared supervisor could not be created: %w", err)
	}
	return s, nil
}

func ptr(l zerolog.Logger) *zerolog.Logger {
	return &l
}
