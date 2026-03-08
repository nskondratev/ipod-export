package dedupe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nskondratev/ipod-export/internal/model"
)

const (
	ModeNone   = "none"
	ModeSource = "source"
	ModeHash   = "hash"
)

type Detector interface {
	Seen(track model.Track) (bool, error)
}

func NewDetector(mode string) (Detector, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", ModeSource:
		return &sourceDetector{seen: make(map[string]struct{})}, nil
	case ModeNone:
		return noopDetector{}, nil
	case ModeHash:
		return &hashDetector{seen: make(map[string]struct{})}, nil
	default:
		return nil, fmt.Errorf("unsupported duplicate mode %q", mode)
	}
}

type noopDetector struct{}

func (noopDetector) Seen(model.Track) (bool, error) {
	return false, nil
}

type sourceDetector struct {
	seen map[string]struct{}
}

func (d *sourceDetector) Seen(track model.Track) (bool, error) {
	key := strings.TrimSpace(track.TrackID)
	if key == "" {
		key = filepath.Clean(track.FilePath)
	}
	if _, ok := d.seen[key]; ok {
		return true, nil
	}
	d.seen[key] = struct{}{}
	return false, nil
}

type hashDetector struct {
	seen map[string]struct{}
}

func (d *hashDetector) Seen(track model.Track) (bool, error) {
	file, err := os.Open(track.FilePath)
	if err != nil {
		return false, fmt.Errorf("open source file: %w", err)
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return false, fmt.Errorf("hash source file: %w", err)
	}

	key := hex.EncodeToString(sum.Sum(nil))
	if _, ok := d.seen[key]; ok {
		return true, nil
	}
	d.seen[key] = struct{}{}
	return false, nil
}
