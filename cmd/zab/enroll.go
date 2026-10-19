package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/openziti-test-kitchen/ziti-access-bridge/enroll"
)

func enrollCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "enroll <enrollment.jwt>",
		Short: "Enroll ZAB's OpenZiti identity from an enrollment JWT and record the controllers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := load(*configPath)
			if err != nil {
				return err
			}
			result, err := enroll.Enroll(args[0], cfg)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "enrolled")
			fmt.Fprintf(out, "  key:         %s\n", result.KeyFile)
			fmt.Fprintf(out, "  cert:        %s\n", result.CertFile)
			fmt.Fprintf(out, "  ca:          %s\n", result.CAFile)
			fmt.Fprintf(out, "  endpoints:   %s\n", result.EndpointFile)
			for _, c := range result.Controllers {
				fmt.Fprintf(out, "  controller:  %s\n", c)
			}
			return nil
		},
	}
}
