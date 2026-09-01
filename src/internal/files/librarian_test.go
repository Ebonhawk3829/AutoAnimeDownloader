package files

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func intPtr(i int) *int { return &i }

// linkCount reports the hardlink count of path. The syscall struct behind FileInfo.Sys()
// is platform-specific (and has no Nlink at all on Windows), so it is read reflectively:
// ok == false means "this platform does not expose it" and the caller should skip.
func linkCount(t *testing.T, path string) (int, bool) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	sys := info.Sys()
	if sys == nil {
		return 0, false
	}
	v := reflect.Indirect(reflect.ValueOf(sys))
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	field := v.FieldByName("Nlink")
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int(field.Uint()), true
	case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(field.Int()), true
	}
	return 0, false
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestJellyfinName(t *testing.T) {
	cases := []struct {
		anime string
		ep    int
		ext   string
		want  string
	}{
		{"My Anime", 5, ".mkv", "My Anime - E05.mkv"},
		{"My Anime", 12, ".mp4", "My Anime - E12.mp4"},
		{"Anime: Colon", 1, ".mkv", "Anime Colon - E01.mkv"},
	}
	for _, c := range cases {
		if got := standardName(c.anime, c.ep, c.ext); got != c.want {
			t.Errorf("standardName(%q,%d,%q) = %q, want %q", c.anime, c.ep, c.ext, got, c.want)
		}
	}
}

// O marcador de season TEM de sobreviver: cada season e uma entrada propria na AniList, com
// id e capa proprios. Apagar o marcador jogava Season 1/2/3 na mesma pasta e o Jellyfin
// casava tudo com a season 1 (decisions.md #45).
func TestSanitizeNameKeepsSeasonMarker(t *testing.T) {
	cases := map[string]string{
		"Anime/Name:Test":                                       "AnimeNameTest",
		"My Anime Season 2":                                     "My Anime Season 2",
		"Show 2nd Season":                                       "Show 2nd Season",
		"Mushoku Tensei: Jobless Reincarnation":                 "Mushoku Tensei Jobless Reincarnation",
		"Mushoku Tensei: Jobless Reincarnation Season 3":        "Mushoku Tensei Jobless Reincarnation Season 3",
		"Mushoku Tensei: Jobless Reincarnation Cour 2":          "Mushoku Tensei Jobless Reincarnation Cour 2",
		"Mushoku Tensei: Jobless Reincarnation Season 2 Part 2": "Mushoku Tensei Jobless Reincarnation Season 2 Part 2",
	}
	seen := make(map[string]string)
	for in, want := range cases {
		got := sanitizeName(in)
		if got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%q e %q caem na mesma pasta %q", other, in, got)
		}
		seen[got] = in
	}
}

func TestOrganizeSingleEpisodeJellyfin(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "torrentid")
	completed := filepath.Join(tmp, "completed")
	src := filepath.Join(dataDir, "raw episode name.mkv")
	writeFile(t, src, "video-bytes")

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "My Anime",
		CompletedPath:  completed,
		EpisodeNumber:  intPtr(5),
		
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}

	wantDest := filepath.Join(completed, "My Anime", "My Anime - E05.mkv")
	if len(created) != 1 || created[0] != wantDest {
		t.Fatalf("created = %v, want [%s]", created, wantDest)
	}

	// Move semantics: the source left the download folder.
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source should have been moved out of the download folder")
	}
	if _, err := os.Stat(wantDest); err != nil {
		t.Fatalf("dest missing: %v", err)
	}
}

func TestOrganizeBatchRawNames(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "batchid")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 01 [1080p].mkv"), "a")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 02 [1080p].mkv"), "b")
	writeFile(t, filepath.Join(dataDir, "readme.txt"), "not video")

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "Anime",
		CompletedPath:  completed,
		IsBatch:        true,
		
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created = %v, want 2 video links (txt skipped)", created)
	}
	// Rename-always-on: pack files with readable episode numbers get standard names.
	for _, name := range []string{"Anime - E01.mkv", "Anime - E02.mkv"} {
		p := filepath.Join(completed, "Anime", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected standard-named file %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(completed, "Anime", "readme.txt")); err == nil {
		t.Errorf("non-video file should not be moved")
	}
}

// Com a flag ligada, os arquivos do pack ganham o MESMO nome dos baixados avulsos, na mesma
// pasta do anime. O que nao tem numero legivel (NCOP) e o segundo arquivo que cai no mesmo
// numero ficam com o nome cru — renomear os dois colidiria e um sobrescreveria o outro.
func TestOrganizeBatchJellyfinNames(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "batchid")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 01 [1080p].mkv"), "a")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 02 [1080p].mkv"), "b")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 02v2 [1080p].mkv"), "c")
	writeFile(t, filepath.Join(dataDir, "NCOP.mkv"), "d")

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "Anime",
		CompletedPath:  completed,
		IsBatch:        true,
		
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if len(created) != 4 {
		t.Fatalf("created = %v, want 4 links", created)
	}
	for _, name := range []string{
		"Anime - E01.mkv",
		"Anime - E02.mkv",
		"[Sub] Anime - 02v2 [1080p].mkv", // colisao com E02: nome cru
		"NCOP.mkv",                       // sem numero: nome cru
	} {
		if _, err := os.Stat(filepath.Join(completed, "Anime", name)); err != nil {
			t.Errorf("expected link %s: %v", name, err)
		}
	}
}

// Grupo que separa o nome com "_": o numero do episodio sai do arquivo igual ao de um nome com
// espacos, senao o pack inteiro ia para a biblioteca com nome cru.
func TestOrganizeBatchUnderscoreNames(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "batchid")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "[DB]Anime_-_01_(Dual Audio_10bit_BD1080p_x265).mkv"), "a")
	writeFile(t, filepath.Join(dataDir, "[DB]Anime_-_02_(Dual Audio_10bit_BD1080p_x265).mkv"), "b")
	writeFile(t, filepath.Join(dataDir, "[DB]Anime_-_NCED01_(10bit_BD1080p_x265).mkv"), "c")

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "Anime",
		CompletedPath:  completed,
		IsBatch:        true,
		
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("created = %v, want 3 links", created)
	}
	for _, name := range []string{
		"Anime - E01.mkv",
		"Anime - E02.mkv",
		"[DB]Anime_-_NCED01_(10bit_BD1080p_x265).mkv", // sem numero de episodio: nome cru
	} {
		if _, err := os.Stat(filepath.Join(completed, "Anime", name)); err != nil {
			t.Errorf("expected link %s: %v", name, err)
		}
	}
}

// Pack e avulso do mesmo anime dividem UMA pasta (destDir sai de AnimeName, nunca do nome do
// torrent) e, com a flag, o mesmo padrao de nome — entao os episodios se misturam em ordem.
func TestOrganizeBatchAndSingleShareOneFolder(t *testing.T) {
	tmp := t.TempDir()
	completed := filepath.Join(tmp, "completed")

	batchDir := filepath.Join(tmp, "save", "batch")
	writeFile(t, filepath.Join(batchDir, "[Sub] Anime - 01 [1080p].mkv"), "a")
	singleDir := filepath.Join(tmp, "save", "single")
	writeFile(t, filepath.Join(singleDir, "[Other] Anime - 02 [720p].mkv"), "b")

	lib := NewLibrarian(NewOSFileSystem())
	if _, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: batchDir, AnimeName: "Anime", CompletedPath: completed,
		IsBatch: true,
	}); err != nil {
		t.Fatalf("Organize batch: %v", err)
	}
	if _, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: singleDir, AnimeName: "Anime", CompletedPath: completed,
		EpisodeNumber: intPtr(2),
	}); err != nil {
		t.Fatalf("Organize single: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(completed, "Anime"))
	if err != nil {
		t.Fatalf("read library folder: %v", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"Anime - E01.mkv", "Anime - E02.mkv"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("library folder = %v, want %v", got, want)
	}
}

// With move semantics, a second Organize on the same (now empty) data dir reports "no
// video files" instead of re-reporting the same path — idempotency at the job level comes
// from the LibraryPaths marker, not from re-running the move.
func TestOrganizeSecondRunOnEmptyDataDirErrors(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "id")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "ep.mkv"), "x")

	lib := NewLibrarian(NewOSFileSystem())
	req := OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "A", CompletedPath: completed,
		EpisodeNumber: intPtr(1),
	}
	first, err := lib.Organize(req)
	if err != nil {
		t.Fatalf("first Organize: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first = %v, want 1 path", first)
	}
	second, err := lib.Organize(req)
	if err == nil {
		t.Fatalf("second Organize on empty data dir should error, got %v", second)
	}
}

// Destination already exists (e.g. a leftover from a previous run): the newly moved file
// replaces it, and the path is still reported.
func TestOrganizeDestinationAlreadyExistsIsReplaced(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "id")
	completed := filepath.Join(tmp, "completed")
	src := filepath.Join(dataDir, "ep.mkv")
	writeFile(t, src, "video-bytes")

	// A stale file already sitting at the destination name.
	destDir := filepath.Join(completed, "A")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(destDir, "A - E01.mkv")
	writeFile(t, dest, "stale-bytes")

	lib := NewLibrarian(NewOSFileSystem())
	req := OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "A", CompletedPath: completed,
		EpisodeNumber: intPtr(1),
	}
	created, err := lib.Organize(req)
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if len(created) != 1 || created[0] != dest {
		t.Fatalf("created = %v, want [%s]", created, dest)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "video-bytes" {
		t.Errorf("dest content = %q, want the newly moved bytes", data)
	}
}

// Destination exists but points at different bytes (redownload/replace): the new file
// wins, and the seeded source keeps exactly one extra link.
func TestOrganizeReplacesDifferentFileAtDestination(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "newid")
	completed := filepath.Join(tmp, "completed")
	src := filepath.Join(dataDir, "new release.mkv")
	writeFile(t, src, "new-bytes")

	// A stale, unrelated file already sitting at the destination name.
	dest := filepath.Join(completed, "A", "A - E01.mkv")
	writeFile(t, dest, "stale-bytes")

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "A", CompletedPath: completed,
		EpisodeNumber: intPtr(1),
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if len(created) != 1 || created[0] != dest {
		t.Fatalf("created = %v, want [%s]", created, dest)
	}

	// The new file won and the source left the download folder.
	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(content) != "new-bytes" {
		t.Errorf("dest content = %q, want %q", content, "new-bytes")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source should have been moved out of the download folder")
	}
}
func TestOrganizeEmptyCompletedPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "save", "id")
	writeFile(t, filepath.Join(dataDir, "ep.mkv"), "x")

	cwd := t.TempDir()
	t.Chdir(cwd)

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "My Anime", CompletedPath: "",
		EpisodeNumber: intPtr(1),
	})
	if err == nil {
		t.Fatalf("expected error for empty completed path, created = %v", created)
	}
	entries, readErr := os.ReadDir(cwd)
	if readErr != nil {
		t.Fatalf("read cwd: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("Organize wrote %d entries into the working directory: %v", len(entries), entries)
	}
}

// EpisodeNumber == 0 is missing data, never a real episode: fall back to the raw name.
func TestOrganizeEpisodeNumberZeroKeepsRawName(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "id")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "raw episode name.mkv"), "x")

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "A", CompletedPath: completed,
		EpisodeNumber: intPtr(0),
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	want := filepath.Join(completed, "A", "raw episode name.mkv")
	if len(created) != 1 || created[0] != want {
		t.Fatalf("created = %v, want [%s]", created, want)
	}
	if _, err := os.Stat(filepath.Join(completed, "A", "A - E00.mkv")); err == nil {
		t.Errorf("episode 0 must never produce an E00 name")
	}
}

func TestOrganizeNoVideoFiles(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "id")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "readme.txt"), "x")

	lib := NewLibrarian(NewOSFileSystem())
	_, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "A", CompletedPath: completed,
		EpisodeNumber: intPtr(1),
	})
	if err == nil {
		t.Fatalf("expected error when no video files present")
	}
}

func TestRemoveFromLibrary(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "lib", "a.mkv")
	writeFile(t, target, "x")

	lib := NewLibrarian(NewOSFileSystem())
	if err := lib.RemoveFromLibrary(target); err != nil {
		t.Fatalf("RemoveFromLibrary: %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("file should be removed")
	}
	// Removing again (missing) is not an error.
	if err := lib.RemoveFromLibrary(target); err != nil {
		t.Errorf("RemoveFromLibrary on missing path should be nil, got %v", err)
	}
	// Empty path is a no-op.
	if err := lib.RemoveFromLibrary(""); err != nil {
		t.Errorf("RemoveFromLibrary(\"\") should be nil, got %v", err)
	}
}

func TestProbePath(t *testing.T) {
	t.Run("cria biblioteca e diretorio de download", func(t *testing.T) {
		completed := filepath.Join(t.TempDir(), "library")
		download := filepath.Join(t.TempDir(), "downloads")
		lib := NewLibrarian(NewOSFileSystem())

		if err := lib.ProbePath(completed, download); err != nil {
			t.Fatalf("ProbePath: %v", err)
		}

		if _, err := os.Stat(download); err != nil {
			t.Errorf("diretorio de download nao foi criado: %v", err)
		}
		if _, err := os.Stat(completed); err != nil {
			t.Errorf("diretorio da biblioteca nao foi criado: %v", err)
		}
	})

	t.Run("nao deixa arquivos de sonda para tras", func(t *testing.T) {
		completed := filepath.Join(t.TempDir(), "library")
		download := filepath.Join(t.TempDir(), "downloads")
		lib := NewLibrarian(NewOSFileSystem())

		if err := lib.ProbePath(completed, download); err != nil {
			t.Fatalf("ProbePath: %v", err)
		}

		for _, p := range []string{
			filepath.Join(completed, ".aad_move_probe"),
			filepath.Join(download, ".aad_move_probe"),
		} {
			if _, err := os.Stat(p); err == nil {
				t.Errorf("sobrou arquivo de sonda em %s", p)
			}
		}
	})

	t.Run("rejeita caminhos vazios", func(t *testing.T) {
		lib := NewLibrarian(NewOSFileSystem())
		if err := lib.ProbePath("", filepath.Join(t.TempDir(), "dl")); err == nil {
			t.Error("quero erro para biblioteca vazia, veio nil")
		}
		if err := lib.ProbePath(filepath.Join(t.TempDir(), "lib"), ""); err == nil {
			t.Error("quero erro para download vazio, veio nil")
		}
	})

	t.Run("e idempotente", func(t *testing.T) {
		completed := filepath.Join(t.TempDir(), "library")
		download := filepath.Join(t.TempDir(), "downloads")
		lib := NewLibrarian(NewOSFileSystem())

		if err := lib.ProbePath(completed, download); err != nil {
			t.Fatalf("primeira chamada: %v", err)
		}
		if err := lib.ProbePath(completed, download); err != nil {
			t.Fatalf("segunda chamada: %v", err)
		}
	})
}

func TestOrganizeBatchSameBasenameInSubfoldersKeepsBothFiles(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "batchid")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "Season 1", "[Sub] Anime - 01 [1080p].mkv"), "s1e1")
	writeFile(t, filepath.Join(dataDir, "Season 2", "[Sub] Anime - 01 [1080p].mkv"), "s2e1")

	lib := NewLibrarian(NewOSFileSystem())
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "Anime",
		CompletedPath:  completed,
		IsBatch:        true,
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created = %v, want 2 files", created)
	}
	if created[0] == created[1] {
		t.Fatalf("both files landed on the same library path: %v", created)
	}
	// Rename-always-on: the first file takes the standard name; the second (same episode
	// number after the rename) collides and keeps its raw name, disambiguated by the
	// relative path.
	for path, want := range map[string]string{
		filepath.Join(completed, "Anime", "Anime - E01.mkv"):                        "s1e1",
		filepath.Join(completed, "Anime", "[Sub] Anime - 01 [1080p].mkv"):          "s2e1",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("expected file %s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}
func TestOrganizeBatchContinuousNumberingMapsToEntryNumbers(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "batchid")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime S2 - 13 [1080p].mkv"), "a")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime S2 - 14 [1080p].mkv"), "b")

	lib := NewLibrarian(NewOSFileSystem())
	if _, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "Anime Season 2",
		CompletedPath:  completed,
		TotalEpisodes:  2,
		IsBatch:        true,
		
	}); err != nil {
		t.Fatalf("Organize: %v", err)
	}
	for _, name := range []string{"Anime Season 2 - E01.mkv", "Anime Season 2 - E02.mkv"} {
		if _, err := os.Stat(filepath.Join(completed, "Anime Season 2", name)); err != nil {
			t.Errorf("expected link %s: %v", name, err)
		}
	}
}

// A traducao so vale com evidencia inequivoca. Um extra numerado acima do total (13 num pack
// 01-12) mantem o minimo em 1: nada e deslocado.
func TestOrganizeBatchExtraAboveTotalDoesNotShift(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "batchid")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 01 [1080p].mkv"), "a")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 02 [1080p].mkv"), "b")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime - 13 [1080p].mkv"), "c")

	lib := NewLibrarian(NewOSFileSystem())
	if _, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "Anime",
		CompletedPath:  completed,
		TotalEpisodes:  2,
		IsBatch:        true,
		
	}); err != nil {
		t.Fatalf("Organize: %v", err)
	}
	for _, name := range []string{"Anime - E01.mkv", "Anime - E02.mkv", "Anime - E13.mkv"} {
		if _, err := os.Stat(filepath.Join(completed, "Anime", name)); err != nil {
			t.Errorf("expected link %s: %v", name, err)
		}
	}
}

// Pack incompleto (menos arquivos que o total da entrada) nao da para afirmar que comeca no
// episodio 1 da season: fica como esta.
func TestOrganizeBatchIncompletePackDoesNotShift(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "batchid")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime S2 - 23 [1080p].mkv"), "a")
	writeFile(t, filepath.Join(dataDir, "[Sub] Anime S2 - 24 [1080p].mkv"), "b")

	lib := NewLibrarian(NewOSFileSystem())
	if _, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir,
		AnimeName:      "Anime Season 2",
		CompletedPath:  completed,
		TotalEpisodes:  12,
		IsBatch:        true,
		
	}); err != nil {
		t.Fatalf("Organize: %v", err)
	}
	for _, name := range []string{"Anime Season 2 - E23.mkv", "Anime Season 2 - E24.mkv"} {
		if _, err := os.Stat(filepath.Join(completed, "Anime Season 2", name)); err != nil {
			t.Errorf("expected link %s: %v", name, err)
		}
	}
}

func TestPackEpisodeOffset(t *testing.T) {
	cases := []struct {
		name    string
		numbers []int
		total   int
		want    int
	}{
		{"pack completo de season 2 com numeracao continua", []int{13, 14, 15}, 3, 12},
		{"pack completo numerado a partir de 1", []int{1, 2, 3}, 3, 0},
		{"extra acima do total nao desloca", []int{1, 2, 13}, 2, 0},
		{"pack incompleto nao desloca", []int{23, 24}, 12, 0},
		{"total desconhecido nao desloca", []int{13, 14}, 0, 0},
		{"sem arquivos numerados", nil, 12, 0},
	}
	for _, c := range cases {
		if got := packEpisodeOffset(c.numbers, c.total); got != c.want {
			t.Errorf("%s: packEpisodeOffset(%v, %d) = %d, want %d", c.name, c.numbers, c.total, got, c.want)
		}
	}
}
