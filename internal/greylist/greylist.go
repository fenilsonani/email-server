package greylist

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"time"
)

// Greylister handles greylisting logic for spam prevention
type Greylister struct {
	db       *sql.DB
	minDelay time.Duration // Minimum time before accepting (default 5 minutes)
	maxAge   time.Duration // Maximum age of greylist entries (default 35 days)
	enabled  bool
}

// Config holds greylisting configuration
type Config struct {
	Enabled  bool
	MinDelay time.Duration
	MaxAge   time.Duration
}

// DefaultConfig returns the default greylisting configuration
func DefaultConfig() Config {
	return Config{
		Enabled:  true,
		MinDelay: 5 * time.Minute,
		MaxAge:   35 * 24 * time.Hour,
	}
}

// New creates a new Greylister
func New(db *sql.DB, cfg Config) (*Greylister, error) {
	if db == nil {
		return nil, nil // Graceful degradation
	}

	// Create greylist table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS greylist (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender_ip TEXT NOT NULL,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			passed BOOLEAN DEFAULT FALSE,
			pass_count INTEGER DEFAULT 0,
			last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(sender_ip, sender, recipient)
		);
		CREATE INDEX IF NOT EXISTS idx_greylist_triplet ON greylist(sender_ip, sender, recipient);
		CREATE INDEX IF NOT EXISTS idx_greylist_first_seen ON greylist(first_seen);
	`)
	if err != nil {
		return nil, err
	}

	minDelay := cfg.MinDelay
	if minDelay == 0 {
		minDelay = 5 * time.Minute
	}

	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = 35 * 24 * time.Hour
	}

	return &Greylister{
		db:       db,
		minDelay: minDelay,
		maxAge:   maxAge,
		enabled:  cfg.Enabled,
	}, nil
}

// Check checks if the triplet (sender IP, sender email, recipient) should be allowed
// Returns (allow bool, firstTime bool, err error)
// - allow: true if the message should be accepted
// - firstTime: true if this is the first time seeing this triplet (for logging)
func (g *Greylister) Check(ctx context.Context, senderIP, sender, recipient string) (allow bool, firstTime bool, err error) {
	if g == nil || !g.enabled {
		return true, false, nil
	}

	// Normalize IP (use /24 network for IPv4 to handle dynamic IPs)
	senderIP = normalizeIP(senderIP)
	sender = strings.ToLower(sender)
	recipient = strings.ToLower(recipient)

	// Look up existing entry
	var firstSeen time.Time
	var passed bool
	err = g.db.QueryRowContext(ctx,
		`SELECT first_seen, passed FROM greylist
		 WHERE sender_ip = ? AND sender = ? AND recipient = ?`,
		senderIP, sender, recipient,
	).Scan(&firstSeen, &passed)

	if err == sql.ErrNoRows {
		// First time seeing this triplet - record it and defer
		// Use INSERT OR IGNORE to handle concurrent inserts gracefully
		result, err := g.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO greylist (sender_ip, sender, recipient) VALUES (?, ?, ?)`,
			senderIP, sender, recipient,
		)
		if err != nil {
			return false, true, err
		}
		// Check if we actually inserted (vs ignored due to race)
		rowsAffected, _ := result.RowsAffected()
		return false, rowsAffected > 0, nil
	}
	if err != nil {
		return false, false, err
	}

	// Already passed - allow immediately
	if passed {
		// Update last_seen and pass_count
		_, _ = g.db.ExecContext(ctx,
			`UPDATE greylist SET last_seen = CURRENT_TIMESTAMP, pass_count = pass_count + 1
			 WHERE sender_ip = ? AND sender = ? AND recipient = ?`,
			senderIP, sender, recipient,
		)
		return true, false, nil
	}

	// Check if minimum delay has passed
	if time.Since(firstSeen) >= g.minDelay {
		// Delay has passed - mark as passed and allow
		_, err = g.db.ExecContext(ctx,
			`UPDATE greylist SET passed = TRUE, last_seen = CURRENT_TIMESTAMP, pass_count = 1
			 WHERE sender_ip = ? AND sender = ? AND recipient = ?`,
			senderIP, sender, recipient,
		)
		if err != nil {
			return false, false, err
		}
		return true, false, nil
	}

	// Still within delay period - defer
	return false, false, nil
}

// Cleanup removes old greylist entries
func (g *Greylister) Cleanup(ctx context.Context) error {
	if g == nil || !g.enabled {
		return nil
	}

	// Remove entries that haven't passed and are older than maxAge
	// Also remove passed entries that haven't been seen in maxAge
	_, err := g.db.ExecContext(ctx,
		`DELETE FROM greylist WHERE
		 (passed = FALSE AND first_seen < ?) OR
		 (passed = TRUE AND last_seen < ?)`,
		time.Now().Add(-g.maxAge), time.Now().Add(-g.maxAge),
	)
	return err
}

// Stats returns greylist statistics
func (g *Greylister) Stats(ctx context.Context) (total, passed, pending int, err error) {
	if g == nil || !g.enabled {
		return 0, 0, 0, nil
	}

	err = g.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		 SUM(CASE WHEN passed THEN 1 ELSE 0 END),
		 SUM(CASE WHEN NOT passed THEN 1 ELSE 0 END)
		 FROM greylist`,
	).Scan(&total, &passed, &pending)
	return
}

// IsEnabled returns whether greylisting is enabled
func (g *Greylister) IsEnabled() bool {
	return g != nil && g.enabled
}

// normalizeIP normalizes an IP address for greylisting
// For IPv4, uses /24 network; for IPv6, uses /48 network
func normalizeIP(ip string) string {
	// Handle ip:port format
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip // Return as-is if can't parse
	}

	if v4 := parsed.To4(); v4 != nil {
		// IPv4: use /24 network
		return v4.Mask(net.CIDRMask(24, 32)).String()
	}

	// IPv6: use /48 network
	return parsed.Mask(net.CIDRMask(48, 128)).String()
}

// StartCleanupRoutine starts a background goroutine to periodically clean up old entries
func (g *Greylister) StartCleanupRoutine(ctx context.Context) {
	if g == nil || !g.enabled {
		return
	}

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := g.Cleanup(ctx); err != nil {
					// Log error but continue
				}
			}
		}
	}()
}
