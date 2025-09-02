package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
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

// run the UI service in standalone mode
func run() error {
	// Load UI configuration
	cfg, err := ui.NewConfig()
	if err != nil {
		log.Printf("Failed to load UI configuration %v", err.Error())
		os.Exit(1)
	}

	appLogger := logger.InitLogger(logger.ParseLogLevel(cfg.LogLevel), cfg.Environment)

	appLogger.Info("Starting UI server", slog.String("version", version.Get().Version))

	appLogger.Info("using signalsd API", slog.String("api_url", cfg.APIBaseURL))

	// Create UI server
	server := ui.NewStandaloneServer(cfg, appLogger)

	// Set up graceful shutdown handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run the server
	if err := server.Start(ctx); err != nil {
		appLogger.Error("UI server error", slog.String("error", err.Error()))
		return err
	}

	appLogger.Info("UI server shutdown complete")
	return nil
}
