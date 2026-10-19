package sdkpool

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/michaelquigley/pfxlog"
	edgeapis "github.com/openziti/sdk-golang/v2/edge-apis"
	"github.com/openziti/sdk-golang/v2/ziti"
)

// DefaultIdleWindow and DefaultGrace are the Options defaults.
const (
	DefaultIdleWindow = 30 * time.Minute
	DefaultGrace      = 5 * time.Minute
)

// ErrAuthentication wraps an OpenZiti authentication failure returned by Pool.Acquire. The
// OpenZiti controller rejected the access provider JWT, so the requester has no OpenZiti
// Identity the JWT maps to.
var ErrAuthentication = errors.New("openziti authentication failed")

// Options configures a Pool.
type Options struct {
	// Controllers lists the OpenZiti Edge Client API URLs, each of the form
	// https://host:port/edge/client/v1. At least one is required.
	Controllers []string

	// CAPool verifies the OpenZiti controllers' TLS certificates. Nil uses the system roots.
	CAPool *x509.CertPool

	// ConfigTypes lists the OpenZiti config types each context requests with its services.
	ConfigTypes []string

	// IdleWindow is how long a context lives after the last Acquire for its key. It defaults
	// to DefaultIdleWindow.
	IdleWindow time.Duration

	// Grace is how long a context lives past the newest access provider JWT expiry it has
	// seen. It defaults to DefaultGrace.
	Grace time.Duration

	// OnEvict, when set, is called with the key of every context the Pool closes.
	OnEvict func(key string)

	// OnContext, when set, is called with every context the Pool creates, before it
	// authenticates, so the caller can attach listeners such as controller discovery.
	OnContext func(zctx ziti.Context)

	// Context overrides the ziti.Options every context is built with. Nil uses the
	// sdk-golang defaults.
	Context *ziti.Options
}

type entry struct {
	once sync.Once
	ctx  ziti.Context
	err  error

	mu       sync.Mutex
	jwt      string
	lastSeen time.Time
	lastExp  time.Time
}

// Pool keeps one OpenZiti SDK context per key.
type Pool struct {
	opts         Options
	authenticate func(jwt string) (ziti.Context, error)
	mu           sync.Mutex
	entries      map[string]*entry
}

// New builds a Pool. It returns an error when opts names no controller.
func New(opts Options) (*Pool, error) {
	if len(opts.Controllers) == 0 {
		return nil, errors.New("at least one openziti controller is required")
	}
	if opts.IdleWindow <= 0 {
		opts.IdleWindow = DefaultIdleWindow
	}
	if opts.Grace <= 0 {
		opts.Grace = DefaultGrace
	}
	p := &Pool{
		opts:    opts,
		entries: map[string]*entry{},
	}
	p.authenticate = p.authenticateWithController
	return p, nil
}

// Acquire returns the context for key. When none exists it authenticates to OpenZiti with
// jwt and keeps the result. When one exists it records exp, hands jwt to the context when
// jwt differs from the one the context last saw, and returns it. Concurrent first calls for
// one key share a single authentication.
func (p *Pool) Acquire(key, jwt string, exp time.Time) (ziti.Context, error) {
	now := time.Now()

	p.mu.Lock()
	e, ok := p.entries[key]
	if !ok {
		e = &entry{jwt: jwt, lastSeen: now, lastExp: exp}
		p.entries[key] = e
	}
	p.mu.Unlock()

	e.once.Do(func() {
		e.ctx, e.err = p.authenticate(jwt)
		if e.err != nil {
			p.mu.Lock()
			if p.entries[key] == e {
				delete(p.entries, key)
			}
			p.mu.Unlock()
		}
	})
	if e.err != nil {
		return nil, e.err
	}

	e.mu.Lock()
	e.lastSeen = now
	if exp.After(e.lastExp) {
		e.lastExp = exp
	}
	if e.jwt != jwt {
		e.jwt = jwt
		e.ctx.LoginWithJWT(jwt)
	}
	e.mu.Unlock()

	return e.ctx, nil
}

// SetControllers replaces the controller list new contexts authenticate against. Contexts
// already in the Pool keep the controller set the SDK discovered for them.
func (p *Pool) SetControllers(controllers []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opts.Controllers = append([]string(nil), controllers...)
}

func (p *Pool) controllers() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.opts.Controllers...)
}

func (p *Pool) authenticateWithController(jwt string) (ziti.Context, error) {
	creds := edgeapis.NewJwtCredentials(jwt)
	creds.CaPool = p.opts.CAPool
	creds.ConfigTypes = p.opts.ConfigTypes

	cfg := &ziti.Config{
		ZtAPIs:      p.controllers(),
		ConfigTypes: p.opts.ConfigTypes,
		Credentials: creds,
	}

	zctx, err := ziti.NewContextWithOpts(cfg, p.opts.Context)
	if err != nil {
		return nil, fmt.Errorf("openziti context could not be created: %w", err)
	}

	if p.opts.OnContext != nil {
		p.opts.OnContext(zctx)
	}

	if err := zctx.Authenticate(); err != nil {
		zctx.Close()
		return nil, fmt.Errorf("%w: %w", ErrAuthentication, err)
	}

	return zctx, nil
}

// Run closes idle and expired contexts until ctx is done, then closes every context.
func (p *Pool) Run(ctx context.Context) {
	interval := min(p.opts.IdleWindow, p.opts.Grace) / 2
	if interval < time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.Close()
			return
		case now := <-ticker.C:
			p.reap(now)
		}
	}
}

func (p *Pool) reap(now time.Time) {
	var expired []string

	p.mu.Lock()
	for key, e := range p.entries {
		e.mu.Lock()
		idle := now.After(e.lastSeen.Add(p.opts.IdleWindow))
		past := now.After(e.lastExp.Add(p.opts.Grace))
		e.mu.Unlock()
		if idle || past {
			expired = append(expired, key)
		}
	}
	p.mu.Unlock()

	for _, key := range expired {
		p.evict(key)
	}
}

func (p *Pool) evict(key string) {
	p.mu.Lock()
	e, ok := p.entries[key]
	if ok {
		delete(p.entries, key)
	}
	p.mu.Unlock()
	if !ok {
		return
	}

	pfxlog.Logger().WithField("key", key).Debug("closing openziti context")
	if e.ctx != nil {
		e.ctx.Close()
	}
	if p.opts.OnEvict != nil {
		p.opts.OnEvict(key)
	}
}

// Close closes every context in the Pool.
func (p *Pool) Close() {
	p.mu.Lock()
	keys := make([]string, 0, len(p.entries))
	for key := range p.entries {
		keys = append(keys, key)
	}
	p.mu.Unlock()

	for _, key := range keys {
		p.evict(key)
	}
}

// Len returns the number of contexts the Pool holds, including ones still authenticating.
func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}
