package exporter

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nskondratev/ipod-export/internal/dedupe"
	"github.com/nskondratev/ipod-export/internal/model"
)

func TestExportStopsWhenContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "source.mp3")
	if err := os.WriteFile(src, []byte("audio"), 0o600); err != nil {
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
	if err := os.WriteFile(src, []byte("audio"), 0o600); err != nil {
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
	if err := os.WriteFile(src, []byte("audio-bytes"), 0o600); err != nil {
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

func TestExportCopiesFilesInParallel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src1 := filepath.Join(dir, "source1.mp3")
	src2 := filepath.Join(dir, "source2.mp3")
	if err := os.WriteFile(src1, []byte("audio-one"), 0o600); err != nil {
		t.Fatalf("WriteFile(src1) error = %v", err)
	}
	if err := os.WriteFile(src2, []byte("audio-two"), 0o600); err != nil {
		t.Fatalf("WriteFile(src2) error = %v", err)
	}

	exp := Exporter{
		Logger: log.New(io.Discard, "", 0),
		Config: Config{
			OutputDir: dir,
			Jobs:      2,
			Detector:  mustDetector(t, dedupe.ModeNone),
			Resolver:  DefaultConflictResolver{},
			AllowedExts: map[string]struct{}{
				".mp3": {},
			},
		},
	}

	report, err := exp.Export(context.Background(), []model.Track{
		{TrackID: "1", Artist: "Artist One", Title: "Track One", FilePath: src1},
		{TrackID: "2", Artist: "Artist Two", Title: "Track Two", FilePath: src2},
	})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if report.Exported != 2 {
		t.Fatalf("Exported = %d, want 2", report.Exported)
	}

	for _, want := range []string{
		filepath.Join(dir, "Artist One - Track One.mp3"),
		filepath.Join(dir, "Artist Two - Track Two.mp3"),
	} {
		// #nosec G304 -- test reads files it just created inside t.TempDir().
		data, err := os.ReadFile(want)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", want, err)
		}
		if !strings.HasPrefix(string(data), "audio-") {
			t.Fatalf("unexpected content in %q: %q", want, string(data))
		}
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
