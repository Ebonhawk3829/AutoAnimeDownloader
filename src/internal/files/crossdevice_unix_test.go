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

func TestOrganizeCrossDeviceCleansUp(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "save", "id")
	completed := filepath.Join(tmp, "completed")
	writeFile(t, filepath.Join(dataDir, "ep.mkv"), "x")

	lib := &organizer{fs: NewOSFileSystem(), move: exdevMove}
	_, err := lib.Organize(OrganizeRequest{
		TorrentDataDir: dataDir, AnimeName: "A", CompletedPath: completed,
		EpisodeNumber: intPtr(1),
	})
	if err == nil {
		t.Fatalf("expected cross-device error")
	}
	if !isCrossDevice(errors.Unwrap(err)) {
		t.Errorf("error should wrap a cross-device error: %v", err)
	}
	// No orphan library folder left behind.
	if _, statErr := os.Stat(filepath.Join(completed, "A")); statErr == nil {
		t.Errorf("orphan library folder should have been cleaned up")
	}
}

func TestProbePathFailsWhenMoveUnsupported(t *testing.T) {
	tmp := t.TempDir()
	completed := filepath.Join(tmp, "completed")
	download := filepath.Join(tmp, "downloads")

	lib := &organizer{fs: NewOSFileSystem(), move: exdevMove}
	if err := lib.ProbePath(completed, download); err == nil {
		t.Fatalf("expected error from ProbePath when the move func fails")
	}
	// Probe source cleaned up despite the failure.
	if _, statErr := os.Stat(filepath.Join(download, ".aad_move_probe")); statErr == nil {
		t.Errorf("probe source not cleaned up after failure")
	}
}
