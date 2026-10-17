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
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/aeilot/TraceX/internal/experiment"
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

func run(ctx context.Context, args []string, out, errOut io.Writer) (result error) {
	flags := flag.NewFlagSet("tracex", flag.ContinueOnError)
	flags.SetOutput(errOut)
	addr := flags.String("addr", "127.0.0.1:4318", "HTTP listen address (loopback by default)")
	var runPaths pathsFlag
	flags.Var(&runPaths, "run", "run directory or normalized JSON; repeat for comparison")
	runsDir := flags.String("runs-dir", "", "discover immediate run directories and JSON profiles")
	watch := flags.Duration("watch", time.Second, "source refresh interval; 0 disables")
	exportPath := flags.String("export", "", "write normalized JSON to this file and exit; use - for stdout")
	ariesDir := flags.String("aries-dir", "", "ARIES repository; explicitly enables experiment controls")
	ariesBin := flags.String("aries-bin", "", "ARIES executable (default ARIES_DIR/bin/aries)")
	executor := flags.String("aries-executor", "local", "ARIES execution host: local or orb")
	machine := flags.String("aries-machine", "", "OrbStack machine (required for orb executor)")
	jobsDir := flags.String("jobs-dir", "", "private experiment job directory (default ARIES_DIR/.cache/tracex/jobs)")
	stopTimeout := flags.Duration("stop-timeout", 2*time.Minute, "grace period for ARIES shutdown; never force kills")
	flags.Usage = func() {
		fmt.Fprintln(errOut, "Usage: tracex [--run PATH ...] [--runs-dir DIR] [--watch 1s] [--addr HOST:PORT] [--export PATH|-]")
		fmt.Fprintln(errOut, "Visualize agent workloads; optionally prepare and run configured ARIES experiments.")
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
	if *ariesDir != "" && *exportPath != "" {
		return fmt.Errorf("--aries-dir cannot be combined with --export")
	}
	if *ariesDir == "" && (*ariesBin != "" || *machine != "" || *jobsDir != "" || *executor != "local") {
		return fmt.Errorf("experiment options require --aries-dir")
	}
	if *executor != "local" && *executor != "orb" {
		return fmt.Errorf("--aries-executor must be local or orb")
	}
	if *executor == "orb" && *machine == "" {
		return fmt.Errorf("--aries-machine is required for orb executor")
	}
	if *executor == "local" && *machine != "" {
		return fmt.Errorf("--aries-machine requires orb executor")
	}
	if *stopTimeout <= 0 {
		return fmt.Errorf("--stop-timeout must be positive")
	}
	if *watch < 0 {
		return fmt.Errorf("watch must be nonnegative")
	}
	if *runsDir != "" {
		entries, err := os.ReadDir(*runsDir)
		if err != nil {
			return fmt.Errorf("discover runs: %w", err)
		}
		discovered := []string{}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) == ".json" {
				discovered = append(discovered, filepath.Join(*runsDir, e.Name()))
			}
		}
		sort.Strings(discovered)
		runPaths = append(runPaths, discovered...)
		if len(discovered) == 0 {
			return fmt.Errorf("runs directory contains no run directories or JSON files")
		}
	}
	if *exportPath != "" && len(runPaths) > 1 {
		return fmt.Errorf("export requires a single --run source")
	}
	p := profile.Demo()
	if *exportPath != "" && len(runPaths) == 1 {
		var err error
		p, err = profile.Load(runPaths[0])
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
	sources := []server.Source{}
	for _, path := range runPaths {
		sources = append(sources, server.Source{Path: path})
	}
	if len(sources) == 0 {
		variant := profile.DemoVariant()
		sources = append(sources, server.Source{Profile: &p}, server.Source{Profile: &variant})
	}
	workspaceCtx, cancelWorkspace := context.WithCancel(ctx)
	defer cancelWorkspace()
	opts := server.Options{WatchInterval: *watch}
	if *ariesDir != "" {
		opts.Experiment = &experiment.Config{Root: *ariesDir, Binary: *ariesBin, Executor: *executor, Machine: *machine, JobsDir: *jobsDir, StopTimeout: *stopTimeout}
		if opts.WatchInterval == 0 {
			opts.WatchInterval = time.Second
		}
	}
	handler, err := server.NewWorkspace(workspaceCtx, sources, opts)
	if err != nil {
		return err
	}
	defer func() {
		cancelWorkspace()
		if closer, ok := handler.(interface{ Close(context.Context) error }); ok {
			result = errors.Join(result, closer.Close(context.Background()))
		}
	}()
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
	if len(runPaths) == 0 {
		fmt.Fprintln(out, "Showing synthetic demo data. Use --run PATH to load your run.")
	}
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

type pathsFlag []string

func (p *pathsFlag) String() string { return fmt.Sprint([]string(*p)) }
func (p *pathsFlag) Set(v string) error {
	if v == "" {
		return fmt.Errorf("empty run path")
	}
	*p = append(*p, v)
	return nil
}
