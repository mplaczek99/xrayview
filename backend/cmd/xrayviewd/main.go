package main

import (
	"context"
	"log"
	"os/signal"

	"xrayview/backend/internal/app"
	"xrayview/backend/internal/shutdown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), shutdown.Signals()...)
	defer stop()

	application, err := app.NewFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	if err := application.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
