package logging

import (
	"log/slog"
	"os"
)

func New(serviceName string, level slog.Level) *slog.Logger {
	return slog.New(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}),
	).With(
		slog.String("service", serviceName),
	)
}
