package setup

import (
	"net"
	"strconv"
	"testing"
)

func TestCheckPortAvailableUsesLoopbackBinding(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	result := checkPortAvailable(port, "test")
	if result.Status != "warn" {
		t.Fatalf("checkPortAvailable() status = %q, want %q", result.Status, "warn")
	}
}

func TestCheckPortAvailablePassesWhenPortIsFree(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	result := checkPortAvailable(port, "test")
	if result.Status != "pass" {
		t.Fatalf("checkPortAvailable(%s) status = %q, want %q", strconv.Itoa(port), result.Status, "pass")
	}
}
