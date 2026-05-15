package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/server"
	"github.com/information-sharing-networks/signalsd/app/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: signalsd-ui [--version]\n\nWeb UI for managing Information Sharing Networks\n")
	}
	flag.Parse()

	if *showVersion {
		v := version.Get()
		fmt.Printf("%s (built %s, commit %s)\n", v.Version, v.BuildDate, v.GitCommit)
		return
	}

	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run the UI service in standalone mode
func run() error {
	// Load UI configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Printf("Failed to load UI configuration %v", err.Error())
		os.Exit(1)
	}

	appLogger := logger.InitLogger(logger.ParseLogLevel(cfg.LogLevel), cfg.Environment)

	appLogger.Info("Starting UI server", slog.String("version", version.Get().Version))

	appLogger.Info("using signalsd API", slog.String("api_url", cfg.APIBaseURL))

	// Create UI server
	server := server.NewStandaloneServer(cfg, appLogger)

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
