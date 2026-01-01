# Contributing to Personal Email Server

Thank you for your interest in contributing! This document provides guidelines and instructions for contributing.

## Code of Conduct

Please be respectful and constructive in all interactions. We welcome contributors of all backgrounds and experience levels.

## How to Contribute

### Reporting Bugs

1. **Check existing issues** to avoid duplicates
2. **Use the bug report template** when creating a new issue
3. **Include**:
   - Go version (`go version`)
   - Operating system and version
   - Steps to reproduce
   - Expected vs actual behavior
   - Relevant logs (sanitize any sensitive data)

### Suggesting Features

1. **Check existing issues and discussions** for similar suggestions
2. **Open a discussion** first for major features
3. **Describe the use case** - why is this feature needed?
4. **Consider the scope** - does it fit the project's goals?

### Submitting Code

#### Setup

```bash
# Fork and clone
git clone https://github.com/YOUR_USERNAME/email-server.git
cd email-server

# Add upstream remote
git remote add upstream https://github.com/fenilsonani/email-server.git

# Install dependencies
go mod download

# Run tests
go test ./...
```

#### Development Workflow

1. **Create a branch** from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes**:
   - Follow Go conventions and existing code style
   - Add tests for new functionality
   - Update documentation as needed

3. **Test your changes**:
   ```bash
   # Run all tests
   go test ./...

   # Run with race detector
   go test -race ./...

   # Run linter
   golangci-lint run
   ```

4. **Commit your changes**:
   - Use clear, descriptive commit messages
   - Reference related issues: `Fixes #123`

5. **Push and create a Pull Request**:
   ```bash
   git push origin feature/your-feature-name
   ```

#### Pull Request Guidelines

- **One feature/fix per PR** - keep PRs focused
- **Include tests** for new functionality
- **Update documentation** if needed
- **Describe your changes** in the PR description
- **Link related issues**

### Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Run `golangci-lint` before submitting
- Keep functions small and focused
- Add comments for complex logic
- Use meaningful variable names

### Testing

- Write unit tests for new functionality
- Aim for good coverage on critical paths
- Use table-driven tests where appropriate
- Mock external dependencies

```go
func TestExample(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid input", "test", "expected", false},
        {"empty input", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Function(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("got = %v, want %v", got, tt.want)
            }
        })
    }
}
```

## Project Structure

```
email-server/
├── cmd/mailserver/     # Main application entry point
├── internal/           # Internal packages (not importable)
│   ├── admin/          # Admin web panel
│   ├── audit/          # Audit logging
│   ├── auth/           # Authentication
│   ├── config/         # Configuration
│   ├── dav/            # CalDAV/CardDAV
│   ├── greylist/       # Greylisting
│   ├── imap/           # IMAP server
│   ├── logging/        # Structured logging
│   ├── metrics/        # Prometheus metrics
│   ├── queue/          # Redis message queue
│   ├── resilience/     # Circuit breakers
│   ├── security/       # TLS, DKIM
│   ├── smtp/           # SMTP server & delivery
│   └── storage/        # Maildir & metadata
├── configs/            # Example configurations
├── deploy/             # Deployment files
└── docs/               # Documentation
```

## Getting Help

- **Questions**: Open a [Discussion](https://github.com/fenilsonani/email-server/discussions)
- **Bugs**: Open an [Issue](https://github.com/fenilsonani/email-server/issues)
- **Security**: See [SECURITY.md](SECURITY.md)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
