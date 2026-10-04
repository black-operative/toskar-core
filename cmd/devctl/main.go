package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "about":
		fmt.Print(version.CurrentOffer().Text())
	case "paths":
		cfg := config.DefaultConfig()
		fmt.Printf("data:     %s\n", cfg.DataDir)
		fmt.Printf("models:   %s\n", cfg.ModelsDir)
		fmt.Printf("runtimes: %s\n", cfg.RuntimesDir)
		fmt.Printf("logs:     %s\n", cfg.LogsDir)
		fmt.Printf("db:       %s\n", cfg.DBPath)
	case "automations":
		if err := automationsCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "mcp":
		if err := mcpCommand(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "join":
		exit(joinCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "join-token":
		exit(joinTokenCommand(os.Args[2:], newDaemon(), os.Stdout, time.Now))
	case "network":
		exit(networkCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "leave":
		exit(leaveCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "completion":
		if err := completionCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: toskarctl <version|about|paths|automations|mcp|join|join-token|network|leave|completion>\n")
}

// exit ends with err's message and exit status: 2 for usage, 1 otherwise.
func exit(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	var e *exitError
	if errors.As(err, &e) {
		os.Exit(e.code)
	}
	os.Exit(1)
}
