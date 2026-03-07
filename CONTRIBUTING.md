# Contributing

## Reporting Bugs

Check existing issues first, then open one with:
- Go version, OS
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs (sanitize sensitive data)

## Development Setup

```bash
git clone https://github.com/YOUR_USERNAME/email-server.git
cd email-server
git remote add upstream https://github.com/fenilsonani/email-server.git
go mod download
go test ./...
```

## Workflow

1. Branch from `main`:
   ```bash
   git checkout -b feat/your-feature
   ```

2. Make changes, add tests, update docs as needed.

3. Test:
   ```bash
   go test ./...
   go test -race ./...
   go vet ./...
   ```

4. Commit using [Conventional Commits](https://www.conventionalcommits.org/):
   ```
   feat(smtp): add STARTTLS support
   fix(imap): resolve connection timeout
   ```
   Types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `chore`

5. Push and open a PR. One feature/fix per PR.

## Project Structure

```
cmd/mailserver/     Entry point
internal/
  admin/            Web admin panel
  audit/            Audit logging
  auth/             Authentication
  config/           Configuration
  dav/              CalDAV/CardDAV
  greylist/         Greylisting
  imap/             IMAP server
  metrics/          Prometheus metrics
  queue/            Redis message queue
  resilience/       Circuit breakers
  security/         TLS, DKIM
  smtp/             SMTP server & delivery
  storage/          Maildir & metadata
configs/            Example configs
deploy/             systemd, Docker, nginx
```

## Help

- Questions: [Discussions](https://github.com/fenilsonani/email-server/discussions)
- Bugs: [Issues](https://github.com/fenilsonani/email-server/issues)
- Security: [SECURITY.md](SECURITY.md)

By contributing, you agree that your contributions will be licensed under the MIT License.
