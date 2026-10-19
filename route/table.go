package route

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/openziti/edge-api/rest_model"
)

// ErrNoRoute is returned by Table.Resolve when no OpenZiti service in the Table carries a
// route for the hostname and path.
var ErrNoRoute = errors.New("no zab route for hostname and path")

// AmbiguousRouteError is returned by Table.Resolve when more than one OpenZiti service in
// the Table carries the same hostname and path prefix.
type AmbiguousRouteError struct {
	Hostname   string
	PathPrefix string
	Services   []string
}

// Error implements error.
func (e *AmbiguousRouteError) Error() string {
	return fmt.Sprintf("zab route %s%s is carried by more than one openziti service: %s",
		e.Hostname, e.PathPrefix, strings.Join(e.Services, ", "))
}

// Match is one resolved route: the OpenZiti service to dial, its Config, and the Route
// within that Config that matched.
type Match struct {
	Service string
	Config  *Config
	Route   Route
}

type entry struct {
	service string
	config  *Config
	route   Route
}

// Table resolves a hostname and path to an OpenZiti service across the services one
// requester may dial.
type Table struct {
	entries []entry
}

// NewTable builds a Table from the OpenZiti services visible to one requester. Services
// without a config of any ConfigTypes type are skipped. A service whose ConfigType config fails to parse
// is skipped and reported through the returned error, and the Table still holds every other
// service.
func NewTable(services []rest_model.ServiceDetail) (*Table, error) {
	t := &Table{}
	var errs []error

	for i := range services {
		svc := &services[i]
		body, ok := configBody(svc)
		if !ok {
			continue
		}

		name := ""
		if svc.Name != nil {
			name = *svc.Name
		}

		cfg, err := ParseConfig(body)
		if err != nil {
			errs = append(errs, fmt.Errorf("openziti service %q: %w", name, err))
			continue
		}

		for _, r := range cfg.Routes {
			t.entries = append(t.entries, entry{service: name, config: cfg, route: r})
		}
	}

	return t, errors.Join(errs...)
}

// Resolve returns the Match whose hostname equals host and whose path prefix is the
// longest prefix of path, where host may carry a port. It returns ErrNoRoute when nothing
// matches and an *AmbiguousRouteError when the longest prefix is carried by more than one
// OpenZiti service.
func (t *Table) Resolve(host, path string) (*Match, error) {
	hostname := normalizeHost(host)
	if path == "" {
		path = "/"
	}

	best := -1
	var matches []entry

	for _, e := range t.entries {
		if e.route.Hostname != hostname || !prefixMatches(e.route.PathPrefix, path) {
			continue
		}
		n := len(e.route.PathPrefix)
		switch {
		case n > best:
			best = n
			matches = matches[:0]
			matches = append(matches, e)
		case n == best:
			matches = append(matches, e)
		}
	}

	switch len(matches) {
	case 0:
		return nil, ErrNoRoute
	case 1:
		m := matches[0]
		return &Match{Service: m.service, Config: m.config, Route: m.route}, nil
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.service)
		}
		return nil, &AmbiguousRouteError{
			Hostname:   hostname,
			PathPrefix: matches[0].route.PathPrefix,
			Services:   names,
		}
	}
}

// configBody returns the service's config body for the newest ZAB config type it carries.
func configBody(svc *rest_model.ServiceDetail) (map[string]any, bool) {
	for _, spec := range ConfigTypes() {
		if body, ok := svc.Config[spec.Name]; ok {
			return body, true
		}
	}
	return nil, false
}

func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

func prefixMatches(prefix, path string) bool {
	if prefix == "/" {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return len(path) == len(prefix) || path[len(prefix)] == '/'
}
