package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/michaelquigley/pfxlog"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openziti-test-kitchen/ziti-access-bridge/config"
)

const version = "zab"

func main() {
	var configPath string

	root := &cobra.Command{
		Use:           "zab",
		Short:         "Ziti Access Bridge: carry Cloudflare Access requests onto an OpenZiti overlay network",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", config.DefaultPath, "Configuration file")

	root.AddCommand(
		runCommand(&configPath),
		enrollCommand(&configPath),
		agentCommand(&configPath),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "zab:", err)
		os.Exit(1)
	}
}

// load reads the configuration file and initializes logging at its level.
func load(path string) (*config.File, logrus.Level, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, 0, err
	}
	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		return nil, 0, fmt.Errorf("logLevel: %w", err)
	}
	pfxlog.GlobalInit(level, pfxlog.DefaultOptions())
	return cfg, level, nil
}
