package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aeilot/TraceX/internal/profile"
	"github.com/aeilot/TraceX/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "tracex: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("tracex", flag.ContinueOnError)
	flags.SetOutput(errOut)
	addr := flags.String("addr", "127.0.0.1:4318", "HTTP listen address (loopback by default)")
	runPath := flags.String("run", "", "ARIES run directory or normalized TraceX JSON (default: synthetic demo)")
	exportPath := flags.String("export", "", "write normalized JSON to this file and exit; use - for stdout")
	flags.Usage = func() {
		fmt.Fprintln(errOut, "Usage: tracex [--run PATH] [--addr HOST:PORT] [--export PATH|-]")
		fmt.Fprintln(errOut, "Visualize a read-only agent workload snapshot with a DAG and linear timeline.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q; use --run PATH", flags.Arg(0))
	}
	p := profile.Demo()
	if *runPath != "" {
		var err error
		p, err = profile.Load(*runPath)
		if err != nil {
			return fmt.Errorf("load run: %w", err)
		}
	}
	if *exportPath != "" {
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return fmt.Errorf("encode profile: %w", err)
		}
		data = append(data, '\n')
		if *exportPath == "-" {
			_, err = out.Write(data)
			return err
		}
		if err := os.WriteFile(*exportPath, data, 0600); err != nil {
			return fmt.Errorf("write export: %w", err)
		}
		fmt.Fprintf(out, "Exported profile to %s\n", *exportPath)
		return nil
	}
	handler, err := server.New(p)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w (choose another port with --addr)", *addr, err)
	}
	httpServer := server.HTTP(*addr, handler)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
		case <-done:
		}
	}()
	defer close(done)
	fmt.Fprintf(out, "TraceX: http://%s\n", listener.Addr())
	if *runPath == "" {
		fmt.Fprintln(out, "Showing synthetic demo data. Use --run PATH to load your run.")
	}
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
