package exporter

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/nskondratev/ipod-export/internal/dedupe"
	"github.com/nskondratev/ipod-export/internal/model"
)

func TestExportStopsWhenContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "source.mp3")
	if err := os.WriteFile(src, []byte("audio"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exp := Exporter{
		Logger: log.New(io.Discard, "", 0),
		Config: Config{
			OutputDir: dir,
			Detector:  mustDetector(t, dedupe.ModeNone),
			Resolver:  DefaultConflictResolver{},
			AllowedExts: map[string]struct{}{
				".mp3": {},
			},
		},
	}

	report, err := exp.Export(ctx, []model.Track{{
		TrackID:  "1",
		Artist:   "Artist",
		Title:    "Track",
		FilePath: src,
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Export() error = %v, want %v", err, context.Canceled)
	}
	if report.Exported != 0 {
		t.Fatalf("Exported = %d, want 0", report.Exported)
	}
}

func TestCopyFileRemovesPartialTempFileOnCancel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "source.mp3")
	dst := filepath.Join(dir, "dest.mp3")
	if err := os.WriteFile(src, []byte("audio"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := copyFile(ctx, src, dst, false, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copyFile() error = %v, want %v", err, context.Canceled)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dest stat error = %v, want %v", err, os.ErrNotExist)
	}

	matches, err := filepath.Glob(filepath.Join(dir, ".dest.mp3.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestPlanCopyJobsCollectsTotalSizes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "source.mp3")
	if err := os.WriteFile(src, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	exp := Exporter{
		Logger: log.New(io.Discard, "", 0),
		Config: Config{
			OutputDir: dir,
			Detector:  mustDetector(t, dedupe.ModeNone),
			Resolver:  DefaultConflictResolver{},
			AllowedExts: map[string]struct{}{
				".mp3": {},
			},
		},
	}

	jobs, report, err := exp.planCopyJobs(context.Background(), []model.Track{{
		TrackID:  "1",
		Artist:   "Artist",
		Title:    "Track",
		FilePath: src,
	}})
	if err != nil {
		t.Fatalf("planCopyJobs() error = %v", err)
	}
	if report != (Report{}) {
		t.Fatalf("report = %+v, want zero report", report)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(jobs))
	}
	if jobs[0].Size != int64(len("audio-bytes")) {
		t.Fatalf("job size = %d, want %d", jobs[0].Size, len("audio-bytes"))
	}
}

func mustDetector(t *testing.T, mode string) dedupe.Detector {
	t.Helper()

	detector, err := dedupe.NewDetector(mode)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}
	return detector
}
