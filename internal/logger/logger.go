package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Global loggers
var (
	// Stdout sends logs to stdout
	Stdout zerolog.Logger

	// StdoutWithSource sends logs to stdout with source info
	StdoutWithSource zerolog.Logger

	// Stderr sends logs to stderr
	Stderr zerolog.Logger

	// StderrWithSource sends logs to stderr with source info
	StderrWithSource zerolog.Logger
)

func init() {
	// Configure zerolog to use UTC time and human-friendly formatting for timestamps
	zerolog.TimeFieldFormat = time.RFC3339

	// Create console writers for human-friendly output
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	consoleErrWriter := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}

	// Create loggers with appropriate outputs
	Stdout = zerolog.New(consoleWriter).With().Timestamp().Logger()
	StdoutWithSource = zerolog.New(consoleWriter).With().Timestamp().Caller().Logger()
	Stderr = zerolog.New(consoleErrWriter).With().Timestamp().Logger()
	StderrWithSource = zerolog.New(consoleErrWriter).With().Timestamp().Caller().Logger()
}
