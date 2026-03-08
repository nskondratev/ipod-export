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
	"runtime"

	"github.com/nskondratev/ipod-export/internal/dedupe"
	"github.com/nskondratev/ipod-export/internal/exporter"
	"github.com/nskondratev/ipod-export/internal/ipoddb"
	"github.com/nskondratev/ipod-export/internal/model"
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

	if err := validateConfig(cfg); err != nil {
		return err
	}

	detector, err := dedupe.NewDetector(resolveDuplicateMode(cfg))
	if err != nil {
		return err
	}

	tracks, err := readTracks(ctx, logger, cfg)
	if err != nil {
		return err
	}

	exp := newExporter(logger, cfg, detector)

	return exportTracks(ctx, logger, exp, tracks)
}

func validateConfig(cfg cliConfig) error {
	if cfg.IPodPath == "" {
		return errors.New("missing required --ipod path")
	}

	if cfg.OutputPath == "" {
		return errors.New("missing required --out path")
	}

	return validateIPodPath(cfg.IPodPath)
}

func resolveDuplicateMode(cfg cliConfig) string {
	if cfg.HashDuplicates {
		return dedupe.ModeHash
	}

	return cfg.DuplicateMode
}

func readTracks(ctx context.Context, logger *log.Logger, cfg cliConfig) ([]model.Track, error) {
	tracks, err := ipoddb.NewITunesDBReader(logger).ReadTracks(ctx, cfg.IPodPath)
	if err == nil {
		return tracks, nil
	}

	if errors.Is(err, context.Canceled) {
		logger.Printf("shutdown requested while reading iPod database")

		return nil, errInterrupted
	}

	if !cfg.FallbackTags {
		return nil, fmt.Errorf("read iPod database: %w", err)
	}

	logger.Printf("database parsing failed, using filesystem fallback: %v", err)

	tracks, err = ipoddb.NewFilesystemFallbackReader(logger).ReadTracks(ctx, cfg.IPodPath)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Printf("shutdown requested while scanning filesystem fallback")

			return nil, errInterrupted
		}

		return nil, fmt.Errorf("filesystem fallback failed: %w", err)
	}

	return tracks, nil
}

func newExporter(logger *log.Logger, cfg cliConfig, detector dedupe.Detector) exporter.Exporter {
	return exporter.Exporter{
		Logger: logger,
		Config: exporter.Config{
			OutputDir:      cfg.OutputPath,
			DryRun:         cfg.DryRun,
			Verbose:        cfg.Verbose,
			Overwrite:      cfg.Overwrite,
			Jobs:           cfg.Jobs,
			ShowProgress:   !cfg.DryRun && !cfg.Verbose && !cfg.NoProgress,
			ProgressOutput: os.Stderr,
			Detector:       detector,
			Resolver:       exporter.DefaultConflictResolver{},
			AllowedExts:    ipoddb.SupportedAudioExtensions(),
		},
	}
}

func exportTracks(
	ctx context.Context,
	logger *log.Logger,
	exp exporter.Exporter,
	tracks []model.Track,
) error {
	report, err := exp.Export(ctx, tracks)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logReport(logger, "shutdown requested", report)

			return errInterrupted
		}

		return err
	}

	logReport(logger, "completed", report)

	return nil
}

func logReport(logger *log.Logger, prefix string, report exporter.Report) {
	logger.Printf(
		"%s: exported=%d skipped_duplicates=%d skipped_existing=%d",
		prefix,
		report.Exported,
		report.SkippedDuplicates,
		report.SkippedExisting,
	)
}

func installSignalHandler(logger *log.Logger, cancel context.CancelFunc) func() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, handledSignals()...)

	done := make(chan struct{})

	go func() {
		defer close(done)

		sig, ok := <-signals
		if !ok {
			return
		}

		_, _ = fmt.Fprintln(os.Stderr)

		logger.Printf("received %s, shutting down gracefully; press Ctrl+C again to force exit", sig)
		cancel()

		sig, ok = <-signals
		if !ok {
			return
		}

		_, _ = fmt.Fprintln(os.Stderr)

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
	Jobs           int
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
	flag.IntVar(&cfg.Jobs, "jobs", 1, "number of files to copy in parallel")
	flag.BoolVar(&cfg.Overwrite, "overwrite", false, "overwrite destination files when names resolve to an existing file")
	flag.BoolVar(&cfg.HashDuplicates, "hash-duplicates", false, "use content hashing to detect duplicate files")
	flag.BoolVar(
		&cfg.FallbackTags,
		"fallback-tags",
		false,
		"fall back to scanning audio files when iTunesDB parsing fails "+
			"(metadata fallback is scaffolded, not full tag parsing)",
	)
	flag.StringVar(&cfg.DuplicateMode, "duplicates", dedupe.ModeSource, "duplicate handling mode: none, source, hash")
	flag.Parse()

	if cfg.Jobs < 1 {
		cfg.Jobs = 1
	}

	if cfg.Jobs > runtime.NumCPU()*4 {
		cfg.Jobs = runtime.NumCPU() * 4
	}

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
