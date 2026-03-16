package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile(t *testing.T) {
	// Create temp directories
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(srcDir, "test.txt")
	content := []byte("Hello, World!")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy file
	dstPath := filepath.Join(dstDir, "test.txt")
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	// Verify content
	copied, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read copied file: %v", err)
	}

	if string(copied) != string(content) {
		t.Errorf("Content mismatch: got %q, want %q", string(copied), string(content))
	}
}

func TestCopyFile_CreatesParentDirs(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(srcPath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy to nested path
	dstPath := filepath.Join(dstDir, "a", "b", "c", "test.txt")
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	if _, err := os.Stat(dstPath); os.IsNotExist(err) {
		t.Error("Destination file was not created")
	}
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create directory structure
	dirs := []string{
		"subdir1",
		"subdir2",
		"subdir1/nested",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(srcDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
	}

	// Create files
	files := map[string]string{
		"file1.txt":              "content1",
		"subdir1/file2.txt":      "content2",
		"subdir1/nested/file3.txt": "content3",
		"subdir2/file4.txt":      "content4",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(srcDir, path), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	// Copy directory
	dstPath := filepath.Join(dstDir, "copied")
	if err := copyDir(srcDir, dstPath); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	// Verify all files exist with correct content
	for path, expectedContent := range files {
		fullPath := filepath.Join(dstPath, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read %s: %v", path, err)
			continue
		}
		if string(content) != expectedContent {
			t.Errorf("Content mismatch for %s: got %q, want %q", path, string(content), expectedContent)
		}
	}
}

func TestCreateTarGz(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"file1.txt":         "content1",
		"subdir/file2.txt":  "content2",
	}
	for path, content := range files {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	// Create tar.gz
	archivePath := filepath.Join(outDir, "test.tar.gz")
	if err := createTarGz(archivePath, srcDir); err != nil {
		t.Fatalf("createTarGz failed: %v", err)
	}

	// Verify archive exists
	fi, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("Archive not created: %v", err)
	}
	if fi.Size() == 0 {
		t.Error("Archive is empty")
	}

	// Verify archive contents
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("Failed to open archive: %v", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	foundFiles := make(map[string]bool)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read tar header: %v", err)
		}
		foundFiles[header.Name] = true
	}

	for path := range files {
		if !foundFiles[path] {
			t.Errorf("File %s not found in archive", path)
		}
	}
}

func TestExtractTarGz(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	extractDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"file1.txt":         "content1",
		"subdir/file2.txt":  "content2",
		"subdir/file3.txt":  "content3",
	}
	for path, content := range files {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	// Create archive
	archivePath := filepath.Join(archiveDir, "test.tar.gz")
	if err := createTarGz(archivePath, srcDir); err != nil {
		t.Fatalf("createTarGz failed: %v", err)
	}

	// Extract archive
	if err := extractTarGz(archivePath, extractDir); err != nil {
		t.Fatalf("extractTarGz failed: %v", err)
	}

	// Verify extracted files
	for path, expectedContent := range files {
		fullPath := filepath.Join(extractDir, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read extracted file %s: %v", path, err)
			continue
		}
		if string(content) != expectedContent {
			t.Errorf("Content mismatch for %s: got %q, want %q", path, string(content), expectedContent)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, test := range tests {
		result := formatBytes(test.input)
		if result != test.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", test.input, result, test.expected)
		}
	}
}

func TestBackupRestore_RoundTrip(t *testing.T) {
	// Create source directory structure (simulating mail server data)
	srcDir := t.TempDir()
	backupDir := t.TempDir()
	restoreDir := t.TempDir()

	// Create mock data structure
	dataStructure := map[string]string{
		"metadata.db":                    "SQLite database content",
		"maildir/user@example.com/new/1": "Email 1 content",
		"maildir/user@example.com/new/2": "Email 2 content",
		"maildir/user@example.com/cur/3": "Email 3 content",
		"dkim/example.com.key":           "-----BEGIN RSA PRIVATE KEY-----\nMockKey\n-----END RSA PRIVATE KEY-----",
		"dkim/example.com.pub":           "-----BEGIN PUBLIC KEY-----\nMockPubKey\n-----END PUBLIC KEY-----",
	}

	for path, content := range dataStructure {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	// Create backup
	backupPath := filepath.Join(backupDir, "backup.tar.gz")
	if err := createTarGz(backupPath, srcDir); err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Verify backup file exists and has content
	fi, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("Backup file not found: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("Backup file is empty")
	}
	t.Logf("Backup size: %s", formatBytes(fi.Size()))

	// Restore backup
	if err := extractTarGz(backupPath, restoreDir); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify all files restored correctly
	for path, expectedContent := range dataStructure {
		fullPath := filepath.Join(restoreDir, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read restored file %s: %v", path, err)
			continue
		}
		if string(content) != expectedContent {
			t.Errorf("Content mismatch for %s", path)
		}
	}

	t.Log("Backup/Restore round trip successful")
}

func TestCopyDir_EmptyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create empty subdirectory
	emptySubdir := filepath.Join(srcDir, "empty")
	if err := os.MkdirAll(emptySubdir, 0755); err != nil {
		t.Fatalf("Failed to create empty directory: %v", err)
	}

	// Copy directory
	dstPath := filepath.Join(dstDir, "copied")
	if err := copyDir(srcDir, dstPath); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	// Verify empty directory exists
	copiedEmpty := filepath.Join(dstPath, "empty")
	fi, err := os.Stat(copiedEmpty)
	if err != nil {
		t.Fatalf("Empty directory not copied: %v", err)
	}
	if !fi.IsDir() {
		t.Error("Empty directory not a directory after copy")
	}
}

func TestCopyFile_NonExistent(t *testing.T) {
	dstDir := t.TempDir()

	err := copyFile("/nonexistent/file.txt", filepath.Join(dstDir, "out.txt"))
	if err == nil {
		t.Error("Expected error for non-existent source file")
	}
}

func TestExtractTarGz_InvalidArchive(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid archive
	invalidPath := filepath.Join(tmpDir, "invalid.tar.gz")
	if err := os.WriteFile(invalidPath, []byte("not a valid archive"), 0644); err != nil {
		t.Fatalf("Failed to create invalid archive: %v", err)
	}

	extractDir := filepath.Join(tmpDir, "extract")
	err := extractTarGz(invalidPath, extractDir)
	if err == nil {
		t.Error("Expected error for invalid archive")
	}
}

func TestExtractTarGz_RejectsTraversalEntries(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "traversal.tar.gz")
	extractDir := t.TempDir()
	outsidePath := filepath.Join(extractDir, "..", "outside.txt")

	if err := writeTestTarGz(archivePath, []tar.Header{
		{
			Name:     "../outside.txt",
			Mode:     0644,
			Size:     int64(len("escape")),
			Typeflag: tar.TypeReg,
		},
	}, []string{"escape"}); err != nil {
		t.Fatalf("failed to create traversal archive: %v", err)
	}

	err := extractTarGz(archivePath, extractDir)
	if err == nil {
		t.Fatal("expected traversal archive to be rejected")
	}

	if _, statErr := os.Stat(outsidePath); !os.IsNotExist(statErr) {
		t.Fatalf("outside path should not be created, stat err=%v", statErr)
	}
}

func TestExtractTarGz_RejectsAbsoluteEntries(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "absolute.tar.gz")
	extractDir := t.TempDir()

	if err := writeTestTarGz(archivePath, []tar.Header{
		{
			Name:     "/tmp/absolute.txt",
			Mode:     0644,
			Size:     int64(len("escape")),
			Typeflag: tar.TypeReg,
		},
	}, []string{"escape"}); err != nil {
		t.Fatalf("failed to create absolute-path archive: %v", err)
	}

	err := extractTarGz(archivePath, extractDir)
	if err == nil {
		t.Fatal("expected absolute-path archive to be rejected")
	}
}

func writeTestTarGz(archivePath string, headers []tar.Header, bodies []string) error {
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
