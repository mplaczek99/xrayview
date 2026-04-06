package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"xrayview/go-backend/internal/app"
	"xrayview/go-backend/internal/config"
	"xrayview/go-backend/internal/contracts"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return fmt.Errorf("expected a subcommand")
	}

	switch args[0] {
	case "serve":
		return serve()
	case "print-config":
		return printConfig()
	case "list-commands":
		for _, command := range contracts.SupportedCommandStrings() {
			fmt.Println(command)
		}
		return nil
	case "version":
		fmt.Printf("%s contract-v%d\n", contracts.ServiceName, contracts.BackendContractVersion)
		return nil
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.NewFromEnvironment()
	if err != nil {
		return err
	}

	return application.Run(ctx)
}

func printConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}

func printUsage(stream *os.File) {
	fmt.Fprintln(stream, "usage: xrayview-cli <subcommand>")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "subcommands:")
	fmt.Fprintln(stream, "  serve         run the phase 7 local HTTP backend")
	fmt.Fprintln(stream, "  print-config  print resolved backend configuration as JSON")
	fmt.Fprintln(stream, "  list-commands print supported command names")
	fmt.Fprintln(stream, "  version       print service and contract version")
}
