package ipoddb

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"

	"github.com/nskondratev/ipod-export/internal/model"
)

type SQLiteLibraryReader struct {
	logger *log.Logger
}

func NewSQLiteLibraryReader(logger *log.Logger) *SQLiteLibraryReader {
	return &SQLiteLibraryReader{logger: logger}
}

func (r *SQLiteLibraryReader) ReadTracks(ctx context.Context, mountPath string) ([]model.Track, error) {
	paths, err := locateSQLiteLibraries(mountPath)
	if err != nil {
		return nil, err
	}

	locations, err := loadSQLiteLocations(ctx, paths.locations, mountPath)
	if err != nil {
		return nil, err
	}

	libraryDB, err := sql.Open("sqlite", paths.library)
	if err != nil {
		return nil, fmt.Errorf("open sqlite library %q: %w", paths.library, err)
	}
	defer libraryDB.Close()

	rows, err := libraryDB.QueryContext(
		ctx,
		`select pid, coalesce(artist, ''), coalesce(title, ''), coalesce(album, ''), coalesce(year, 0)
		 from item
		 where is_song = 1`,
	)
	if err != nil {
		return nil, fmt.Errorf("query sqlite library tracks: %w", err)
	}
	defer rows.Close()

	tracks := make([]model.Track, 0, len(locations))
	for rows.Next() {
		var (
			pid    int64
			artist string
			title  string
			album  string
			year   int
		)
		if err := rows.Scan(&pid, &artist, &title, &album, &year); err != nil {
			return nil, fmt.Errorf("scan sqlite library track: %w", err)
		}

		location, ok := locations[pid]
		if !ok || location == "" {
			continue
		}

		tracks = append(tracks, model.Track{
			TrackID:  strconv.FormatInt(pid, 10),
			Artist:   artist,
			Title:    title,
			Album:    album,
			Year:     year,
			FilePath: location,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite library tracks: %w", err)
	}

	if r.logger != nil {
		r.logger.Printf("parsed sqlite iTunes library %q tracks=%d", paths.library, len(tracks))
	}

	return tracks, nil
}

type sqliteLibraryPaths struct {
	library   string
	locations string
}

func locateSQLiteLibraries(mountPath string) (sqliteLibraryPaths, error) {
	library := filepath.Join(mountPath, "iPod_Control", "iTunes", "iTunes Library.itlp", "Library.itdb")
	locations := filepath.Join(mountPath, "iPod_Control", "iTunes", "iTunes Library.itlp", "Locations.itdb")

	if _, err := os.Stat(library); err != nil {
		return sqliteLibraryPaths{}, fmt.Errorf("could not find sqlite library database under %q", mountPath)
	}
	if _, err := os.Stat(locations); err != nil {
		return sqliteLibraryPaths{}, fmt.Errorf("could not find sqlite locations database under %q", mountPath)
	}

	return sqliteLibraryPaths{library: library, locations: locations}, nil
}

func loadSQLiteLocations(ctx context.Context, dbPath, mountPath string) (map[int64]string, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite locations %q: %w", dbPath, err)
	}
	defer db.Close()

	rows, err := db.QueryContext(
		ctx,
		`select l.item_pid, coalesce(bl.path, ''), coalesce(l.location, '')
		 from location l
		 left join base_location bl on bl.id = l.base_location_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query sqlite locations: %w", err)
	}
	defer rows.Close()

	locations := make(map[int64]string)
	for rows.Next() {
		var (
			pid      int64
			basePath string
			location string
		)
		if err := rows.Scan(&pid, &basePath, &location); err != nil {
			return nil, fmt.Errorf("scan sqlite location: %w", err)
		}

		fullPath := filepath.Join(mountPath, filepath.FromSlash(basePath), filepath.FromSlash(location))
		locations[pid] = fullPath
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite locations: %w", err)
	}

	return locations, nil
}
