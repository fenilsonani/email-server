package admin

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractBackup_RejectsTraversalEntries(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "traversal.tar.gz")
	destDir := t.TempDir()
	outsidePath := filepath.Join(destDir, "..", "outside.txt")

	if err := writeAdminTestTarGz(archivePath, []tar.Header{
		{
			Name:     "../outside.txt",
			Mode:     0644,
			Size:     int64(len("escape")),
			Typeflag: tar.TypeReg,
		},
	}, []string{"escape"}); err != nil {
		t.Fatalf("failed to create traversal archive: %v", err)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("failed to open archive: %v", err)
	}
	defer file.Close()

	if err := extractBackup(file, destDir); err == nil {
		t.Fatal("expected traversal archive to be rejected")
	}

	if _, statErr := os.Stat(outsidePath); !os.IsNotExist(statErr) {
		t.Fatalf("outside path should not be created, stat err=%v", statErr)
	}
}

func TestExtractBackup_RejectsAbsoluteEntries(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "absolute.tar.gz")
	destDir := t.TempDir()

	if err := writeAdminTestTarGz(archivePath, []tar.Header{
		{
			Name:     "/tmp/absolute.txt",
			Mode:     0644,
			Size:     int64(len("escape")),
			Typeflag: tar.TypeReg,
		},
	}, []string{"escape"}); err != nil {
		t.Fatalf("failed to create absolute-path archive: %v", err)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("failed to open archive: %v", err)
	}
	defer file.Close()

	if err := extractBackup(file, destDir); err == nil {
		t.Fatal("expected absolute-path archive to be rejected")
	}
}

func TestExtractBackup_StripsSpecialPermissionBits(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "mode.tar.gz")
	destDir := t.TempDir()

	if err := writeAdminTestTarGz(archivePath, []tar.Header{
		{
			Name:     "maildir/message.txt",
			Mode:     0o4755,
			Size:     int64(len("hello")),
			Typeflag: tar.TypeReg,
		},
	}, []string{"hello"}); err != nil {
		t.Fatalf("failed to create mode archive: %v", err)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("failed to open archive: %v", err)
	}
	defer file.Close()

	if err := extractBackup(file, destDir); err != nil {
		t.Fatalf("extractBackup() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(destDir, "maildir", "message.txt"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("permissions = %o, want %o", info.Mode().Perm(), 0o755)
	}
	if info.Mode()&os.ModeSetuid != 0 {
		t.Fatalf("setuid bit should be stripped, mode=%v", info.Mode())
	}
}

func writeAdminTestTarGz(archivePath string, headers []tar.Header, bodies []string) error {
	outFile, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	for i, header := range headers {
		hdr := header
		if err := tarWriter.WriteHeader(&hdr); err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(bodies[i])); err != nil {
				return err
			}
		}
	}

	return nil
}
