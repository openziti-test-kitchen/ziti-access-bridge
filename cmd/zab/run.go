package main

import (
	"os"

	"github.com/spf13/cobra"

	zab "github.com/openziti-test-kitchen/ziti-access-bridge"
	"github.com/openziti-test-kitchen/ziti-access-bridge/cftunnel"
)

func runCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the bridge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, level, err := load(*configPath)
			if err != nil {
				return err
			}
			return zab.Run(cmd.Context(), cfg, zab.RunOptions{
				Version:        version,
				CloudflaredLog: cftunnel.NewLogger(os.Stderr, level),
			})
		},
	}
}
