package exporter

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProgressBarRendersMetrics(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	progress := NewProgressBar(&out, 10, 1024)
	progress.startedAt = time.Now().Add(-2 * time.Second)
	progress.StartFile("track.mp3")
	progress.AddBytes(512)
	progress.FinishFile()
	progress.Finish()

	got := out.String()
	for _, want := range []string{
		"1/10 files",
		"512B/1.0KiB",
		"elapsed 00:02",
		"ETA 00:02",
		"track.mp3",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress output = %q, want substring %q", got, want)
		}
	}
}
