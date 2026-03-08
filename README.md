# ipod-export

`ipod-export` is a Go CLI for exporting music from a mounted Apple iPod Shuffle or iPod Nano on macOS.

The tool reads the iPod music database from `iPod_Control/iTunes/iTunesDB`, resolves randomized on-device file paths, and copies tracks into a flat output directory with human-readable filenames.

## Current status

Implemented today:

- CLI entrypoint with `dry-run`, `verbose`, `overwrite`, duplicate handling, and fallback scanning
- Flat export layout with filename sanitization and conflict resolution
- Duplicate detection by source identity or SHA-256 hashing
- Initial binary `iTunesDB` parsing for:
  - `mhbd`
  - `mhsd` track datasets
  - `mhlt` track lists
  - `mhit` track records
  - `mhod` string objects for title, location, album, and artist

Known limitations:

- `iTunesDB` is not fully implemented for every historical device/database variant yet
- `Year` parsing is currently best-effort and may need adjustment for some database versions
- `--fallback-tags` currently falls back to filesystem scanning; it does not parse ID3/AAC tags yet

## Requirements

- macOS
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

## Usage

Basic export:

```bash
go run ./cmd/ipod-export \
  --ipod /Volumes/IPOD \
  --out ~/Music/ipod-export
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
- `--overwrite`: allow overwriting existing destination files
- `--duplicates`: duplicate handling mode: `none`, `source`, or `hash`
- `--hash-duplicates`: shorthand for hash-based duplicate detection
- `--fallback-tags`: if `iTunesDB` parsing fails, scan `iPod_Control/Music` directly

## Output naming

The exporter keeps a flat output directory.

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

The iPod database typically stores file locations in the classic colon-separated format:

```text
:iPod_Control:Music:F00:ABCD.mp3
```

`ipod-export` converts that into a real filesystem path under the mounted iPod root before copying.

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

## Development

Run tests:

```bash
go test ./...
```
