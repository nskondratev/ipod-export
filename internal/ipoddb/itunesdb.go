package ipoddb

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nskondratev/ipod-export/internal/model"
)

func (r *ITunesDBReader) ReadTracks(ctx context.Context, mountPath string) ([]model.Track, error) {
	dbPath, err := locateDatabase(mountPath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database %q: %w", dbPath, err)
	}
	defer file.Close()

	header, err := readHeader(file)
	if err != nil {
		return nil, fmt.Errorf("read database header: %w", err)
	}

	if header.Signature != "mhbd" {
		return nil, fmt.Errorf("unexpected database signature %q", header.Signature)
	}
	if r.logger != nil {
		r.logger.Printf("detected iTunesDB %q version=%d header_size=%d", dbPath, header.Version, header.HeaderSize)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("%w: track record parsing after the mhbd header is still TODO", ErrUnsupportedDB)
}

type databaseHeader struct {
	Signature  string
	HeaderSize uint32
	BodySize   uint32
	Version    uint32
}

func locateDatabase(mountPath string) (string, error) {
	candidates := []string{
		filepath.Join(mountPath, "iPod_Control", "iTunes", "iTunesDB"),
		filepath.Join(mountPath, "iTunes_Control", "iTunes", "iTunesDB"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not find iTunesDB under %q", mountPath)
}

func readHeader(r io.Reader) (databaseHeader, error) {
	var raw [16]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return databaseHeader{}, err
	}

	return databaseHeader{
		Signature:  string(raw[0:4]),
		HeaderSize: binary.LittleEndian.Uint32(raw[4:8]),
		BodySize:   binary.LittleEndian.Uint32(raw[8:12]),
		Version:    binary.LittleEndian.Uint32(raw[12:16]),
	}, nil
}

func ResolveTrackPath(mountPath, raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}
	if filepath.IsAbs(cleaned) {
		return cleaned
	}

	cleaned = strings.ReplaceAll(cleaned, ":", string(filepath.Separator))
	cleaned = strings.TrimLeft(cleaned, string(filepath.Separator))
	return filepath.Join(mountPath, cleaned)
}

func NewTrack(id, mountPath, rawPath string) model.Track {
	resolved := ResolveTrackPath(mountPath, rawPath)
	return model.Track{
		TrackID:  id,
		FilePath: resolved,
	}
}
