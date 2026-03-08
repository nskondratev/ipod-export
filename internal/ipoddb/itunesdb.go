package ipoddb

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/nskondratev/ipod-export/internal/model"
)

const (
	chunkDatabase = "mhbd"
	chunkDataSet  = "mhsd"
	chunkTrackSet = "mhlt"
	chunkTrack    = "mhit"
	chunkString   = "mhod"

	dataSetTracks = 1

	stringTitle    = 1
	stringLocation = 2
	stringAlbum    = 3
	stringArtist   = 4
)

func (r *ITunesDBReader) ReadTracks(ctx context.Context, mountPath string) ([]model.Track, error) {
	dbPath, err := locateDatabase(mountPath)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, fmt.Errorf("read database %q: %w", dbPath, err)
	}

	tracks, version, err := parseDatabase(ctx, data, mountPath)
	if err != nil {
		return nil, err
	}

	if r.logger != nil {
		r.logger.Printf("parsed iTunesDB %q version=%d tracks=%d", dbPath, version, len(tracks))
	}

	return tracks, nil
}

type sizedChunk struct {
	Kind       string
	HeaderSize int
	TotalSize  int
	Offset     int
}

type databaseHeader struct {
	sizedChunk
	Version    uint32
	ChildCount uint32
}

type dataSetHeader struct {
	sizedChunk
	Type uint32
}

type trackListHeader struct {
	HeaderSize int
	TrackCount int
}

type trackHeader struct {
	sizedChunk
	StringCount int
	TrackID     uint32
	Year        int
}

type stringObject struct {
	sizedChunk
	Type  uint32
	Value string
}

func parseDatabase(ctx context.Context, data []byte, mountPath string) ([]model.Track, uint32, error) {
	root, err := readDatabaseHeader(data)
	if err != nil {
		return nil, 0, err
	}

	end := min(len(data), root.Offset+root.TotalSize)
	offset := root.Offset + root.HeaderSize
	var tracks []model.Track

	for offset < end {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}

		ds, err := readDataSetHeader(data, offset, end)
		if err != nil {
			return nil, 0, err
		}

		if ds.Type == dataSetTracks {
			parsed, err := parseTrackDataSet(ctx, data, ds, mountPath)
			if err != nil {
				return nil, 0, err
			}
			tracks = append(tracks, parsed...)
		}

		offset += ds.TotalSize
	}

	if len(tracks) == 0 {
		return nil, root.Version, fmt.Errorf("%w: no track records found in iTunesDB", ErrUnsupportedDB)
	}

	return tracks, root.Version, nil
}

func parseTrackDataSet(ctx context.Context, data []byte, ds dataSetHeader, mountPath string) ([]model.Track, error) {
	childOffset := ds.Offset + ds.HeaderSize
	childEnd := min(len(data), ds.Offset+ds.TotalSize)
	list, err := readTrackListHeader(data, childOffset, childEnd)
	if err != nil {
		return nil, err
	}

	offset := childOffset + list.HeaderSize
	tracks := make([]model.Track, 0, list.TrackCount)
	for i := 0; i < list.TrackCount; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		track, size, err := parseTrack(data, offset, childEnd, mountPath)
		if err != nil {
			return nil, err
		}
		if track.FilePath != "" {
			tracks = append(tracks, track)
		}
		offset += size
	}

	return tracks, nil
}

func parseTrack(data []byte, offset, limit int, mountPath string) (model.Track, int, error) {
	header, err := readTrackHeader(data, offset, limit)
	if err != nil {
		return model.Track{}, 0, err
	}

	track := NewTrack(strconv.FormatUint(uint64(header.TrackID), 10), mountPath, "")
	track.Year = header.Year

	cursor := offset + header.HeaderSize
	end := offset + header.TotalSize
	parsedStrings := 0

	for cursor < end {
		if header.StringCount > 0 && parsedStrings >= header.StringCount {
			break
		}
		if !hasChunkSignature(data, cursor, chunkString) {
			break
		}

		obj, err := readStringObject(data, cursor, end)
		if err != nil {
			return model.Track{}, 0, err
		}
		applyStringObject(&track, obj, mountPath)
		cursor += obj.TotalSize
		parsedStrings++
	}

	return track, header.TotalSize, nil
}

func applyStringObject(track *model.Track, obj stringObject, mountPath string) {
	switch obj.Type {
	case stringTitle:
		track.Title = obj.Value
	case stringLocation:
		track.FilePath = ResolveTrackPath(mountPath, obj.Value)
	case stringAlbum:
		track.Album = obj.Value
	case stringArtist:
		track.Artist = obj.Value
	}
}

func readDatabaseHeader(data []byte) (databaseHeader, error) {
	chunk, err := readSizedChunk(data, 0, len(data), chunkDatabase)
	if err != nil {
		return databaseHeader{}, err
	}
	if chunk.HeaderSize < 24 {
		return databaseHeader{}, fmt.Errorf("mhbd header too small: %d", chunk.HeaderSize)
	}

	return databaseHeader{
		sizedChunk: chunk,
		Version:    readUint32(data, 16),
		ChildCount: readUint32(data, 20),
	}, nil
}

func readDataSetHeader(data []byte, offset, limit int) (dataSetHeader, error) {
	chunk, err := readSizedChunk(data, offset, limit, chunkDataSet)
	if err != nil {
		return dataSetHeader{}, err
	}
	if chunk.HeaderSize < 16 {
		return dataSetHeader{}, fmt.Errorf("mhsd header too small at %d: %d", offset, chunk.HeaderSize)
	}

	return dataSetHeader{
		sizedChunk: chunk,
		Type:       readUint32(data, offset+12),
	}, nil
}

func readTrackListHeader(data []byte, offset, limit int) (trackListHeader, error) {
	if offset+12 > limit || offset+12 > len(data) {
		return trackListHeader{}, fmt.Errorf("truncated mhlt at %d", offset)
	}
	if got := string(data[offset : offset+4]); got != chunkTrackSet {
		return trackListHeader{}, fmt.Errorf("expected %s at %d, got %q", chunkTrackSet, offset, got)
	}

	headerSize := int(readUint32(data, offset+4))
	if headerSize < 12 || offset+headerSize > limit || offset+headerSize > len(data) {
		return trackListHeader{}, fmt.Errorf("invalid mhlt header size %d at %d", headerSize, offset)
	}

	return trackListHeader{
		HeaderSize: headerSize,
		TrackCount: int(readUint32(data, offset+8)),
	}, nil
}

func readTrackHeader(data []byte, offset, limit int) (trackHeader, error) {
	chunk, err := readSizedChunk(data, offset, limit, chunkTrack)
	if err != nil {
		return trackHeader{}, err
	}
	if chunk.HeaderSize < 24 {
		return trackHeader{}, fmt.Errorf("mhit header too small at %d: %d", offset, chunk.HeaderSize)
	}

	header := trackHeader{
		sizedChunk:  chunk,
		StringCount: int(readUint32(data, offset+12)),
		TrackID:     readUint32(data, offset+16),
	}
	if chunk.HeaderSize >= 160 && offset+148 <= len(data)-4 {
		year := int(readUint32(data, offset+148))
		if year > 0 && year < 10000 {
			header.Year = year
		}
	}

	return header, nil
}

func readStringObject(data []byte, offset, limit int) (stringObject, error) {
	chunk, err := readSizedChunk(data, offset, limit, chunkString)
	if err != nil {
		return stringObject{}, err
	}
	if chunk.HeaderSize < 16 {
		return stringObject{}, fmt.Errorf("mhod header too small at %d: %d", offset, chunk.HeaderSize)
	}

	raw := data[offset : offset+chunk.TotalSize]
	value := decodeStringPayload(extractStringPayload(raw, chunk.HeaderSize))

	return stringObject{
		sizedChunk: chunk,
		Type:       readUint32(data, offset+12),
		Value:      value,
	}, nil
}

func readSizedChunk(data []byte, offset, limit int, want string) (sizedChunk, error) {
	if offset+12 > limit || offset+12 > len(data) {
		return sizedChunk{}, fmt.Errorf("truncated %s header at %d", want, offset)
	}

	kind := string(data[offset : offset+4])
	if want != "" && kind != want {
		return sizedChunk{}, fmt.Errorf("expected %s at %d, got %q", want, offset, kind)
	}

	headerSize := int(readUint32(data, offset+4))
	totalSize := int(readUint32(data, offset+8))
	if headerSize < 12 {
		return sizedChunk{}, fmt.Errorf("invalid %s header size %d at %d", kind, headerSize, offset)
	}
	if totalSize < headerSize {
		return sizedChunk{}, fmt.Errorf("invalid %s total size %d at %d", kind, totalSize, offset)
	}
	if offset+totalSize > limit || offset+totalSize > len(data) {
		return sizedChunk{}, fmt.Errorf("%s at %d exceeds container bounds", kind, offset)
	}

	return sizedChunk{
		Kind:       kind,
		HeaderSize: headerSize,
		TotalSize:  totalSize,
		Offset:     offset,
	}, nil
}

func extractStringPayload(chunk []byte, headerSize int) []byte {
	if len(chunk) >= 40 {
		length := int(binary.LittleEndian.Uint32(chunk[28:32]))
		if length > 0 && 40+length <= len(chunk) {
			return chunk[40 : 40+length]
		}
	}

	if headerSize >= len(chunk) {
		return nil
	}

	return chunk[headerSize:]
}

func decodeStringPayload(payload []byte) string {
	if looksUTF16LE(payload) {
		payload = trimTrailingUTF16NUL(payload)
		if len(payload) == 0 {
			return ""
		}
		if len(payload)%2 != 0 {
			payload = payload[:len(payload)-1]
		}

		words := make([]uint16, 0, len(payload)/2)
		for i := 0; i+1 < len(payload); i += 2 {
			words = append(words, binary.LittleEndian.Uint16(payload[i:i+2]))
		}
		return strings.TrimSpace(string(utf16.Decode(words)))
	}

	payload = trimTrailingNUL(payload)
	if len(payload) == 0 {
		return ""
	}
	return strings.TrimSpace(string(payload))
}

func looksUTF16LE(payload []byte) bool {
	if len(payload) < 2 {
		return false
	}

	zeroBytes := 0
	for i := 1; i < len(payload); i += 2 {
		if payload[i] == 0 {
			zeroBytes++
		}
	}

	return zeroBytes > 0 && zeroBytes >= len(payload)/4
}

func trimTrailingNUL(payload []byte) []byte {
	for len(payload) > 0 && payload[len(payload)-1] == 0 {
		payload = payload[:len(payload)-1]
	}
	return payload
}

func trimTrailingUTF16NUL(payload []byte) []byte {
	for len(payload) >= 2 && payload[len(payload)-1] == 0 && payload[len(payload)-2] == 0 {
		payload = payload[:len(payload)-2]
	}
	return payload
}

func hasChunkSignature(data []byte, offset int, want string) bool {
	return offset+4 <= len(data) && string(data[offset:offset+4]) == want
}

func readUint32(data []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(data[offset : offset+4])
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
