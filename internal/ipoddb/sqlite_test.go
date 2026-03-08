package ipoddb

import (
	"context"
	"database/sql"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteLibraryReaderReadTracks(t *testing.T) {
	t.Parallel()

	mountPath := t.TempDir()
	audioPath := filepath.Join(mountPath, "iPod_Control", "Music", "F00", "ABCD.mp3")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(audio) error = %v", err)
	}
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatalf("WriteFile(audio) error = %v", err)
	}

	base := filepath.Join(mountPath, "iPod_Control", "iTunes", "iTunes Library.itlp")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("MkdirAll(base) error = %v", err)
	}

	libraryPath := filepath.Join(base, "Library.itdb")
	locationsPath := filepath.Join(base, "Locations.itdb")

	libraryDB := openSQLiteDB(t, libraryPath)
	defer libraryDB.Close()
	mustExecSQLite(t, libraryDB, `create table item (pid integer primary key, is_song integer, year integer, title text, artist text, album text)`)
	mustExecSQLite(t, libraryDB, `insert into item(pid, is_song, year, title, artist, album) values (42, 1, 1997, 'No Surprises', 'Radiohead', 'OK Computer')`)

	locationsDB := openSQLiteDB(t, locationsPath)
	defer locationsDB.Close()
	mustExecSQLite(t, locationsDB, `create table base_location (id integer primary key, path text)`)
	mustExecSQLite(t, locationsDB, `create table location (item_pid integer not null, sub_id integer not null default 0, base_location_id integer default 0, location_type integer, location text, extension integer, kind_id integer default 0, date_created integer default 0, file_size integer default 0, file_creator integer, file_type integer, num_dir_levels_file integer, num_dir_levels_lib integer, primary key (item_pid, sub_id))`)
	mustExecSQLite(t, locationsDB, `insert into base_location(id, path) values (1, 'iPod_Control/Music')`)
	mustExecSQLite(t, locationsDB, `insert into location(item_pid, sub_id, base_location_id, location) values (42, 0, 1, 'F00/ABCD.mp3')`)

	reader := NewSQLiteLibraryReader(log.New(io.Discard, "", 0))
	tracks, err := reader.ReadTracks(context.Background(), mountPath)
	if err != nil {
		t.Fatalf("ReadTracks() error = %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks len = %d, want 1", len(tracks))
	}
	if tracks[0].FilePath != audioPath {
		t.Fatalf("FilePath = %q, want %q", tracks[0].FilePath, audioPath)
	}
	if tracks[0].Title != "No Surprises" || tracks[0].Artist != "Radiohead" {
		t.Fatalf("unexpected track metadata: %+v", tracks[0])
	}
}

func openSQLiteDB(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(%q) error = %v", path, err)
	}
	return db
}

func mustExecSQLite(t *testing.T, db *sql.DB, query string) {
	t.Helper()

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("Exec(%q) error = %v", query, err)
	}
}
