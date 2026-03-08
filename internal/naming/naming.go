package naming

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/nskondratev/ipod-export/internal/model"
)

const (
	unknownArtist = "Unknown Artist"
	unknownTitle  = "Unknown Title"
)

type ConflictResolver interface {
	Resolve(track model.Track, ext string, exists func(string) bool) string
}

type Resolver struct{}

func (Resolver) Resolve(track model.Track, ext string, exists func(string) bool) string {
	primary := BuildPrimary(track, ext)
	if !exists(primary) {
		return primary
	}

	secondary := BuildSecondary(track, ext)
	if secondary != primary && !exists(secondary) {
		return secondary
	}

	base := strings.TrimSuffix(secondary, filepath.Ext(secondary))
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !exists(candidate) {
			return candidate
		}
	}
}

func BuildPrimary(track model.Track, ext string) string {
	return sanitizeFilename(
		fmt.Sprintf("%s - %s", sanitizePart(defaultArtist(track.Artist)), sanitizePart(defaultTitle(track.Title))),
		ext,
	)
}

func BuildSecondary(track model.Track, ext string) string {
	detail := detailSuffix(track)
	if detail == "" {
		return BuildPrimary(track, ext)
	}

	return sanitizeFilename(fmt.Sprintf(
		"%s - %s (%s)%s",
		sanitizePart(defaultArtist(track.Artist)),
		sanitizePart(defaultTitle(track.Title)),
		sanitizePart(detail),
		ext,
	), "")
}

func detailSuffix(track model.Track) string {
	parts := make([]string, 0, 2)
	if album := strings.TrimSpace(track.Album); album != "" {
		parts = append(parts, album)
	}
	if track.Year > 0 {
		parts = append(parts, fmt.Sprintf("%d", track.Year))
	}
	return strings.Join(parts, ", ")
}

func sanitizePart(value string) string {
	replacer := strings.NewReplacer(
		"<", "-",
		">", "-",
		"/", "-",
		":", "-",
		"\"", "'",
		"\\", "-",
		"|", "-",
		"?", "",
		"*", "-",
		"\x00", "",
	)
	value = replacer.Replace(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Trim(value, ". ")
	if value == "" {
		return "Unknown"
	}
	return value
}

func sanitizeFilename(base, ext string) string {
	base = strings.TrimSpace(base)
	if ext != "" {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	base = strings.TrimRight(base, ". ")
	if base == "" {
		base = "Unknown"
	}
	if isWindowsReservedName(base) {
		base += "_"
	}
	return base + ext
}

func isWindowsReservedName(value string) bool {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimRight(normalized, ". ")
	normalized = strings.ToUpper(normalized)

	switch normalized {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}

	if len(normalized) == 4 {
		prefix := normalized[:3]
		suffix := normalized[3]
		if (prefix == "COM" || prefix == "LPT") && suffix >= '1' && suffix <= '9' {
			return true
		}
	}

	return false
}

func defaultArtist(value string) string {
	if strings.TrimSpace(value) == "" {
		return unknownArtist
	}
	return value
}

func defaultTitle(value string) string {
	if strings.TrimSpace(value) == "" {
		return unknownTitle
	}
	return value
}
