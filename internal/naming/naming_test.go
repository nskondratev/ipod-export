package naming

import (
	"testing"

	"github.com/nskondratev/ipod-export/internal/model"
)

func TestBuildPrimaryUsesDefaultsAndSanitizes(t *testing.T) {
	t.Parallel()

	got := BuildPrimary(model.Track{
		Artist: " AC/DC ",
		Title:  "Back:In/Black",
	}, ".mp3")

	want := "AC-DC - Back-In-Black.mp3"
	if got != want {
		t.Fatalf("BuildPrimary() = %q, want %q", got, want)
	}
}

func TestBuildSecondaryOmitsMissingMetadata(t *testing.T) {
	t.Parallel()

	got := BuildSecondary(model.Track{
		Artist: "Radiohead",
		Title:  "No Surprises",
	}, ".m4a")

	want := "Radiohead - No Surprises.m4a"
	if got != want {
		t.Fatalf("BuildSecondary() = %q, want %q", got, want)
	}
}

func TestResolverFallsBackToDetailedNameAndSuffix(t *testing.T) {
	t.Parallel()

	resolver := Resolver{}
	track := model.Track{
		Artist: "Radiohead",
		Title:  "No Surprises",
		Album:  "OK Computer",
		Year:   1997,
	}

	existing := map[string]struct{}{
		"Radiohead - No Surprises.mp3":                     {},
		"Radiohead - No Surprises (OK Computer, 1997).mp3": {},
	}

	got := resolver.Resolve(track, ".mp3", func(name string) bool {
		_, ok := existing[name]
		return ok
	})

	want := "Radiohead - No Surprises (OK Computer, 1997) (2).mp3"
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}
