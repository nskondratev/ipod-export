package ipoddb

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestITunesDBReaderReadTracksParsesTrackMetadata(t *testing.T) {
	t.Parallel()

	mountPath := t.TempDir()
	audioPath := filepath.Join(mountPath, "iPod_Control", "Music", "F00", "ABCD.mp3")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatalf("WriteFile(audio) error = %v", err)
	}

	dbPath := filepath.Join(mountPath, "iPod_Control", "iTunes", "iTunesDB")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(db) error = %v", err)
	}
	if err := os.WriteFile(dbPath, buildTestDatabase(), 0o644); err != nil {
		t.Fatalf("WriteFile(iTunesDB) error = %v", err)
	}

	reader := NewITunesDBReader(log.New(io.Discard, "", 0))
	tracks, err := reader.ReadTracks(context.Background(), mountPath)
	if err != nil {
		t.Fatalf("ReadTracks() error = %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("ReadTracks() len = %d, want 1", len(tracks))
	}

	track := tracks[0]
	if track.TrackID != "42" {
		t.Fatalf("TrackID = %q, want %q", track.TrackID, "42")
	}
	if track.Title != "No Surprises" {
		t.Fatalf("Title = %q, want %q", track.Title, "No Surprises")
	}
	if track.Artist != "Radiohead" {
		t.Fatalf("Artist = %q, want %q", track.Artist, "Radiohead")
	}
	if track.Album != "OK Computer" {
		t.Fatalf("Album = %q, want %q", track.Album, "OK Computer")
	}
	if track.Year != 1997 {
		t.Fatalf("Year = %d, want %d", track.Year, 1997)
	}
	if track.FilePath != audioPath {
		t.Fatalf("FilePath = %q, want %q", track.FilePath, audioPath)
	}
}

func TestResolveTrackPathConvertsColonSeparatedPaths(t *testing.T) {
	t.Parallel()

	got := ResolveTrackPath("/Volumes/IPOD", ":iPod_Control:Music:F12:EFGH.m4a")
	want := filepath.Join("/Volumes/IPOD", "iPod_Control", "Music", "F12", "EFGH.m4a")
	if got != want {
		t.Fatalf("ResolveTrackPath() = %q, want %q", got, want)
	}
}

func TestDecodeStringPayloadPrefersUTF16LEForCyrillic(t *testing.T) {
	t.Parallel()

	payload := encodeUTF16LE("легендарь огненных")
	got := decodeStringPayload(payload)
	want := "легендарь огненных"
	if got != want {
		t.Fatalf("decodeStringPayload() = %q, want %q", got, want)
	}
}

func TestDecodeStringPayloadKeepsUTF8WhenItAlreadyLooksCorrect(t *testing.T) {
	t.Parallel()

	payload := []byte("Radiohead")
	got := decodeStringPayload(payload)
	want := "Radiohead"
	if got != want {
		t.Fatalf("decodeStringPayload() = %q, want %q", got, want)
	}
}

func buildTestDatabase() []byte {
	title := buildStringObject(stringTitle, "No Surprises")
	location := buildStringObject(stringLocation, ":iPod_Control:Music:F00:ABCD.mp3")
	album := buildStringObject(stringAlbum, "OK Computer")
	artist := buildStringObject(stringArtist, "Radiohead")
	track := buildTrack(42, title, location, album, artist)
	trackList := buildTrackList(track)
	dataSet := buildDataSet(dataSetTracks, trackList)
	return buildDatabase(dataSet)
}

func buildDatabase(children ...[]byte) []byte {
	payload := flatten(children...)
	buf := make([]byte, 24)
	copy(buf[0:4], []byte(chunkDatabase))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(buf)+len(payload)))
	binary.LittleEndian.PutUint32(buf[16:20], 0x19)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(len(children)))
	return append(buf, payload...)
}

func buildDataSet(kind uint32, child []byte) []byte {
	buf := make([]byte, 16)
	copy(buf[0:4], []byte(chunkDataSet))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(buf)+len(child)))
	binary.LittleEndian.PutUint32(buf[12:16], kind)
	return append(buf, child...)
}

func buildTrackList(tracks ...[]byte) []byte {
	payload := flatten(tracks...)
	buf := make([]byte, 12)
	copy(buf[0:4], []byte(chunkTrackSet))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(tracks)))
	return append(buf, payload...)
}

func buildTrack(trackID uint32, strings ...[]byte) []byte {
	payload := flatten(strings...)
	buf := make([]byte, 160)
	copy(buf[0:4], []byte(chunkTrack))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(buf)+len(payload)))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(len(strings)))
	binary.LittleEndian.PutUint32(buf[16:20], trackID)
	binary.LittleEndian.PutUint32(buf[148:152], 1997)
	return append(buf, payload...)
}

func buildStringObject(kind uint32, value string) []byte {
	payload := encodeUTF16LE(value)
	buf := make([]byte, 40)
	copy(buf[0:4], []byte(chunkString))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(buf)+len(payload)))
	binary.LittleEndian.PutUint32(buf[12:16], kind)
	binary.LittleEndian.PutUint32(buf[28:32], uint32(len(payload)))
	return append(buf, payload...)
}

func encodeUTF16LE(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	buf := make([]byte, len(encoded)*2)
	for i, code := range encoded {
		binary.LittleEndian.PutUint16(buf[i*2:], code)
	}
	return buf
}

func flatten(chunks ...[]byte) []byte {
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}

	out := make([]byte, 0, total)
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}
