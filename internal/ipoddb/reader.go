package ipoddb

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nskondratev/ipod-export/internal/model"
)

var ErrUnsupportedDB = errors.New("iTunesDB parsing is not implemented yet")

var supportedAudioExtensions = map[string]struct{}{
	".aac":  {},
	".aif":  {},
	".aiff": {},
	".alac": {},
	".m4a":  {},
	".m4b":  {},
	".mp3":  {},
	".wav":  {},
}

type Reader interface {
	ReadTracks(ctx context.Context, mountPath string) ([]model.Track, error)
}

func SupportedAudioExtensions() map[string]struct{} {
	return supportedAudioExtensions
}

type ITunesDBReader struct {
	logger *log.Logger
}

func NewITunesDBReader(logger *log.Logger) *ITunesDBReader {
	return &ITunesDBReader{logger: logger}
}

type FilesystemFallbackReader struct {
	logger *log.Logger
}

func NewFilesystemFallbackReader(logger *log.Logger) *FilesystemFallbackReader {
	return &FilesystemFallbackReader{logger: logger}
}

func (r *FilesystemFallbackReader) ReadTracks(ctx context.Context, mountPath string) ([]model.Track, error) {
	root := filepath.Join(mountPath, "iPod_Control", "Music")
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	var tracks []model.Track

	err := filepath.WalkDir(root, r.walkFallbackTrack(ctx, &tracks))
	if err != nil {
		return nil, err
	}

	if r.logger != nil {
		r.logger.Printf("filesystem fallback discovered %d audio files", len(tracks))
	}

	return tracks, nil
}

func (r *FilesystemFallbackReader) walkFallbackTrack(
	ctx context.Context,
	tracks *[]model.Track,
) fs.WalkDirFunc {
	return func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if d.IsDir() || !isSupportedAudioFile(path) {
			return nil
		}

		*tracks = append(*tracks, buildFallbackTrack(path))

		return nil
	}
}

func isSupportedAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := supportedAudioExtensions[ext]

	return ok
}

func buildFallbackTrack(path string) model.Track {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.TrimSuffix(filepath.Base(path), ext)

	return model.Track{
		TrackID:  path,
		Artist:   "",
		Title:    base,
		FilePath: path,
	}
}
