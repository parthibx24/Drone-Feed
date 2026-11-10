// drone-gateway is the central server: one MAVLink link per drone, the
// command/mission/parameter protocols, server-side recording, web login, and
// the REST + WebSocket API and React UI that operators use.
//
//	drone-gateway -config gateway.yaml     run the server
//	drone-gateway hash-password            print a bcrypt hash for auth.users
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/parthibx24/Drone-Feed/server/gateway"
)

var version = "dev"

func main() {
	// Accept the subcommand anywhere: the Docker entrypoint prepends -config.
	for _, a := range os.Args[1:] {
		if a == "hash-password" {
			hashPassword()
			return
		}
	}

	cfgPath := flag.String("config", "gateway.yaml", "path to the configuration file")
	logLevel := flag.String("log-level", "info", "debug, info, warn, error")
	jsonLogs := flag.Bool("json-logs", false, "log as JSON (for Loki)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(*logLevel))
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if *jsonLogs {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	log := slog.New(h)

	cfg, err := gateway.LoadConfig(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	g, err := gateway.New(cfg, log, version)
	if err != nil {
		log.Error("startup", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := g.Run(ctx); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

// hashPassword reads a password (hidden when typed at a terminal, or from
// stdin when piped) and prints its bcrypt hash.
func hashPassword() {
	var pw string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprint(os.Stderr, "Repeat:   ")
		b2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil || string(b) != string(b2) {
			fmt.Fprintln(os.Stderr, "passwords do not match")
			os.Exit(1)
		}
		pw = string(b)
	} else {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pw = strings.TrimRight(line, "\r\n")
	}
	if len(pw) < 8 {
		fmt.Fprintln(os.Stderr, "use at least 8 characters")
		os.Exit(1)
	}
	h, err := gateway.HashPassword(pw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(h)
}
