# ipod-export

`ipod-export` is a Go CLI for exporting music from a mounted Apple iPod Shuffle or iPod Nano.

The tool reads the iPod music database, resolves randomized on-device file paths, and copies tracks into a flat output directory with human-readable filenames.

## Current status

Implemented today:

- CLI entrypoint with `dry-run`, `verbose`, `overwrite`, duplicate handling, and fallback scanning
- Flat export layout with filename sanitization and conflict resolution
- Duplicate detection by source identity or SHA-256 hashing
- Graceful shutdown on `Ctrl+C` / `SIGTERM` with context cancellation and partial-file cleanup
- Progress bar during real copy with throughput, elapsed time, and ETA
- Initial binary `iTunesDB` parsing for:
  - `mhbd`
  - `mhsd` track datasets
  - `mhlt` track lists
  - `mhit` track records
  - `mhod` string objects for title, location, album, and artist
- SQLite library parsing for newer devices that store metadata in:
  - `iTunes Library.itlp/Library.itdb`
  - `iTunes Library.itlp/Locations.itdb`

Known limitations:

- `iTunesDB` is not fully implemented for every historical device/database variant yet
- `Year` parsing is currently best-effort and may need adjustment for some database versions
- `--fallback-tags` currently falls back to filesystem scanning; it does not parse ID3/AAC tags yet

## Requirements

- macOS, Linux, or Windows
- Go 1.26+
- A mounted iPod with an `iPod_Control` directory

## Build

```bash
go build -o bin/ipod-export ./cmd/ipod-export
```

Or run directly without building:

```bash
go run ./cmd/ipod-export --help
```

Cross-compile examples:

```bash
GOOS=linux GOARCH=amd64 go build -o bin/ipod-export-linux ./cmd/ipod-export
GOOS=windows GOARCH=amd64 go build -o bin/ipod-export.exe ./cmd/ipod-export
```

## Usage

Basic export:

```bash
go run ./cmd/ipod-export \
  --ipod /Volumes/IPOD \
  --out ~/Music/ipod-export
```

Windows example:

```powershell
ipod-export.exe --ipod "E:\\" --out "$HOME\\Music\\ipod-export"
```

Safe first pass with verbose logging and no writes:

```bash
go run ./cmd/ipod-export \
  --ipod /Volumes/IPOD \
  --out ~/Music/ipod-export \
  --dry-run \
  --verbose
```

Using the compiled binary:

```bash
./bin/ipod-export \
  --ipod /Volumes/IPOD \
  --out ~/Music/ipod-export
```

## Flags

- `--ipod`: path to the mounted iPod
- `--out`: destination directory for exported audio files
- `--dry-run`: print planned copies without writing files
- `--verbose`: enable detailed logging
- `--no-progress`: disable the interactive progress bar
- `--jobs`: number of files to copy in parallel
- `--overwrite`: allow overwriting existing destination files
- `--duplicates`: duplicate handling mode: `none`, `source`, or `hash`
- `--hash-duplicates`: shorthand for hash-based duplicate detection
- `--fallback-tags`: if database parsing fails, scan `iPod_Control/Music` directly

## Output naming

The exporter keeps a flat output directory.
Generated names are sanitized to be safe on macOS, Linux, and Windows.

Filename generation order:

1. `Artist - Title.ext`
2. `Artist - Title (Album, Year).ext`
3. `Artist - Title (Album, Year) (2).ext`

Fallbacks:

- missing artist -> `Unknown Artist`
- missing title -> `Unknown Title`
- missing album/year -> omitted from the secondary name

## Duplicate handling

- `source`: skip tracks already seen in the iPod database by `TrackID` or source path
- `hash`: compute SHA-256 and skip exact duplicate audio content
- `none`: disable duplicate detection

## How it resolves iPod paths

Older iPods typically store file locations in the classic colon-separated format:

```text
:iPod_Control:Music:F00:ABCD.mp3
```

`ipod-export` converts that into a real filesystem path under the mounted iPod root before copying.

Newer devices such as iPod Nano 7 can use SQLite-backed `Library.itdb` and `Locations.itdb`; the tool reads those automatically when present.

## Example workflow

1. Mount the iPod in Finder.
2. Run a dry-run first:

```bash
go run ./cmd/ipod-export \
  --ipod /Volumes/IPOD \
  --out ~/Music/ipod-export \
  --dry-run \
  --verbose
```

3. If the planned output looks correct, run the same command without `--dry-run`.

If your terminal does not handle carriage-return redraw cleanly, run the real export with `--no-progress`.

To speed up copying on flash-based iPods, try a small worker count such as:

```bash
go run ./cmd/ipod-export \
  --ipod /Volumes/IPOD \
  --out ~/Music/ipod-export \
  --jobs 4
```

If you stop the tool with `Ctrl+C`, the first signal starts graceful shutdown, stops scheduling new work, and removes any partially copied temporary file before exiting. A second `Ctrl+C` forces immediate exit with code `130`.

## Development

Run tests:

```bash
go test ./...
```
