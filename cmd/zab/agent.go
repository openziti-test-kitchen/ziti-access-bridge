package main

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/openziti-test-kitchen/ziti-access-bridge/agent"
	"github.com/openziti-test-kitchen/ziti-access-bridge/config"
	"github.com/openziti-test-kitchen/ziti-access-bridge/control"
)

func agentCommand(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Inspect and administer a zab installation",
	}
	cmd.AddCommand(
		agentStatusCommand(configPath),
		agentRegisterConfigsCommand(configPath),
		agentServiceCommand(configPath),
		agentStopCommand(configPath),
	)
	return cmd
}

func agentStatusCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report enrollment, controller liveness, the running zab, and config type registration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := load(*configPath)
			if err != nil {
				return err
			}
			printReport(cmd.OutOrStdout(), cfg, agent.Status(cmd.Context(), cfg))
			return nil
		},
	}
}

func agentRegisterConfigsCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "register-configs",
		Short: "Register the zab config types with the OpenZiti controller, or update their schemas",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := load(*configPath)
			if err != nil {
				return err
			}
			results, err := agent.RegisterConfigTypes(cmd.Context(), cfg)
			for _, r := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s %s  id=%s\n", r.Action, r.Name, r.ID)
			}
			return err
		},
	}
}

func agentStopCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Ask the running zab to shut down",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			return control.NewClient(cfg.SocketFile).Stop(cmd.Context())
		},
	}
}

func printReport(out io.Writer, cfg *config.File, r *agent.Report) {
	fmt.Fprintf(out, "config:      %s\n", cfg.Path)

	fmt.Fprintln(out, "enrollment:")
	switch {
	case r.Enrollment.Err != "":
		fmt.Fprintf(out, "  not enrolled: %s\n", r.Enrollment.Err)
	default:
		fmt.Fprintf(out, "  subject:   %s\n", r.Enrollment.Subject)
		fmt.Fprintf(out, "  expires:   %s\n", r.Enrollment.NotAfter.Local().Format(time.RFC1123))
	}

	fmt.Fprintln(out, "running:")
	switch {
	case r.RunningErr != "":
		fmt.Fprintf(out, "  not running: %s\n", r.RunningErr)
	default:
		fmt.Fprintf(out, "  version:   %s\n", r.Running.Version)
		fmt.Fprintf(out, "  since:     %s\n", r.Running.StartedAt.Local().Format(time.RFC1123))
		fmt.Fprintf(out, "  pool size: %d\n", r.Running.PoolSize)
	}

	fmt.Fprintln(out, "controllers:")
	if len(r.Controllers) == 0 {
		fmt.Fprintln(out, "  none in the endpoint file")
	}
	for _, c := range r.Controllers {
		mark := "  "
		if c.Active {
			mark = "* "
		}
		switch {
		case c.Live:
			fmt.Fprintf(out, "  %s%s  live  %s\n", mark, c.URL, c.Version)
		default:
			fmt.Fprintf(out, "  %s%s  down  %s\n", mark, c.URL, c.Err)
		}
	}
	if r.Running != nil && r.Running.ActiveController != "" {
		fmt.Fprintln(out, "  * in use by the running zab")
	}

	fmt.Fprintln(out, "config types:")
	if r.ConfigErr != "" {
		fmt.Fprintf(out, "  unknown: %s\n", r.ConfigErr)
	}
	for _, ct := range r.ConfigTypes {
		switch {
		case !ct.Registered:
			fmt.Fprintf(out, "  %s - missing\n", ct.Name)
		case !ct.SchemaCurrent:
			fmt.Fprintf(out, "  %s - registered, schema out of date\n", ct.Name)
		default:
			fmt.Fprintf(out, "  %s - supported\n", ct.Name)
		}
	}
}
