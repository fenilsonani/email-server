package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	imapserver "github.com/fenilsonani/email-server/internal/imap"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/security"
	smtpserver "github.com/fenilsonani/email-server/internal/smtp"
	"github.com/fenilsonani/email-server/internal/smtp/delivery"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
	"github.com/fenilsonani/email-server/internal/storage/metadata"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
	db      *metadata.DB
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "mailserver",
	Short: "Personal email server with IMAP, SMTP, CalDAV, and CardDAV",
	Long: `A personal email server supporting:
- IMAP with IDLE for Apple Mail sync
- SMTP for sending and receiving email
- CalDAV for calendar sync
- CardDAV for contacts sync
- Multiple domains with DKIM signing`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for help commands
		if cmd.Name() == "help" || cmd.Name() == "version" {
			return nil
		}

		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		return nil
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the mail server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}

		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		// Open database
		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		// Run migrations
		if err := db.Migrate(context.Background()); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		// Initialize TLS
		tlsManager, err := security.NewTLSManager(cfg)
		if err != nil {
			return fmt.Errorf("failed to initialize TLS: %w", err)
		}

		// Initialize authenticator
		authenticator := auth.NewAuthenticator(db.DB)

		// Initialize maildir store
		store, err := maildir.NewStore(db.DB, cfg.Storage.MaildirPath)
		if err != nil {
			return fmt.Errorf("failed to initialize maildir store: %w", err)
		}

		// Initialize structured logger
		logger, err := logging.New(logging.Config{
			Level:  cfg.Logging.Level,
			Format: cfg.Logging.Format,
			Output: cfg.Logging.Output,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}

		// Initialize Redis queue for message delivery
		retryMaxAge, _ := time.ParseDuration(cfg.Queue.RetryMaxAge)
		if retryMaxAge == 0 {
			retryMaxAge = 7 * 24 * time.Hour // Default 7 days
		}
		redisQueue, err := queue.NewRedisQueue(queue.Config{
			RedisURL:    cfg.Queue.RedisURL,
			Prefix:      cfg.Queue.Prefix,
			MaxRetries:  cfg.Queue.MaxRetries,
			RetryMaxAge: retryMaxAge,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize Redis queue: %w", err)
		}
		defer redisQueue.Close()

		// Initialize DKIM signer pool
		dkimPool := security.NewDKIMSignerPool()
		for _, domain := range cfg.Domains {
			if domain.DKIMKeyFile != "" {
				if err := dkimPool.AddSigner(domain.Name, domain.DKIMSelector, domain.DKIMKeyFile); err != nil {
					logger.Warn("Failed to load DKIM key for domain",
						"domain", domain.Name,
						"error", err.Error())
				} else {
					logger.Info("Loaded DKIM key", "domain", domain.Name, "selector", domain.DKIMSelector)
				}
			}
		}

		// Initialize delivery engine
		connectTimeout, _ := time.ParseDuration(cfg.Delivery.ConnectTimeout)
		if connectTimeout == 0 {
			connectTimeout = 30 * time.Second
		}
		commandTimeout, _ := time.ParseDuration(cfg.Delivery.CommandTimeout)
		if commandTimeout == 0 {
			commandTimeout = 5 * time.Minute
		}
		deliveryEngine := delivery.NewEngine(delivery.Config{
			Workers:        cfg.Delivery.Workers,
			Hostname:       cfg.Server.Hostname,
			ConnectTimeout: connectTimeout,
			CommandTimeout: commandTimeout,
			MaxMessageSize: int64(cfg.Security.MaxMessageSize),
			RequireTLS:     cfg.Delivery.RequireTLS,
			VerifyTLS:      cfg.Delivery.VerifyTLS,
			RelayHost:      cfg.Delivery.RelayHost,
		}, redisQueue, dkimPool, logger)
		deliveryEngine.Start()
		defer deliveryEngine.Stop()

		// Create IMAP backend and server
		imapBackend := imapserver.NewBackend(authenticator, store)
		imapAddr := fmt.Sprintf(":%d", cfg.Server.IMAPPort)
		imapsAddr := fmt.Sprintf(":%d", cfg.Server.IMAPSPort)
		imapSrv := imapserver.NewServer(imapBackend, imapAddr, imapsAddr, tlsManager.TLSConfig())

		// Create SMTP backend and server with delivery engine
		smtpBackend := smtpserver.NewBackend(cfg, authenticator, store, deliveryEngine, logger)
		smtpSrv := smtpserver.NewServer(smtpBackend, cfg, tlsManager.TLSConfig())

		fmt.Printf("Mail server starting on %s\n", cfg.Server.Hostname)
		fmt.Printf("  SMTP:  %d (MX), %d (submission), %d (SMTPS)\n",
			cfg.Server.SMTPPort, cfg.Server.SubmissionPort, cfg.Server.SMTPSPort)
		fmt.Printf("  IMAP:  %d, %d (TLS)\n", cfg.Server.IMAPPort, cfg.Server.IMAPSPort)
		fmt.Printf("  DAV:   %d\n", cfg.Server.DAVPort)

		// Start IMAP servers
		if err := imapSrv.ListenAndServe(); err != nil {
			return fmt.Errorf("failed to start IMAP server: %w", err)
		}
		if tlsManager.HasTLS() {
			if err := imapSrv.ListenAndServeTLS(tlsManager.TLSConfig()); err != nil {
				return fmt.Errorf("failed to start IMAPS server: %w", err)
			}
		}

		// Start SMTP servers
		if err := smtpSrv.ListenAndServe(); err != nil {
			return fmt.Errorf("failed to start SMTP server: %w", err)
		}
		if err := smtpSrv.ListenAndServeSubmission(); err != nil {
			return fmt.Errorf("failed to start SMTP submission server: %w", err)
		}
		if tlsManager.HasTLS() {
			if err := smtpSrv.ListenAndServeTLS(); err != nil {
				return fmt.Errorf("failed to start SMTPS server: %w", err)
			}
		}

		fmt.Println("\nServer is running. Press Ctrl+C to stop.")

		// Wait for shutdown signal
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		fmt.Println("\nShutting down...")

		// Graceful shutdown
		imapSrv.Close()
		smtpSrv.Close()

		return nil
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		if err := db.Migrate(context.Background()); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		fmt.Println("Migrations completed successfully")
		return nil
	},
}

// Domain management commands
var domainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Manage email domains",
}

var domainAddCmd = &cobra.Command{
	Use:   "add <domain>",
	Short: "Add a new domain",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		domainName := args[0]

		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		if err := db.Migrate(context.Background()); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		// Insert domain
		result, err := db.ExecContext(context.Background(),
			"INSERT INTO domains (name, dkim_selector) VALUES (?, ?)",
			domainName, "mail",
		)
		if err != nil {
			return fmt.Errorf("failed to add domain: %w", err)
		}

		id, _ := result.LastInsertId()
		fmt.Printf("Domain '%s' added with ID %d\n", domainName, id)
		return nil
	},
}

var domainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all domains",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		rows, err := db.QueryContext(context.Background(),
			"SELECT id, name, dkim_selector, is_active, created_at FROM domains ORDER BY name",
		)
		if err != nil {
			return fmt.Errorf("failed to query domains: %w", err)
		}
		defer rows.Close()

		fmt.Printf("%-5s %-30s %-10s %-8s %s\n", "ID", "DOMAIN", "DKIM", "ACTIVE", "CREATED")
		fmt.Println("-------------------------------------------------------------------")

		for rows.Next() {
			var id int64
			var name, selector string
			var active bool
			var created string
			if err := rows.Scan(&id, &name, &selector, &active, &created); err != nil {
				return err
			}
			status := "yes"
			if !active {
				status = "no"
			}
			fmt.Printf("%-5d %-30s %-10s %-8s %s\n", id, name, selector, status, created)
		}
		return rows.Err()
	},
}

// User management commands
var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage email users",
}

var userAddCmd = &cobra.Command{
	Use:   "add <email> <password>",
	Short: "Add a new user",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]
		password := args[1]

		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		if err := db.Migrate(context.Background()); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		// Parse email
		authenticator := auth.NewAuthenticator(db.DB)
		parts := splitEmail(email)
		if len(parts) != 2 {
			return fmt.Errorf("invalid email format: %s", email)
		}
		username, domain := parts[0], parts[1]

		// Get domain ID
		domainID, err := authenticator.GetDomainID(context.Background(), domain)
		if err != nil {
			return fmt.Errorf("domain '%s' not found. Add it first with: mailserver domain add %s", domain, domain)
		}

		// Hash password
		hash, err := auth.HashPassword(password)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		// Insert user
		result, err := db.ExecContext(context.Background(),
			"INSERT INTO users (domain_id, username, password_hash) VALUES (?, ?, ?)",
			domainID, username, hash,
		)
		if err != nil {
			return fmt.Errorf("failed to add user: %w", err)
		}

		userID, _ := result.LastInsertId()

		// Create default mailboxes
		defaultMailboxes := []struct {
			name       string
			specialUse string
		}{
			{"INBOX", ""},
			{"Drafts", `\Drafts`},
			{"Sent", `\Sent`},
			{"Trash", `\Trash`},
			{"Junk", `\Junk`},
			{"Archive", `\Archive`},
		}

		for _, mb := range defaultMailboxes {
			_, err = db.ExecContext(context.Background(),
				"INSERT INTO mailboxes (user_id, name, uidvalidity, uidnext, special_use) VALUES (?, ?, ?, 1, ?)",
				userID, mb.name, generateUIDValidity(), mb.specialUse,
			)
			if err != nil {
				fmt.Printf("Warning: failed to create mailbox %s: %v\n", mb.name, err)
			}
		}

		fmt.Printf("User '%s' added with ID %d\n", email, userID)
		fmt.Println("Default mailboxes created: INBOX, Drafts, Sent, Trash, Junk, Archive")
		return nil
	},
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		rows, err := db.QueryContext(context.Background(), `
			SELECT u.id, u.username, d.name, u.display_name, u.is_active, u.created_at
			FROM users u
			JOIN domains d ON u.domain_id = d.id
			ORDER BY d.name, u.username
		`)
		if err != nil {
			return fmt.Errorf("failed to query users: %w", err)
		}
		defer rows.Close()

		fmt.Printf("%-5s %-40s %-20s %-8s %s\n", "ID", "EMAIL", "NAME", "ACTIVE", "CREATED")
		fmt.Println("---------------------------------------------------------------------------------")

		for rows.Next() {
			var id int64
			var username, domain string
			var displayName *string
			var active bool
			var created string
			if err := rows.Scan(&id, &username, &domain, &displayName, &active, &created); err != nil {
				return err
			}
			email := fmt.Sprintf("%s@%s", username, domain)
			name := ""
			if displayName != nil {
				name = *displayName
			}
			status := "yes"
			if !active {
				status = "no"
			}
			fmt.Printf("%-5d %-40s %-20s %-8s %s\n", id, email, name, status, created)
		}
		return rows.Err()
	},
}

var userPasswdCmd = &cobra.Command{
	Use:   "passwd <email> <new-password>",
	Short: "Change user password",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]
		newPassword := args[1]

		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		parts := splitEmail(email)
		if len(parts) != 2 {
			return fmt.Errorf("invalid email format: %s", email)
		}
		username, domain := parts[0], parts[1]

		// Hash new password
		hash, err := auth.HashPassword(newPassword)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		// Update password
		result, err := db.ExecContext(context.Background(), `
			UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
			WHERE username = ? AND domain_id = (SELECT id FROM domains WHERE name = ?)
		`, hash, username, domain)
		if err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("user not found: %s", email)
		}

		fmt.Printf("Password updated for '%s'\n", email)
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("mailserver v0.1.0")
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "config.yaml", "config file path")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(versionCmd)

	// Domain commands
	domainCmd.AddCommand(domainAddCmd)
	domainCmd.AddCommand(domainListCmd)
	rootCmd.AddCommand(domainCmd)

	// User commands
	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userPasswdCmd)
	rootCmd.AddCommand(userCmd)
}

func splitEmail(email string) []string {
	for i := len(email) - 1; i >= 0; i-- {
		if email[i] == '@' {
			return []string{email[:i], email[i+1:]}
		}
	}
	return nil
}

func generateUIDValidity() uint32 {
	// Use current unix timestamp as UID validity
	return uint32(os.Getpid()) ^ uint32(0x12345678)
}
