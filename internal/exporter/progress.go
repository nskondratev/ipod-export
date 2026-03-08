package exporter

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type ProgressBar struct {
	output         io.Writer
	totalFiles     int
	totalBytes     int64
	startedAt      time.Time
	lastRenderedAt time.Time
	completedFiles int
	copiedBytes    int64
	currentFile    string
	rendered       bool
	mu             sync.Mutex
}

func NewProgressBar(output io.Writer, totalFiles int, totalBytes int64) *ProgressBar {
	return &ProgressBar{
		output:     output,
		totalFiles: totalFiles,
		totalBytes: totalBytes,
		startedAt:  time.Now(),
	}
}

func (p *ProgressBar) StartFile(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.currentFile = name
	p.renderLocked(false)
}

func (p *ProgressBar) AddBytes(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.copiedBytes += n
	p.renderLocked(false)
}

func (p *ProgressBar) FinishFile() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.completedFiles++
	p.renderLocked(false)
}

func (p *ProgressBar) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.renderLocked(true)
	if p.rendered {
		fmt.Fprintln(p.output)
	}
}

func (p *ProgressBar) renderLocked(force bool) {
	if p.output == nil {
		return
	}
	if !force && p.rendered && time.Since(p.lastRenderedAt) < 100*time.Millisecond {
		return
	}

	elapsed := time.Since(p.startedAt)
	speed := float64(0)
	if elapsed > 0 {
		speed = float64(p.copiedBytes) / elapsed.Seconds()
	}

	percent := float64(0)
	if p.totalBytes > 0 {
		percent = float64(p.copiedBytes) / float64(p.totalBytes)
	} else if p.totalFiles > 0 {
		percent = float64(p.completedFiles) / float64(p.totalFiles)
	}
	if percent > 1 {
		percent = 1
	}

	eta := "--"
	if speed > 0 && p.totalBytes > p.copiedBytes {
		remaining := time.Duration(float64(p.totalBytes-p.copiedBytes)/speed) * time.Second
		eta = formatDuration(remaining)
	}

	line := fmt.Sprintf(
		"\r%s %5.1f%% %d/%d files %s/%s %s/s elapsed %s ETA %s %s",
		renderBar(percent, 24),
		percent*100,
		p.completedFiles,
		p.totalFiles,
		formatBytes(p.copiedBytes),
		formatBytes(p.totalBytes),
		formatBytes(int64(speed)),
		formatDuration(elapsed),
		eta,
		truncateCurrentFile(p.currentFile, 28),
	)

	fmt.Fprint(p.output, line)
	p.rendered = true
	p.lastRenderedAt = time.Now()
}

func renderBar(percent float64, width int) string {
	filled := int(percent * float64(width))
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%dB", value)
	}

	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f%ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func formatDuration(value time.Duration) string {
	if value < 0 {
		value = 0
	}

	value = value.Round(time.Second)
	hours := value / time.Hour
	value -= hours * time.Hour
	minutes := value / time.Minute
	value -= minutes * time.Minute
	seconds := value / time.Second

	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func truncateCurrentFile(name string, limit int) string {
	if limit <= 0 || len(name) <= limit {
		return name
	}
	if limit <= 3 {
		return name[:limit]
	}
	return name[:limit-3] + "..."
}
