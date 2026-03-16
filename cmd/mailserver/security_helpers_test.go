package main

import "testing"

func TestValidateRemoteServer(t *testing.T) {
	tests := []struct {
		name    string
		server  string
		wantErr bool
	}{
		{name: "hostname", server: "mail.example.com"},
		{name: "user at host", server: "deploy@mail.example.com"},
		{name: "reject option injection", server: "-oProxyCommand=sh", wantErr: true},
		{name: "reject spaces", server: "mail example", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRemoteServer(tt.server)
			if tt.wantErr && err == nil {
				t.Fatal("validateRemoteServer() succeeded unexpectedly")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateRemoteServer() error = %v", err)
			}
		})
	}
}

func TestValidateRemotePath(t *testing.T) {
	got, err := validateRemotePath("/srv/mail/../mail/backups")
	if err != nil {
		t.Fatalf("validateRemotePath() error = %v", err)
	}
	if got != "/srv/mail/backups" {
		t.Fatalf("validateRemotePath() = %q, want %q", got, "/srv/mail/backups")
	}

	if _, err := validateRemotePath("relative/path"); err == nil {
		t.Fatal("validateRemotePath() succeeded for relative path")
	}
}
