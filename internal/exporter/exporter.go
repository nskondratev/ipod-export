package exporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nskondratev/ipod-export/internal/dedupe"
	"github.com/nskondratev/ipod-export/internal/model"
	"github.com/nskondratev/ipod-export/internal/naming"
)

type Config struct {
	OutputDir      string
	DryRun         bool
	Verbose        bool
	Overwrite      bool
	ShowProgress   bool
	ProgressOutput io.Writer
	Detector       dedupe.Detector
	Resolver       naming.ConflictResolver
	AllowedExts    map[string]struct{}
}

type Exporter struct {
	Logger *log.Logger
	Config Config
}

type Report struct {
	Exported          int
	SkippedDuplicates int
	SkippedExisting   int
}

type DefaultConflictResolver struct{}

func (DefaultConflictResolver) Resolve(track model.Track, ext string, exists func(string) bool) string {
	return naming.Resolver{}.Resolve(track, ext, exists)
}

func (e Exporter) Export(ctx context.Context, tracks []model.Track) (Report, error) {
	if err := os.MkdirAll(e.Config.OutputDir, 0o755); err != nil {
		return Report{}, fmt.Errorf("create output dir: %w", err)
	}

	jobs, report, err := e.planCopyJobs(ctx, tracks)
	if err != nil {
		return report, err
	}

	if e.Config.DryRun {
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			e.Logger.Printf("[dry-run] copy %q -> %q", job.Track.FilePath, job.Destination)
			report.Exported++
		}
		return report, nil
	}

	var progress *ProgressBar
	if e.Config.ShowProgress && len(jobs) > 0 {
		progress = NewProgressBar(e.progressOutput(), len(jobs), totalBytes(jobs))
		defer progress.Finish()
	}

	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		if progress != nil {
			progress.StartFile(filepath.Base(job.Destination))
		}

		if err := copyFile(ctx, job.Track.FilePath, job.Destination, e.Config.Overwrite, func(written int64) {
			if progress != nil {
				progress.AddBytes(written)
			}
		}); err != nil {
			return report, fmt.Errorf("copy %q to %q: %w", job.Track.FilePath, job.Destination, err)
		}
		report.Exported++
		if progress != nil {
			progress.FinishFile()
		}
		e.logf("copied %q -> %q", job.Track.FilePath, job.Destination)
	}

	return report, nil
}

func (e Exporter) logf(format string, args ...any) {
	if e.Config.Verbose && e.Logger != nil {
		e.Logger.Printf(format, args...)
	}
}

type copyJob struct {
	Track       model.Track
	Destination string
	Size        int64
}

func (e Exporter) progressOutput() io.Writer {
	if e.Config.ProgressOutput != nil {
		return e.Config.ProgressOutput
	}
	return os.Stderr
}

func (e Exporter) planCopyJobs(ctx context.Context, tracks []model.Track) ([]copyJob, Report, error) {
	report := Report{}
	reserved := make(map[string]struct{})
	jobs := make([]copyJob, 0, len(tracks))

	for _, track := range tracks {
		if err := ctx.Err(); err != nil {
			return jobs, report, err
		}

		ext := strings.ToLower(filepath.Ext(track.FilePath))
		if _, ok := e.Config.AllowedExts[ext]; !ok {
			e.logf("skip unsupported file %q", track.FilePath)
			continue
		}

		seen, err := e.Config.Detector.Seen(track)
		if err != nil {
			return jobs, report, err
		}
		if seen {
			report.SkippedDuplicates++
			e.logf("skip duplicate %q", track.FilePath)
			continue
		}

		name := e.Config.Resolver.Resolve(track, ext, func(candidate string) bool {
			if _, ok := reserved[candidate]; ok {
				return true
			}
			if e.Config.Overwrite {
				return false
			}
			_, err := os.Stat(filepath.Join(e.Config.OutputDir, candidate))
			return err == nil
		})
		reserved[name] = struct{}{}

		dst := filepath.Join(e.Config.OutputDir, name)
		if !e.Config.Overwrite {
			if _, err := os.Stat(dst); err == nil {
				report.SkippedExisting++
				e.logf("skip existing %q", dst)
				continue
			}
		}

		info, err := os.Stat(track.FilePath)
		if err != nil {
			return jobs, report, fmt.Errorf("stat source file %q: %w", track.FilePath, err)
		}

		jobs = append(jobs, copyJob{
			Track:       track,
			Destination: dst,
			Size:        info.Size(),
		})
	}

	return jobs, report, nil
}

func totalBytes(jobs []copyJob) int64 {
	var total int64
	for _, job := range jobs {
		total += job.Size
	}
	return total
}

func copyFile(ctx context.Context, src, dst string, overwrite bool, onProgress func(int64)) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
	base := filepath.Base(dst)

	out, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := out.Name()
	completed := false
	defer func() {
		_ = out.Close()
		if !completed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := copyWithContext(ctx, out, in, onProgress); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return os.ErrExist
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}

	completed = true
	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader, onProgress func(int64)) error {
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return err
			}
			if onProgress != nil {
				onProgress(int64(n))
			}
		}

		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		return readErr
	}
}
