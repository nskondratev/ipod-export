package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nskondratev/ipod-export/internal/dedupe"
	"github.com/nskondratev/ipod-export/internal/exporter"
	"github.com/nskondratev/ipod-export/internal/ipoddb"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, errInterrupted) {
			os.Exit(130)
		}
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

var errInterrupted = errors.New("interrupted by signal")

func run() error {
	cfg := parseFlags()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopSignals := installSignalHandler(logger, cancel)
	defer stopSignals()

	if cfg.IPodPath == "" {
		return errors.New("missing required --ipod path")
	}
	if cfg.OutputPath == "" {
		return errors.New("missing required --out path")
	}
	if err := validateIPodPath(cfg.IPodPath); err != nil {
		return err
	}

	dupMode := cfg.DuplicateMode
	if cfg.HashDuplicates {
		dupMode = dedupe.ModeHash
	}

	detector, err := dedupe.NewDetector(dupMode)
	if err != nil {
		return err
	}

	reader := ipoddb.NewITunesDBReader(logger)
	tracks, err := reader.ReadTracks(ctx, cfg.IPodPath)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Printf("shutdown requested while reading iPod database")
			return errInterrupted
		}
		if !cfg.FallbackTags {
			return fmt.Errorf("read iPod database: %w", err)
		}

		logger.Printf("database parsing failed, using filesystem fallback: %v", err)
		tracks, err = ipoddb.NewFilesystemFallbackReader(logger).ReadTracks(ctx, cfg.IPodPath)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				logger.Printf("shutdown requested while scanning filesystem fallback")
				return errInterrupted
			}
			return fmt.Errorf("filesystem fallback failed: %w", err)
		}
	}

	exp := exporter.Exporter{
		Logger: logger,
		Config: exporter.Config{
			OutputDir:      cfg.OutputPath,
			DryRun:         cfg.DryRun,
			Verbose:        cfg.Verbose,
			Overwrite:      cfg.Overwrite,
			ShowProgress:   !cfg.DryRun && !cfg.Verbose && !cfg.NoProgress,
			ProgressOutput: os.Stderr,
			Detector:       detector,
			Resolver:       exporter.DefaultConflictResolver{},
			AllowedExts:    ipoddb.SupportedAudioExtensions(),
		},
	}

	report, err := exp.Export(ctx, tracks)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Printf(
				"shutdown requested: exported=%d skipped_duplicates=%d skipped_existing=%d",
				report.Exported,
				report.SkippedDuplicates,
				report.SkippedExisting,
			)
			return errInterrupted
		}
		return err
	}

	logger.Printf(
		"completed: exported=%d skipped_duplicates=%d skipped_existing=%d",
		report.Exported,
		report.SkippedDuplicates,
		report.SkippedExisting,
	)

	return nil
}

func installSignalHandler(logger *log.Logger, cancel context.CancelFunc) func() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		defer close(done)

		sig, ok := <-signals
		if !ok {
			return
		}

		fmt.Fprintln(os.Stderr)
		logger.Printf("received %s, shutting down gracefully; press Ctrl+C again to force exit", sig)
		cancel()

		sig, ok = <-signals
		if !ok {
			return
		}

		fmt.Fprintln(os.Stderr)
		logger.Printf("received %s again, forcing exit", sig)
		os.Exit(130)
	}()

	return func() {
		signal.Stop(signals)
		close(signals)
		<-done
	}
}

type cliConfig struct {
	IPodPath       string
	OutputPath     string
	DryRun         bool
	Verbose        bool
	NoProgress     bool
	Overwrite      bool
	HashDuplicates bool
	FallbackTags   bool
	DuplicateMode  string
}

func parseFlags() cliConfig {
	var cfg cliConfig

	flag.StringVar(&cfg.IPodPath, "ipod", "", "mounted iPod path")
	flag.StringVar(&cfg.OutputPath, "out", "", "destination directory")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "print planned actions without copying files")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "enable verbose logging")
	flag.BoolVar(&cfg.NoProgress, "no-progress", false, "disable the interactive progress bar")
	flag.BoolVar(&cfg.Overwrite, "overwrite", false, "overwrite destination files when names resolve to an existing file")
	flag.BoolVar(&cfg.HashDuplicates, "hash-duplicates", false, "use content hashing to detect duplicate files")
	flag.BoolVar(&cfg.FallbackTags, "fallback-tags", false, "fall back to scanning audio files when iTunesDB parsing fails (metadata fallback is scaffolded, not full tag parsing)")
	flag.StringVar(&cfg.DuplicateMode, "duplicates", dedupe.ModeSource, "duplicate handling mode: none, source, hash")
	flag.Parse()

	return cfg
}

func validateIPodPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat iPod path %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("iPod path %q is not a directory", path)
	}

	dbDir := filepath.Join(path, "iPod_Control")
	if _, err := os.Stat(dbDir); err != nil {
		return fmt.Errorf("iPod path %q does not look like a mounted iPod: missing %q", path, dbDir)
	}

	return nil
}
