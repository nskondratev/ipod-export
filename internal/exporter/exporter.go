package exporter

import (
	"context"
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
	OutputDir   string
	DryRun      bool
	Verbose     bool
	Overwrite   bool
	Detector    dedupe.Detector
	Resolver    naming.ConflictResolver
	AllowedExts map[string]struct{}
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
	report := Report{}

	if err := os.MkdirAll(e.Config.OutputDir, 0o755); err != nil {
		return report, fmt.Errorf("create output dir: %w", err)
	}

	reserved := make(map[string]struct{})

	for _, track := range tracks {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		ext := strings.ToLower(filepath.Ext(track.FilePath))
		if _, ok := e.Config.AllowedExts[ext]; !ok {
			e.logf("skip unsupported file %q", track.FilePath)
			continue
		}

		seen, err := e.Config.Detector.Seen(track)
		if err != nil {
			return report, err
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

		if e.Config.DryRun {
			e.Logger.Printf("[dry-run] copy %q -> %q", track.FilePath, dst)
			report.Exported++
			continue
		}

		if err := copyFile(track.FilePath, dst); err != nil {
			return report, fmt.Errorf("copy %q to %q: %w", track.FilePath, dst, err)
		}
		report.Exported++
		e.logf("copied %q -> %q", track.FilePath, dst)
	}

	return report, nil
}

func (e Exporter) logf(format string, args ...any) {
	if e.Config.Verbose && e.Logger != nil {
		e.Logger.Printf(format, args...)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
