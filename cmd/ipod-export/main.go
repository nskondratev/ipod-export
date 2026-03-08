package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/nskondratev/ipod-export/internal/dedupe"
	"github.com/nskondratev/ipod-export/internal/exporter"
	"github.com/nskondratev/ipod-export/internal/ipoddb"
)

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()

	logger := log.New(os.Stderr, "", log.LstdFlags)

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
	tracks, err := reader.ReadTracks(context.Background(), cfg.IPodPath)
	if err != nil {
		if !cfg.FallbackTags {
			return fmt.Errorf("read iPod database: %w", err)
		}

		logger.Printf("database parsing failed, using filesystem fallback: %v", err)
		tracks, err = ipoddb.NewFilesystemFallbackReader(logger).ReadTracks(context.Background(), cfg.IPodPath)
		if err != nil {
			return fmt.Errorf("filesystem fallback failed: %w", err)
		}
	}

	exp := exporter.Exporter{
		Logger: logger,
		Config: exporter.Config{
			OutputDir:   cfg.OutputPath,
			DryRun:      cfg.DryRun,
			Verbose:     cfg.Verbose,
			Overwrite:   cfg.Overwrite,
			Detector:    detector,
			Resolver:    exporter.DefaultConflictResolver{},
			AllowedExts: ipoddb.SupportedAudioExtensions(),
		},
	}

	report, err := exp.Export(context.Background(), tracks)
	if err != nil {
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

type cliConfig struct {
	IPodPath       string
	OutputPath     string
	DryRun         bool
	Verbose        bool
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
