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

func InitHttpLogger(logLevel zerolog.Level) *zerolog.Logger {
	var logger zerolog.Logger

	if logLevel == zerolog.DebugLevel {
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
