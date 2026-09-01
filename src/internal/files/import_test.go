package files

import (
	"os"
	"path/filepath"
	"testing"
)

func importTestFS(t *testing.T) FileSystem {
	t.Helper()
	return NewOSFileSystem()
}

// Happy path: a folder named exactly like the sanitized anime name, with numbered episode
// files, adopts every file that has no record yet.
func TestImportLibrary_AdoptsExistingFiles(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, "My Anime", "My Anime - E01.mkv"), "a")
	writeFile(t, filepath.Join(library, "My Anime", "My Anime - E02.mkv"), "b")
	writeFile(t, filepath.Join(library, "My Anime", "readme.txt"), "not video")

	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 42, Name: "My Anime", TotalEpisodes: 12}})
	existing := map[EpisodeKey]bool{}

	records, imported := ImportLibrary(importTestFS(t), library, known, existing)

	if len(records) != 2 || len(imported) != 2 {
		t.Fatalf("records = %d, imported = %d, want 2/2", len(records), len(imported))
	}
	for _, r := range records {
		if r.AnimeID != 42 || r.AnimeName != "My Anime" || r.AnimeTotalEpisodes != 12 {
			t.Errorf("record identity wrong: %+v", r)
		}
		if r.EpisodeHash != "" {
			t.Errorf("imported record must have no hash (no torrent behind it): %+v", r)
		}
		if len(r.LibraryPaths) != 1 {
			t.Errorf("imported record must point at the on-disk file: %+v", r)
		}
		if r.ManuallyManaged {
			t.Errorf("imported records are normal records (watched-delete applies): %+v", r)
		}
	}
	// readme.txt must not appear in any record path.
	for _, imp := range imported {
		if filepath.Ext(imp.Path) == ".txt" {
			t.Errorf("non-video file adopted: %v", imp)
		}
	}
}

// Episodes that already have records are skipped untouched.
func TestImportLibrary_SkipsExistingRecords(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, "My Anime", "My Anime - E01.mkv"), "a")
	writeFile(t, filepath.Join(library, "My Anime", "My Anime - E02.mkv"), "b")

	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 42, Name: "My Anime"}})
	existing := map[EpisodeKey]bool{{AnimeID: 42, Episode: 1}: true}

	records, _ := ImportLibrary(importTestFS(t), library, known, existing)

	if len(records) != 1 {
		t.Fatalf("records = %d, want 1 (only E02)", len(records))
	}
	if records[0].EpisodeNumber != 2 {
		t.Errorf("adopted episode %d, want 2", records[0].EpisodeNumber)
	}
}

// A folder matching no known anime is ignored entirely — the daemon never guesses.
func TestImportLibrary_IgnoresUnknownFolders(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, "Unknown Show", "Unknown Show - E01.mkv"), "a")
	writeFile(t, filepath.Join(library, "My Anime", "My Anime - E01.mkv"), "b")

	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 42, Name: "My Anime"}})

	records, _ := ImportLibrary(importTestFS(t), library, known, map[EpisodeKey]bool{})

	if len(records) != 1 {
		t.Fatalf("records = %d, want 1 (only the known anime)", len(records))
	}
	if records[0].AnimeName != "My Anime" {
		t.Errorf("adopted from the wrong anime: %+v", records[0])
	}
}

// Folder names are matched sanitized: the daemon creates folders via sanitizeName, so a
// colon-bearing title lands in a colon-free folder and must still match.
func TestImportLibrary_MatchesSanitizedFolderNames(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, "Mushoku Tensei Jobless Reincarnation", "Mushoku Tensei Jobless Reincarnation - E05.mkv"), "a")

	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 7, Name: "Mushoku Tensei: Jobless Reincarnation"}})

	records, _ := ImportLibrary(importTestFS(t), library, known, map[EpisodeKey]bool{})

	if len(records) != 1 || records[0].AnimeID != 7 {
		t.Fatalf("records = %+v, want 1 record for anime 7", records)
	}
}

// Files whose names carry no parseable episode number are skipped.
func TestImportLibrary_SkipsUnparseableNames(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, "My Anime", "NCOP.mkv"), "a")
	writeFile(t, filepath.Join(library, "My Anime", "My Anime - E01.mkv"), "b")

	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 42, Name: "My Anime"}})

	records, _ := ImportLibrary(importTestFS(t), library, known, map[EpisodeKey]bool{})

	if len(records) != 1 || records[0].EpisodeNumber != 1 {
		t.Fatalf("records = %+v, want only E01", records)
	}
}

// Guards: empty library path or no known animes means no scan.
func TestImportLibrary_EmptyInputs(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, "My Anime", "My Anime - E01.mkv"), "a")
	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 42, Name: "My Anime"}})

	if recs, _ := ImportLibrary(importTestFS(t), "", known, nil); len(recs) != 0 {
		t.Errorf("empty library path should return nothing, got %v", recs)
	}
	if recs, _ := ImportLibrary(importTestFS(t), library, nil, nil); len(recs) != 0 {
		t.Errorf("no known animes should return nothing, got %v", recs)
	}
}

// The legacy .torrents folder inside the library is never scanned.
func TestImportLibrary_SkipsLegacyTorrentsDir(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, downloadDirName, "someid", "ep.mkv"), "a")

	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 42, Name: "someid"}})

	records, _ := ImportLibrary(importTestFS(t), library, known, map[EpisodeKey]bool{})
	if len(records) != 0 {
		t.Errorf("legacy download dir must be skipped, got %v", records)
	}
}

func TestBuildKnownAnimes_DuplicateSanitizedNamesKeepFirst(t *testing.T) {
	known := BuildKnownAnimes([]KnownAnimeInput{
		{ID: 1, Name: "Show: Title"},
		{ID: 2, Name: "Show Title"},
	})
	got, ok := known["Show Title"]
	if !ok || got.ID != 1 {
		t.Errorf("expected first entry (ID 1) to win, got %+v (ok=%v)", got, ok)
	}
}

func TestBuildKnownAnimes_SkipsEmptyNames(t *testing.T) {
	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 1, Name: "///"}})
	if len(known) != 0 {
		t.Errorf("empty sanitized name should be skipped, got %v", known)
	}
}

// Compile-time guard: the scan must not blow up on a library folder that does not exist.
func TestImportLibrary_MissingLibraryIsNoop(t *testing.T) {
	known := BuildKnownAnimes([]KnownAnimeInput{{ID: 42, Name: "My Anime"}})
	records, imported := ImportLibrary(importTestFS(t), filepath.Join(os.TempDir(), "does-not-exist"), known, nil)
	if len(records) != 0 || len(imported) != 0 {
		t.Errorf("missing library should return nothing, got %v / %v", records, imported)
	}
}

// Synonyms are indexed: a folder named after the romaji/original title matches an entry
// whose main title is the English one.
func TestImportLibrary_MatchesViaSynonyms(t *testing.T) {
	tmp := t.TempDir()
	library := filepath.Join(tmp, "library")
	writeFile(t, filepath.Join(library, "Seihantai na Kimi to Boku 2nd Season", "[Erai-raws] Seihantai na Kimi to Boku 2nd Season - 01 [1080p].mkv"), "a")

	known := BuildKnownAnimes([]KnownAnimeInput{
		{ID: 210031, Name: "You and I Are Polar Opposites Season 2", Synonyms: []string{"Seihantai na Kimi to Boku 2nd Season", "正反対な君と僕 第2期"}},
	})

	records, _ := ImportLibrary(importTestFS(t), library, known, map[EpisodeKey]bool{})

	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].AnimeID != 210031 || records[0].AnimeName != "You and I Are Polar Opposites Season 2" {
		t.Errorf("record identity wrong: %+v", records[0])
	}
	if records[0].EpisodeNumber != 1 {
		t.Errorf("episode = %d, want 1", records[0].EpisodeNumber)
	}
}
