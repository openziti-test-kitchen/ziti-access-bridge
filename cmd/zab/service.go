package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/openziti-test-kitchen/ziti-access-bridge/agent"
	"github.com/openziti-test-kitchen/ziti-access-bridge/route"
)

func agentServiceCommand(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Attach zab configs to OpenZiti services",
		Long: `Attach zab configs to OpenZiti services.

These commands add or replace the zab config on a service and create the service when it
does not exist. Everything else about a service, its policies, its other configs, and its
deletion, stays with the OpenZiti CLI.`,
	}
	cmd.AddCommand(
		agentServiceSetCommand(configPath),
		agentServiceShowCommand(configPath),
		agentServiceListCommand(configPath),
	)
	return cmd
}

func agentServiceSetCommand(configPath *string) *cobra.Command {
	var (
		routes          []string
		scheme          string
		tlsVerify       bool
		hostHeader      string
		stripPathPrefix bool
		stripJWT        bool
	)

	cmd := &cobra.Command{
		Use:   "set <service>",
		Short: "Create or replace the zab config on a service, creating the service if needed",
		Example: `  zab agent service set app --route app.example.com
  zab agent service set api --route app.example.com/api --strip-path-prefix --scheme https --tls-verify=false`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := load(*configPath)
			if err != nil {
				return err
			}

			rc, err := routeConfig(routes, scheme, tlsVerify, hostHeader, stripPathPrefix, stripJWT)
			if err != nil {
				return err
			}

			change, err := agent.SetServiceConfig(cmd.Context(), cfg, args[0], rc)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if change.ServiceCreated {
				fmt.Fprintf(out, "service %s created, id=%s\n", change.Service, change.ServiceID)
			} else {
				fmt.Fprintf(out, "service %s exists, id=%s\n", change.Service, change.ServiceID)
			}
			fmt.Fprintf(out, "config %s %s, id=%s\n", change.ConfigName, change.ConfigAction, change.ConfigID)
			if change.Attached {
				fmt.Fprintln(out, "config attached to service")
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&routes, "route", nil, "Public hostname with an optional path prefix, app.example.com or app.example.com/api. Repeatable, at least one")
	f.StringVar(&scheme, "scheme", route.SchemeHTTP, "What the application speaks: http or https")
	f.BoolVar(&tlsVerify, "tls-verify", true, "With --scheme https, verify the application's TLS certificate")
	f.StringVar(&hostHeader, "host-header", "", "Replace the Host header with this value")
	f.BoolVar(&stripPathPrefix, "strip-path-prefix", false, "Remove the matched path prefix before forwarding")
	f.BoolVar(&stripJWT, "strip-jwt", false, "Remove the Cloudflare Access JWT header before forwarding")
	_ = cmd.MarkFlagRequired("route")

	return cmd
}

func agentServiceShowCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <service>",
		Short: "Print the zab config on a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := load(*configPath)
			if err != nil {
				return err
			}
			sc, err := agent.ShowServiceConfig(cmd.Context(), cfg, args[0])
			if err != nil {
				return err
			}
			printServiceConfig(cmd.OutOrStdout(), sc)
			return nil
		},
	}
}

func agentServiceListCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the services that carry a zab config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := load(*configPath)
			if err != nil {
				return err
			}
			list, err := agent.ListServiceConfigs(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no services carry a zab config")
				return nil
			}
			for i := range list {
				printServiceConfig(cmd.OutOrStdout(), &list[i])
			}
			return nil
		},
	}
}

func routeConfig(routes []string, scheme string, tlsVerify bool, hostHeader string, stripPathPrefix, stripJWT bool) (*route.Config, error) {
	if len(routes) == 0 {
		return nil, errors.New("at least one --route is required")
	}

	rc := &route.Config{
		Scheme:                 scheme,
		HostHeader:             hostHeader,
		StripPathPrefix:        stripPathPrefix,
		StripAccessProviderJWT: stripJWT,
	}
	if !tlsVerify {
		verify := false
		rc.TLSVerify = &verify
	}

	for _, r := range routes {
		hostname, prefix, _ := strings.Cut(r, "/")
		if hostname == "" {
			return nil, fmt.Errorf("route %q has no hostname", r)
		}
		rc.Routes = append(rc.Routes, route.Route{Hostname: hostname, PathPrefix: "/" + prefix})
	}
	return rc, nil
}

func printServiceConfig(out io.Writer, sc *agent.ServiceConfig) {
	fmt.Fprintf(out, "%s  id=%s  %s\n", sc.Service, sc.ServiceID, sc.ConfigType)
	for _, r := range sc.Config.Routes {
		fmt.Fprintf(out, "  route:  %s%s\n", r.Hostname, r.PathPrefix)
	}
	fmt.Fprintf(out, "  scheme: %s", sc.Config.Scheme)
	if sc.Config.Scheme == route.SchemeHTTPS && !sc.Config.VerifyTLS() {
		fmt.Fprint(out, ", tls not verified")
	}
	fmt.Fprintln(out)
	if sc.Config.HostHeader != "" {
		fmt.Fprintf(out, "  host header: %s\n", sc.Config.HostHeader)
	}
	if sc.Config.StripPathPrefix {
		fmt.Fprintln(out, "  strips path prefix")
	}
	if sc.Config.StripAccessProviderJWT {
		fmt.Fprintln(out, "  strips cloudflare access jwt")
	}
}
