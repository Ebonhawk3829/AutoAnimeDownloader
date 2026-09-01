//go:build !windows

package files

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestIsCrossDeviceUnix(t *testing.T) {
	if !isCrossDevice(syscall.EXDEV) {
		t.Errorf("isCrossDevice(EXDEV) = false, want true")
	}
	if isCrossDevice(errors.New("some other error")) {
		t.Errorf("isCrossDevice(other) = true, want false")
	}
	if isCrossDevice(syscall.ENOENT) {
		t.Errorf("isCrossDevice(ENOENT) = true, want false")
	}
}

// exdevMove is a move func that always reports a cross-device error.
func exdevMove(oldname, newname string) error {
	return &os.LinkError{Op: "rename", Old: oldname, New: newname, Err: syscall.EXDEV}
}

// EXDEV agora tem fallback copia+apaga (bind mounts da mesma particao recusam rename com
// EXDEV mesmo com st_dev igual), entao o Organize segue e o arquivo chega ao destino.
func TestOrganizeCrossDeviceFallsBackToCopy(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "id")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "ep.mkv"), "x")

	lib := &organizer{fs: NewOSFileSystem(), move: exdevMove}
	created, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "A", CompletedPath: completed,
		EpisodeNumber: intPtr(1),
	})
	if err != nil {
		t.Fatalf("expected the copy fallback to succeed, got %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created = %v, want 1", created)
	}
	data, err := os.ReadFile(created[0])
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "x" {
		t.Errorf("dest content = %q, want %q", data, "x")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "ep.mkv")); !os.IsNotExist(err) {
		t.Error("source should have been deleted after the copy")
	}
}

// ProbePath com EXDEV cai no fallback de escrita: a biblioteca aceita escrever, entao o
// probe passa (o Organize resolve o move com copia+apaga).
func TestProbePathCrossDeviceFallsBackToWrite(t *testing.T) {
	tmp := t.TempDir()
	completed := filepath.Join(tmp, "completed")
	download := filepath.Join(tmp, "downloads")

	lib := &organizer{fs: NewOSFileSystem(), move: exdevMove}
	if err := lib.ProbePath(completed, download); err != nil {
		t.Fatalf("expected the write fallback to pass, got %v", err)
	}
	for _, p := range []string{
		filepath.Join(download, ".aad_move_probe"),
		filepath.Join(completed, ".aad_move_probe"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("probe file leaked at %s", p)
		}
	}
}
