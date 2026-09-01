package files

import (
	"AutoAnimeDownloader/src/internal/logger"
	"AutoAnimeDownloader/src/internal/nyaa"

	"fmt"
	"path/filepath"
	"time"
)

// LibraryImporter adopts video files already sitting in the library folder into the
// saved-episodes records, so the daemon does not re-download content that is already on
// disk (e.g. files from a previous manager, or placed by hand).
//
// Matching is by folder name: the library is one folder per anime entry, named by
// sanitizeName(AnimeName) — the same function Organize uses — so daemon-created folders
// match exactly. A folder whose name matches no known anime is logged and ignored: the
// daemon never guesses.
//
// Adopted records are NORMAL records (not ManuallyManaged): they participate in the
// watched-delete flow exactly like downloaded episodes, per the owner's preference.
// EpisodeHash stays empty — there is no torrent behind these files — which the rest of
// the code already treats as "no torrent to remove" (removeEpisodesAndLinks skips the
// client half when the hash is empty).

// ImportedEpisode reports one file adopted by the scan, for the summary log.
type ImportedEpisode struct {
	AnimeName string
	Episode   int
	Path      string
}

// ImportLibrary walks the library folder and returns records for every video file that
// is on disk but has no saved-episode record yet, matched against knownAnimes (the
// sanitized names of the animes the daemon tracks, mapped to their AniList media ID and
// total episodes).
//
// existingKeys are the episode keys already recorded — those are skipped untouched, even
// if the on-disk file differs (an existing record owns that episode; the redownload/
// replace flows manage it).
func ImportLibrary(fs FileSystem, libraryPath string, knownAnimes map[string]KnownAnime, existingKeys map[EpisodeKey]bool) ([]EpisodeStruct, []ImportedEpisode) {
	if libraryPath == "" || len(knownAnimes) == 0 {
		return nil, nil
	}

	entries, err := fs.ReadDir(libraryPath)
	if err != nil {
		logger.Logger.Warn().Err(err).Str("library", libraryPath).Msg("Import scan: failed to read library folder")
		return nil, nil
	}

	var records []EpisodeStruct
	var imported []ImportedEpisode

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Legacy layout: the download folder lived inside the library.
		if entry.Name() == downloadDirName {
			continue
		}

		anime, ok := knownAnimes[entry.Name()]
		if !ok {
			continue
		}

		dir := filepath.Join(libraryPath, entry.Name())
		files, err := fs.ReadDir(dir)
		if err != nil {
			logger.Logger.Warn().Err(err).Str("folder", dir).Msg("Import scan: failed to read anime folder")
			continue
		}

		for _, f := range files {
			if f.IsDir() || !nyaa.IsVideoFile(f.Name()) {
				continue
			}
			num := nyaa.ExtractEpisodeNumber(f.Name())
			if num == nil || *num <= 0 {
				logger.Logger.Debug().
					Str("file", f.Name()).
					Str("anime", anime.Name).
					Msg("Import scan: no episode number in filename, skipped")
				continue
			}

			key := EpisodeKey{AnimeID: anime.ID, Episode: *num}
			if existingKeys[key] {
				continue
			}

			path := filepath.Join(dir, f.Name())
			records = append(records, EpisodeStruct{
				AnimeID:            anime.ID,
				AnimeTotalEpisodes: anime.TotalEpisodes,
				AnimeName:          anime.Name,
				EpisodeName:        fmt.Sprintf("%s - Episode %d (imported)", anime.Name, *num),
				EpisodeNumber:      *num,
				DownloadDate:       time.Now(),
				LibraryPaths:       []string{path},
			})
			imported = append(imported, ImportedEpisode{AnimeName: anime.Name, Episode: *num, Path: path})
		}
	}

	return records, imported
}

// KnownAnime describes one tracked anime for the import scan.
type KnownAnime struct {
	ID            int
	Name          string // unsanitized; the caller keys the map by sanitizeName(Name)
	TotalEpisodes int
}

// KnownAnimeInput is one tracked anime as the caller sees it. Synonyms are the AniList
// synonyms: release groups often name folders after the romaji/original title while the
// library manager used the English one (or vice versa), so every candidate name is
// indexed.
type KnownAnimeInput struct {
	ID            int
	Name          string
	TotalEpisodes int
	Synonyms      []string
}

// BuildKnownAnimes maps sanitized candidate folder names (main title + synonyms) to
// their identities. Duplicate sanitized names keep the first winner: one folder cannot
// hold two animes anyway.
func BuildKnownAnimes(animes []KnownAnimeInput) map[string]KnownAnime {
	out := make(map[string]KnownAnime)
	for _, a := range animes {
		candidates := append([]string{a.Name}, a.Synonyms...)
		for _, cand := range candidates {
			key := sanitizeName(cand)
			if key == "" {
				continue
			}
			if _, dup := out[key]; dup {
				continue
			}
			out[key] = KnownAnime{ID: a.ID, Name: a.Name, TotalEpisodes: a.TotalEpisodes}
		}
	}
	return out
}
