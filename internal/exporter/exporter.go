package exporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/nskondratev/ipod-export/internal/dedupe"
	"github.com/nskondratev/ipod-export/internal/model"
	"github.com/nskondratev/ipod-export/internal/naming"
)

type Config struct {
	OutputDir      string
	DryRun         bool
	Verbose        bool
	Overwrite      bool
	Jobs           int
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
	if err := os.MkdirAll(e.Config.OutputDir, 0o750); err != nil {
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

	return e.executeCopyJobs(ctx, jobs, report, progress)
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

type plannedTrack struct {
	job              copyJob
	skip             bool
	skippedDuplicate bool
	skippedExisting  bool
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

		planned, err := e.planTrack(track, reserved)
		if err != nil {
			return jobs, report, err
		}

		jobs, report = appendPlannedTrack(jobs, report, planned)
	}

	return jobs, report, nil
}

func (e Exporter) planTrack(track model.Track, reserved map[string]struct{}) (plannedTrack, error) {
	ext, ok := e.supportedExtension(track.FilePath)
	if !ok {
		e.logf("skip unsupported file %q", track.FilePath)

		return plannedTrack{skip: true}, nil
	}

	seen, err := e.Config.Detector.Seen(track)
	if err != nil {
		return plannedTrack{}, err
	}

	if seen {
		e.logf("skip duplicate %q", track.FilePath)

		return plannedTrack{
			skip:             true,
			skippedDuplicate: true,
		}, nil
	}

	dst := e.planDestination(track, ext, reserved)
	if dst == "" {
		return plannedTrack{
			skip:            true,
			skippedExisting: true,
		}, nil
	}

	size, err := fileSize(track.FilePath)
	if err != nil {
		return plannedTrack{}, fmt.Errorf("stat source file %q: %w", track.FilePath, err)
	}

	return plannedTrack{
		job: copyJob{
			Track:       track,
			Destination: dst,
			Size:        size,
		},
	}, nil
}

func (e Exporter) supportedExtension(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := e.Config.AllowedExts[ext]

	return ext, ok
}

func (e Exporter) planDestination(
	track model.Track,
	ext string,
	reserved map[string]struct{},
) string {
	name := e.Config.Resolver.Resolve(track, ext, func(candidate string) bool {
		return e.candidateExists(candidate, reserved)
	})
	reserved[normalizeCandidateKey(name)] = struct{}{}

	dst := filepath.Join(e.Config.OutputDir, name)
	if !e.Config.Overwrite && destinationExists(dst) {
		e.logf("skip existing %q", dst)

		return ""
	}

	return dst
}

func (e Exporter) candidateExists(candidate string, reserved map[string]struct{}) bool {
	if _, ok := reserved[normalizeCandidateKey(candidate)]; ok {
		return true
	}

	if e.Config.Overwrite {
		return false
	}

	_, err := os.Stat(filepath.Join(e.Config.OutputDir, candidate))

	return err == nil
}

func destinationExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

func totalBytes(jobs []copyJob) int64 {
	var total int64

	for _, job := range jobs {
		total += job.Size
	}

	return total
}

func (e Exporter) executeCopyJobs(
	ctx context.Context,
	jobs []copyJob,
	report Report,
	progress *ProgressBar,
) (Report, error) {
	workerCount := resolveWorkerCount(e.Config.Jobs, len(jobs))

	if workerCount == 0 {
		return report, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobCh := make(chan copyJob)
	resultCh := make(chan copyResult, workerCount)

	var wg sync.WaitGroup
	e.startWorkers(ctx, &wg, workerCount, jobCh, resultCh, progress, cancel)

	go enqueueCopyJobs(ctx, jobs, jobCh)
	go closeResultsWhenDone(&wg, resultCh)

	return collectCopyResults(ctx, resultCh, report)
}

type copyResult struct {
	job copyJob
	err error
}

func resolveWorkerCount(configured, jobCount int) int {
	workerCount := configured
	if workerCount < 1 {
		workerCount = 1
	}

	if workerCount > jobCount && jobCount > 0 {
		workerCount = jobCount
	}

	return workerCount
}

func (e Exporter) startWorkers(
	ctx context.Context,
	wg *sync.WaitGroup,
	workerCount int,
	jobCh <-chan copyJob,
	resultCh chan<- copyResult,
	progress *ProgressBar,
	cancel context.CancelFunc,
) {
	for range workerCount {
		wg.Add(1)

		go e.copyWorker(ctx, wg, jobCh, resultCh, progress, cancel)
	}
}

func (e Exporter) copyWorker(
	ctx context.Context,
	wg *sync.WaitGroup,
	jobCh <-chan copyJob,
	resultCh chan<- copyResult,
	progress *ProgressBar,
	cancel context.CancelFunc,
) {
	defer wg.Done()

	for job := range jobCh {
		result := e.copyJob(ctx, job, progress)
		if result.err == nil {
			resultCh <- result

			continue
		}

		resultCh <- result

		cancel()

		return
	}
}

func (e Exporter) copyJob(ctx context.Context, job copyJob, progress *ProgressBar) copyResult {
	if err := ctx.Err(); err != nil {
		return copyResult{err: err}
	}

	if progress != nil {
		progress.StartFile(filepath.Base(job.Destination))
	}

	err := copyFile(ctx, job.Track.FilePath, job.Destination, e.Config.Overwrite, func(written int64) {
		if progress != nil {
			progress.AddBytes(written)
		}
	})
	if err != nil {
		if progress != nil {
			progress.AbortFile()
		}

		return copyResult{
			job: job,
			err: fmt.Errorf("copy %q to %q: %w", job.Track.FilePath, job.Destination, err),
		}
	}

	if progress != nil {
		progress.FinishFile()
	}

	e.logf("copied %q -> %q", job.Track.FilePath, job.Destination)

	return copyResult{job: job}
}

func enqueueCopyJobs(ctx context.Context, jobs []copyJob, jobCh chan<- copyJob) {
	defer close(jobCh)

	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return
		case jobCh <- job:
		}
	}
}

func closeResultsWhenDone(wg *sync.WaitGroup, resultCh chan<- copyResult) {
	wg.Wait()
	close(resultCh)
}

func collectCopyResults(ctx context.Context, resultCh <-chan copyResult, report Report) (Report, error) {
	for result := range resultCh {
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) && ctx.Err() != nil {
				return report, ctx.Err()
			}

			return report, result.err
		}

		report.Exported++
	}

	if err := ctx.Err(); err != nil {
		return report, err
	}

	return report, nil
}

func normalizeCandidateKey(value string) string {
	switch runtime.GOOS {
	case "windows", "darwin":
		return strings.ToLower(value)
	default:
		return value
	}
}

func copyFile(ctx context.Context, src, dst string, overwrite bool, onProgress func(int64)) (err error) {
	// #nosec G304 -- source files come from the mounted iPod database and validated filesystem scan.
	in, err := os.Open(src)
	if err != nil {
		return err
	}

	defer func() {
		_ = in.Close()
	}()

	tempFile, err := createTempCopy(dst)
	if err != nil {
		return err
	}

	defer func() {
		tempFile.cleanup()
	}()

	if err := copyWithContext(ctx, tempFile.file, in, onProgress); err != nil {
		return err
	}

	if err := tempFile.finish(); err != nil {
		return err
	}

	if err := ensureDestinationWritable(dst, overwrite); err != nil {
		return err
	}

	if err := os.Rename(tempFile.path, dst); err != nil {
		return err
	}

	tempFile.completed = true

	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader, onProgress func(int64)) error {
	buf := make([]byte, 32*1024)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		done, err := copyChunk(dst, src, buf, onProgress)
		if err != nil {
			return err
		}

		if done {
			return nil
		}
	}
}

type tempCopyFile struct {
	file      *os.File
	path      string
	completed bool
}

func createTempCopy(dst string) (*tempCopyFile, error) {
	dir := filepath.Dir(dst)
	base := filepath.Base(dst)

	file, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return nil, err
	}

	return &tempCopyFile{
		file: file,
		path: file.Name(),
	}, nil
}

func (t *tempCopyFile) cleanup() {
	_ = t.file.Close()

	if !t.completed {
		_ = os.Remove(t.path)
	}
}

func (t *tempCopyFile) finish() error {
	if err := t.file.Sync(); err != nil {
		return err
	}

	return t.file.Close()
}

func ensureDestinationWritable(dst string, overwrite bool) error {
	if overwrite {
		return nil
	}

	_, err := os.Stat(dst)
	if err == nil {
		return os.ErrExist
	}

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func copyChunk(
	dst io.Writer,
	src io.Reader,
	buf []byte,
	onProgress func(int64),
) (bool, error) {
	n, readErr := src.Read(buf)
	if n > 0 {
		if _, err := dst.Write(buf[:n]); err != nil {
			return false, err
		}

		if onProgress != nil {
			onProgress(int64(n))
		}
	}

	if readErr == nil {
		return false, nil
	}

	if errors.Is(readErr, io.EOF) {
		return true, nil
	}

	return false, readErr
}

func appendPlannedTrack(jobs []copyJob, report Report, planned plannedTrack) ([]copyJob, Report) {
	if planned.skippedDuplicate {
		report.SkippedDuplicates++

		return jobs, report
	}

	if planned.skippedExisting {
		report.SkippedExisting++

		return jobs, report
	}

	if planned.skip {
		return jobs, report
	}

	jobs = append(jobs, planned.job)

	return jobs, report
}
