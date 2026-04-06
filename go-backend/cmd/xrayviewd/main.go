package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"xrayview/go-backend/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.NewFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	if err := application.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
