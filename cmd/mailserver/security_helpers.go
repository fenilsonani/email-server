package main

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/fenilsonani/email-server/internal/safecast"
)

var validRemoteServer = regexp.MustCompile(`^[A-Za-z0-9._@\[\]:-]+$`)

func validateRemoteServer(server string) error {
	server = strings.TrimSpace(server)
	if server == "" {
		return fmt.Errorf("remote server is required")
	}
	if strings.HasPrefix(server, "-") {
		return fmt.Errorf("remote server cannot start with '-'")
	}
	if !validRemoteServer.MatchString(server) {
		return fmt.Errorf("remote server contains invalid characters")
	}
	return nil
}

func validateRemotePath(remotePath string) (string, error) {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return "", fmt.Errorf("remote path is required")
	}
	cleanPath := path.Clean(remotePath)
	if !strings.HasPrefix(cleanPath, "/") {
		return "", fmt.Errorf("remote path must be absolute")
	}
	return cleanPath, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func safeTarFileMode(mode int64) (os.FileMode, error) {
	return safecast.Int64ToFileMode(mode)
}

func generateProcessUIDValidity() uint32 {
	pid, err := safecast.IntToUint32(os.Getpid())
	if err != nil {
		return 0x12345678
	}
	return pid ^ uint32(0x12345678)
}
