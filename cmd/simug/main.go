package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"simug/internal/app"
)

const (
	exitSuccess  = 0
	exitRunError = 2
	exitUsage    = 64
)

func main() {
	os.Exit(runMain(os.Args[1:]))
}

func runMain(args []string) int {
	cmd := "run"
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "run":
		once, help, err := parseRunArgs(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "simug: %v\n", err)
			fmt.Fprintln(os.Stderr, usageText())
			return exitUsage
		}
		if help {
			fmt.Println("usage: simug run [--once]")
			return exitSuccess
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if once {
			err = app.RunOnce(ctx, ".")
		} else {
			err = app.Run(ctx, ".")
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "simug: %v\n", err)
			return exitRunError
		}
	case "explain-last-failure":
		msg, err := app.ExplainLastFailure(context.Background(), ".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "simug: %v\n", err)
			return exitRunError
		}
		fmt.Println(msg)
	case "help", "-h", "--help":
		fmt.Println(usageText())
	default:
		fmt.Fprintf(os.Stderr, "simug: unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, usageText())
		return exitUsage
	}
	return exitSuccess
}

func parseRunArgs(args []string) (once bool, help bool, err error) {
	for _, arg := range args {
		switch arg {
		case "--once":
			once = true
		case "-h", "--help", "help":
			help = true
		default:
			return false, false, fmt.Errorf("unknown run option %q", arg)
		}
	}
	return once, help, nil
}

func usageText() string {
	return "usage: simug [run [--once]|explain-last-failure]"
}
