# AGENTS.md

## Project purpose

`ipod-export` is a Go CLI that exports music from mounted Apple iPods into a flat destination directory with readable filenames.

Supported database backends in the current codebase:

- classic binary iPod databases such as `iPod_Control/iTunes/iTunesDB`
- newer binary container files such as `iPod_Control/iTunes/iTunesCDB` when they still match the old `mhbd/mhsd/mhlt/mhit/mhod` layout
- newer SQLite-based libraries such as:
  - `iPod_Control/iTunes/iTunes Library.itlp/Library.itdb`
  - `iPod_Control/iTunes/iTunes Library.itlp/Locations.itdb`

If both database readers fail, the CLI can optionally fall back to scanning audio files directly with `--fallback-tags`.

## Code layout

- `cmd/ipod-export/main.go`
  - CLI flags
  - signal handling
  - reader selection and exporter wiring
- `internal/ipoddb/`
  - binary iPod DB parsing
  - SQLite library parsing for newer iPods
  - filesystem fallback reader
- `internal/exporter/`
  - copy planning
  - duplicate handling integration
  - progress bar
  - parallel worker pool
  - graceful shutdown behavior
- `internal/naming/`
  - output filename generation
  - sanitization
  - conflict resolution
- `internal/dedupe/`
  - `source`, `hash`, and `none` duplicate modes
- `internal/model/track.go`
  - shared `Track` model

## Important behavior to preserve

- Output directory must stay flat. Do not reintroduce artist/album folders unless explicitly requested.
- Filename conflict resolution order is intentional:
  1. `Artist - Title.ext`
  2. `Artist - Title (Album, Year).ext`
  3. `Artist - Title (Album, Year) (2).ext`
- Do not regress Unicode handling. Cyrillic and other non-ASCII metadata should remain readable in output filenames.
- Do not regress Windows-safe sanitization:
  - invalid characters are sanitized
  - trailing dots/spaces are removed
  - reserved names like `CON`/`COM1` are avoided
- Do not regress case-insensitive collision handling on macOS/Windows.
- Graceful shutdown currently means:
  - first `Ctrl+C` cancels work
  - second `Ctrl+C` forces exit 130
  - partial temp files are removed
- Parallel copying is opt-in via `--jobs`; default behavior should remain conservative.

## Device/database notes

- Older Shuffle/Nano style devices often work through the binary parser in `internal/ipoddb/itunesdb.go`.
- iPod Nano 7 style devices can expose:
  - `iTunesCDB`
  - SQLite `.itlp/*.itdb` files
- For Nano 7, the current code intentionally falls back from binary parsing to SQLite parsing if the `iTunesCDB` layout does not match the older record format.

## Testing guidance

Always run:

```bash
go test ./...
```

When working in restricted/sandboxed environments, Go may fail writing into the default module cache after SQLite dependency changes. If that happens, use temporary caches:

```bash
GOCACHE=/tmp/ipod-export-gocache GOMODCACHE=/tmp/ipod-export-gomodcache go test ./...
```

Useful manual checks:

- classic dry-run:

```bash
go run ./cmd/ipod-export --ipod "/path/to/ipod" --out /tmp/ipod-export --dry-run --verbose
```

- real copy without progress redraw:

```bash
go run ./cmd/ipod-export --ipod "/path/to/ipod" --out /tmp/ipod-export --jobs 4 --no-progress
```

- cross-platform compile smoke checks:

```bash
GOOS=linux GOARCH=amd64 go build ./cmd/ipod-export
GOOS=windows GOARCH=amd64 go build ./cmd/ipod-export
```

Run lint with the repo-pinned tool dependency:

```bash
make lint
```

## Debugging tips

- If filenames get unexpectedly truncated around `feat.` or similar text, inspect `internal/naming/naming.go` first.
- If parallel export reports `file already exists`, inspect collision planning in `internal/exporter/exporter.go`.
- If a newer iPod is mounted but `iTunesDB` is not found, inspect:
  - `iPod_Control/iTunes/iTunesCDB`
  - `iPod_Control/iTunes/iTunes Library.itlp/`
- If binary parsing fails after locating `iTunesCDB`, check whether SQLite fallback should be used instead of extending the legacy parser.

## Editing guidance

- Prefer small, surgical changes. The project is still compact and easy to reason about.
- Keep new parsing logic separated from export logic.
- Add tests with each parser or naming change. Regressions in metadata decoding and filename generation are easy to miss without tests.
- Do not add platform-specific behavior in shared code when a small build-tag file is cleaner.
