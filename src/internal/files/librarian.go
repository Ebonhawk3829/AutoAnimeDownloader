package files

import (
	"AutoAnimeDownloader/src/internal/logger"
	"AutoAnimeDownloader/src/internal/nyaa"

	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Librarian organizes completed torrent files into the library by MOVING them out of the
// download folder. The torrent is removed from the client right after (no seeding after
// completion). The move requires download folder and library on the same filesystem —
// validated at config save (ProbePath).
type Librarian interface {
	// Organize moves the completed video files of a torrent into the library. A file whose
	// number can't be read keeps the raw name.
	// It returns the absolute paths of the library files it created (or that already
	// existed) so the caller can record them for later removal. It is idempotent: a
	// destination that already exists with the same size is reported and skipped.
	// A destination holding a *different* file is replaced by the new one, which
	// is what the redownload/replace flows want.
	Organize(req OrganizeRequest) ([]string, error)
	// RemoveFromLibrary deletes a single library file. A missing file is not an error.
	RemoveFromLibrary(path string) error
	// ProbePath valida, no save da config e a cada passe de verificacao, que a pasta de
	// download e a biblioteca compartilham o mesmo filesystem (o move exige isso) e cria
	// ambos os diretorios.
	ProbePath(completedPath, downloadPath string) error
}

// OrganizeRequest describes one torrent to organize into the library.
type OrganizeRequest struct {
	// TorrentDataDir is the on-disk root of the torrent's content (<DataDir>/<id>).
	TorrentDataDir string
	AnimeName      string
	CompletedPath  string
	// IsBatch marks multi-episode/movie torrents: the episode number comes from each
	// file's own name instead of EpisodeNumber.
	IsBatch bool
	// EpisodeNumber is used for the standard name ("Anime - E05.ext") of a single
	// episode. Zero/nil means "missing data" — the raw name is kept.
	EpisodeNumber *int
	// TotalEpisodes e o total de episodios da ENTRADA da AniList. Usado so para detectar pack
	// com numeracao continua (ver packEpisodeOffset); zero = desconhecido, nada e deslocado.
	TotalEpisodes int
}

type organizer struct {
	fs   FileSystem
	move func(oldname, newname string) error
}

// NewLibrarian returns a Librarian backed by the given FileSystem. The move function
// defaults to fs.Rename; both Organize and ProbePath use it, so they never diverge.
func NewLibrarian(fs FileSystem) *organizer {
	return &organizer{fs: fs, move: fs.Rename}
}

// A lista de extensoes mora no pacote nyaa: PackFileRange le a MESMA lista de arquivos do pack
// antes de baixar, e duas listas de extensao divergiriam.
func isVideoFile(name string) bool {
	return nyaa.IsVideoFile(name)
}

// sanitizeName strips filesystem-invalid characters. O marcador de season e MANTIDO: uma
// pasta por entrada da AniList (decisions.md #45).
func sanitizeName(name string) string {
	sanitized := stripInvalidChars(name)
	sanitized = strings.TrimSpace(sanitized)
	sanitized = strings.ReplaceAll(sanitized, "  ", " ")
	return sanitized
}

func stripInvalidChars(name string) string {
	invalidChars := []string{":", "<", ">", "|", "?", "*", "\"", "\\", "/"}
	sanitized := name
	for _, char := range invalidChars {
		sanitized = strings.ReplaceAll(sanitized, char, "")
	}
	return sanitized
}

// standardName returns "Anime - E05.ext". Sempre aplicado quando o numero do episodio e
// conhecido: nomes limpos sao mais faceis de casar (mpv-anilist-updater/guessit) e de
// auditar quando Syncthing, mpv e o daemon tocam os mesmos arquivos.
func standardName(animeName string, episodeNumber int, ext string) string {
	return fmt.Sprintf("%s - E%02d%s", sanitizeName(animeName), episodeNumber, ext)
}

// packEpisodeOffset devolve quanto subtrair do numero lido no nome do arquivo para chegar ao
// numero da ENTRADA da AniList. Pack de season >= 2 com numeracao continua (arquivos 13-24 para
// uma entrada de 12 episodios) e o caso; qualquer outro devolve 0.
func packEpisodeOffset(numbers []int, totalEpisodes int) int {
	if totalEpisodes <= 0 || len(numbers) < totalEpisodes {
		return 0
	}
	lowest := numbers[0]
	for _, n := range numbers {
		if n < lowest {
			lowest = n
		}
	}
	if lowest <= totalEpisodes {
		return 0
	}
	return lowest - 1
}

func (o *organizer) Organize(req OrganizeRequest) ([]string, error) {
	// Guard first: with an empty CompletedPath, filepath.Join below yields a relative
	// path and MkdirAll would create the library folder in the process' working dir.
	if req.CompletedPath == "" {
		return nil, fmt.Errorf("completed anime path is not configured")
	}

	videoFiles, err := o.collectVideoFiles(req.TorrentDataDir)
	if err != nil {
		return nil, err
	}
	if len(videoFiles) == 0 {
		return nil, fmt.Errorf("no video files found in %s", req.TorrentDataDir)
	}

	destDir := filepath.Join(req.CompletedPath, sanitizeName(req.AnimeName))

	// Track whether we created destDir, so we can clean it up on a cross-device failure
	// without leaving an orphan folder in the library.
	dirExisted := true
	if _, statErr := o.fs.Stat(destDir); statErr != nil {
		dirExisted = false
	}
	if err := o.fs.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create library folder %s: %w", destDir, err)
	}

	// Standard naming for a genuine single episode with one video file. EpisodeNumber == 0
	// means "missing data" (AniList numbers episodes from 1), so we fall back to the raw
	// filename instead of colliding every episode on "Anime - E00".
	singleRename := !req.IsBatch && req.EpisodeNumber != nil &&
		*req.EpisodeNumber > 0 && len(videoFiles) == 1

	var packOffset int
	if req.IsBatch {
		var numbers []int
		for _, rel := range videoFiles {
			if n := nyaa.ExtractEpisodeNumber(filepath.Base(rel)); n != nil {
				numbers = append(numbers, *n)
			}
		}
		packOffset = packEpisodeOffset(numbers, req.TotalEpisodes)
	}

	used := make(map[string]bool, len(videoFiles))
	var created []string
	for _, rel := range videoFiles {
		src := filepath.Join(req.TorrentDataDir, rel)

		destName := filepath.Base(rel)
		switch {
		case singleRename:
			destName = standardName(req.AnimeName, *req.EpisodeNumber, filepath.Ext(rel))
		case req.IsBatch:
			// Pack: o numero sai do proprio nome do arquivo, para os episodios do pack se
			// misturarem na pasta com os avulsos em vez de manter o nome cru do fansub.
			// Sem numero legivel (NCOP/NCED, extra, filme) ou com colisao entre dois
			// arquivos do mesmo pack, fica o nome cru.
			if n := nyaa.ExtractEpisodeNumber(destName); n != nil {
				if sn := standardName(req.AnimeName, *n-packOffset, filepath.Ext(rel)); !used[sn] {
					destName = sn
				}
			}
		}
		// Dois arquivos do MESMO torrent com o mesmo basename (pack multi-season com uma
		// subpasta por season, achatada por collectVideoFiles) apontariam para o mesmo dest, e o
		// segundo cairia no ramo de "bytes diferentes" removendo o arquivo do primeiro — um
		// episodio sumia da biblioteca. O caminho relativo e unico dentro do torrent.
		if used[destName] {
			destName = strings.ReplaceAll(rel, string(filepath.Separator), " - ")
		}
		used[destName] = true
		dest := filepath.Join(destDir, destName)

		if _, statErr := o.fs.Stat(dest); statErr == nil {
			// Different bytes under the same name (redownload/replace): the user asked
			// for the swap, so the new file wins.
			logger.Logger.Info().
				Str("source", src).
				Str("destination", dest).
				Msg("Replacing existing library file with the newly downloaded one")
			if err := o.fs.Remove(dest); err != nil {
				o.cleanupIfEmpty(destDir, dirExisted)
				return nil, fmt.Errorf("failed to replace existing library file %s: %w", dest, err)
			}
		}

		if err := o.move(src, dest); err != nil {
			if !isCrossDevice(err) {
				o.cleanupIfEmpty(destDir, dirExisted)
				return nil, fmt.Errorf("failed to move %s -> %s: %w", src, dest, err)
			}
			// EXDEV entre pastas no MESMO disco: montagens bind separadas de uma mesma
			// particao podem recusar rename() com EXDEV mesmo com st_dev igual (o kernel
			// compara as montagens, nao os dispositivos). Fallback copia+apaga — semanticamente
			// o mesmo move para este fluxo, pois o torrent sera removido logo depois.
			if err := o.copyThenDelete(src, dest); err != nil {
				o.cleanupIfEmpty(destDir, dirExisted)
				return nil, fmt.Errorf("failed to copy %s -> %s: %w", src, dest, err)
			}
		}
		created = append(created, dest)
	}

	return created, nil
}

// copyThenDelete copia src para dest byte a byte e apaga src. Fallback do rename quando o
// kernel recusa o move entre bind mounts da mesma particao.
func (o *organizer) copyThenDelete(src, dest string) error {
	in, err := o.fs.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := o.fs.Create(dest)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = o.fs.Remove(dest)
		return err
	}
	if err := out.Close(); err != nil {
		_ = o.fs.Remove(dest)
		return err
	}
	return o.fs.Remove(src)
}

func (o *organizer) RemoveFromLibrary(path string) error {
	if path == "" {
		return nil
	}
	if err := o.fs.Remove(path); err != nil {
		if _, statErr := o.fs.Stat(path); statErr != nil {
			// Already gone — not an error.
			return nil
		}
		return err
	}
	return nil
}

func (o *organizer) ProbePath(completedPath, downloadPath string) error {
	if completedPath == "" {
		return fmt.Errorf("completed anime path must be set")
	}
	if downloadPath == "" {
		return fmt.Errorf("download path must be set")
	}
	if err := o.fs.MkdirAll(completedPath, 0755); err != nil {
		return fmt.Errorf("cannot access completed path %s: %w", completedPath, err)
	}
	if err := o.fs.MkdirAll(downloadPath, 0755); err != nil {
		return fmt.Errorf("cannot create download folder %s: %w", downloadPath, err)
	}

	// O move exige o mesmo filesystem. Sonda com rename: a mesma operacao que Organize
	// usa, entao nunca discorda dele.
	probeSrc := filepath.Join(downloadPath, ".aad_move_probe")
	probeDst := filepath.Join(completedPath, ".aad_move_probe")

	// Limpa sobras de uma sonda anterior.
	_ = o.fs.Remove(probeSrc)
	_ = o.fs.Remove(probeDst)

	if err := o.fs.WriteFile(probeSrc, []byte("probe"), 0644); err != nil {
		return fmt.Errorf("cannot write to download path %s: %w", downloadPath, err)
	}
	defer func() { _ = o.fs.Remove(probeSrc) }()

	if err := o.move(probeSrc, probeDst); err != nil {
		if !isCrossDevice(err) {
			return fmt.Errorf("move probe failed: %w", err)
		}
		// EXDEV entre bind mounts da mesma particao: aceitavel, o Organize tem fallback
		// copia+apaga. Valida apenas que a biblioteca aceita escrita.
		if err := o.fs.WriteFile(probeDst, []byte("probe"), 0644); err != nil {
			return fmt.Errorf("download folder (%s) and library (%s) rejected both move and write: %w", downloadPath, completedPath, err)
		}
	}
	_ = o.fs.Remove(probeDst)

	return nil
}

// collectVideoFiles returns the video-file paths under root, relative to root.
func (o *organizer) collectVideoFiles(root string) ([]string, error) {
	var out []string
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		entries, err := o.fs.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			childRel := filepath.Join(rel, e.Name())
			childAbs := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if err := walk(childAbs, childRel); err != nil {
					return err
				}
				continue
			}
			if isVideoFile(e.Name()) {
				out = append(out, childRel)
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func (o *organizer) cleanupIfEmpty(dir string, dirExisted bool) {
	if dirExisted {
		return
	}
	// os.Remove only succeeds on an empty dir; a non-empty pre-existing dir is left alone.
	_ = o.fs.Remove(dir)
}
