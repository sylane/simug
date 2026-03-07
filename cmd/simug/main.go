package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"simug/internal/app"
)

func main() {
	log.SetFlags(0)

	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "run":
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if err := app.Run(ctx, "."); err != nil {
			log.Fatalf("simug: %v", err)
		}
	case "help", "-h", "--help":
		fmt.Println("usage: simug [run]")
	default:
		log.Fatalf("unknown command %q (usage: simug [run])", cmd)
	}
}
