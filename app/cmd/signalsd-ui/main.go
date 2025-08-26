package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui"
	"github.com/information-sharing-networks/signalsd/app/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	cmd := &cobra.Command{
		Use:   "signalsd-ui",
		Short: "Signalsd web user interface",
		Long:  `Web UI for managing Information Sharing Networks`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run()
		},
	}

	v := version.Get()
	cmd.Version = fmt.Sprintf("%s (built %s, commit %s)", v.Version, v.BuildDate, v.GitCommit)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	serverLogger := logger.InitServerLogger()

	// Load UI configuration
	cfg, err := ui.NewConfig(serverLogger)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Failed to load UI configuration")
	}

	serverLogger.Info().Msgf("Starting UI server (version: %s)", version.Get().Version)

	serverLogger.Info().Msgf("using signalsd API at: %s", cfg.APIBaseURL)

	// Create UI server
	server := ui.NewServer(cfg, serverLogger)

	// Set up graceful shutdown handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run the server
	if err := server.Start(ctx); err != nil {
		serverLogger.Error().Msgf("UI server error: %v", err)
		return err
	}

	serverLogger.Info().Msg("UI server shutdown complete")
	return nil
}
