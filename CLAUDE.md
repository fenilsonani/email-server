# Claude Code Rules

## Git (MANDATORY)
- **NO direct push to main** - PRs required with 1 approval
- Branch: `git checkout -b feat|fix|docs/description`
- Commit: `git commit -m "type(scope): msg"` + Co-Authored-By
- PR: `gh pr create`, merge after approval

## Before Push
```bash
go build ./... && go vet ./... && go test ./...
```

## Project
- CLI: `cmd/mailserver/`
- Packages: `internal/`
- Config: `config.yaml`

## Server
- Host: `mail.fenilsonani.com`
- Config: `/etc/mailserver/config.yaml`
- Deploy: `git pull && go build -o /usr/local/bin/mailserver ./cmd/mailserver/ && systemctl restart mailserver`

## Doctor CLI
```bash
mailserver doctor              # health check
mailserver doctor fix --yes    # auto-heal
```
