package security

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertificateWatcherHashFileUsesStableSHA256(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certPath, []byte("cert-a"), 0600); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("key-a"), 0600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}

	watcher, err := NewCertificateWatcher(certPath, keyPath, time.Second, nil)
	if err != nil {
		t.Fatalf("NewCertificateWatcher() error = %v", err)
	}
	defer watcher.watcher.Close()

	hashA, err := watcher.hashFile(certPath)
	if err != nil {
		t.Fatalf("hashFile() error = %v", err)
	}
	if len(hashA) != 64 {
		t.Fatalf("hash length = %d, want 64", len(hashA))
	}

	if err := os.WriteFile(certPath, []byte("cert-b"), 0600); err != nil {
		t.Fatalf("WriteFile(cert updated) error = %v", err)
	}

	hashB, err := watcher.hashFile(certPath)
	if err != nil {
		t.Fatalf("hashFile() error = %v", err)
	}
	if hashA == hashB {
		t.Fatal("hash did not change after file update")
	}
}
