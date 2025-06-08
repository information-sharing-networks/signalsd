package logger

import (
	"os"

	"github.com/rs/zerolog"
)

func InitServerLogger() *zerolog.Logger {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		Level(zerolog.DebugLevel).
		With().
		Timestamp().
		Logger()

	return &logger
}

// use console writer when in dev enviroment
func InitHttpLogger(logLevel zerolog.Level, environment string) *zerolog.Logger {
	var logger zerolog.Logger

	if environment == "dev" {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			Level(logLevel).
			With().
			Timestamp().
			Logger()
	} else {
		logger = zerolog.New(os.Stdout).
			Level(logLevel).
			With().
			Timestamp().
			Logger()
	}

	return &logger
}
