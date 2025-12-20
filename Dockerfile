# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o mailserver ./cmd/mailserver

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 mailserver && \
    adduser -u 1000 -G mailserver -s /bin/sh -D mailserver

# Create directories
RUN mkdir -p /var/lib/mailserver/maildir \
             /var/lib/mailserver/acme \
             /etc/mailserver && \
    chown -R mailserver:mailserver /var/lib/mailserver /etc/mailserver

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/mailserver /app/mailserver

# Copy example config
COPY configs/config.example.yaml /etc/mailserver/config.example.yaml

# Switch to non-root user
USER mailserver

# Expose ports
# SMTP
EXPOSE 25
# Submission
EXPOSE 587
# SMTPS
EXPOSE 465
# IMAP
EXPOSE 143
# IMAPS
EXPOSE 993
# CalDAV/CardDAV
EXPOSE 8443

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD nc -z localhost 993 || exit 1

# Default command
ENTRYPOINT ["/app/mailserver"]
CMD ["serve", "--config", "/etc/mailserver/config.yaml"]
