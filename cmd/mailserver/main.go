package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fenilsonani/email-server/internal/admin"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/autodiscover"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/dns"
	imapserver "github.com/fenilsonani/email-server/internal/imap"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/security"
	"github.com/fenilsonani/email-server/internal/sieve"
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
		// Validate configuration before doing anything
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}

		// Ensure directories exist with proper permissions
		if err := cfg.EnsureDirectories(); err != nil {
			return fmt.Errorf("failed to create required directories: %w", err)
		}

		// Track resources for cleanup
		type resourceTracker struct {
			db             *metadata.DB
			redisQueue     *queue.RedisQueue
			deliveryEngine *delivery.Engine
			imapSrv        *imapserver.Server
			smtpSrv        *smtpserver.Server
			adminSrv       *admin.Server
			logger         *logging.Logger
		}
		resources := &resourceTracker{}

		// Cleanup function - called on both success and error paths
		cleanup := func() {
			if resources.logger != nil {
				resources.logger.Info("Starting graceful shutdown")
			}

			// Parse shutdown timeout from config
			shutdownTimeout := 30 * time.Second
			if cfg.Server.ShutdownTimeout != "" {
				if t, err := time.ParseDuration(cfg.Server.ShutdownTimeout); err == nil {
					shutdownTimeout = t
				}
			}

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()

			// Shutdown in reverse order of initialization
			// 1. Stop accepting new connections first
			if resources.adminSrv != nil {
				if resources.logger != nil {
					resources.logger.Info("Shutting down admin server")
				}
				if err := resources.adminSrv.Shutdown(shutdownCtx); err != nil {
					if resources.logger != nil {
						resources.logger.Error("Admin server shutdown error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "Admin server shutdown error: %v\n", err)
					}
				}
			}

			// 2. Stop SMTP servers (no new mail)
			if resources.smtpSrv != nil {
				if resources.logger != nil {
					resources.logger.Info("Shutting down SMTP servers")
				}
				if err := resources.smtpSrv.Close(); err != nil {
					if resources.logger != nil {
						resources.logger.Error("SMTP server shutdown error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "SMTP server shutdown error: %v\n", err)
					}
				}
			}

			// 3. Stop IMAP servers (no new client connections)
			if resources.imapSrv != nil {
				if resources.logger != nil {
					resources.logger.Info("Shutting down IMAP servers")
				}
				if err := resources.imapSrv.Close(); err != nil {
					if resources.logger != nil {
						resources.logger.Error("IMAP server shutdown error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "IMAP server shutdown error: %v\n", err)
					}
				}
			}

			// 4. Stop delivery engine (finish in-flight deliveries)
			if resources.deliveryEngine != nil {
				if resources.logger != nil {
					resources.logger.Info("Stopping delivery engine")
				}
				resources.deliveryEngine.Stop()
			}

			// 5. Close Redis queue connection
			if resources.redisQueue != nil {
				if resources.logger != nil {
					resources.logger.Info("Closing Redis queue connection")
				}
				if err := resources.redisQueue.Close(); err != nil {
					if resources.logger != nil {
						resources.logger.Error("Redis queue close error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "Redis queue close error: %v\n", err)
					}
				}
			}

			// 6. Close database last (after all users are done)
			if resources.db != nil {
				if resources.logger != nil {
					resources.logger.Info("Closing database")
				}
				if err := resources.db.Close(); err != nil {
					if resources.logger != nil {
						resources.logger.Error("Database close error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "Database close error: %v\n", err)
					}
				}
			}

			if resources.logger != nil {
				resources.logger.Info("Shutdown complete")
			}
		}

		// Ensure cleanup runs on panic
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "PANIC during server operation: %v\n", r)
				cleanup()
				panic(r) // Re-panic after cleanup
			}
		}()

		// Initialize logger early so we can use it for startup errors
		logger, err := logging.New(logging.Config{
			Level:  cfg.Logging.Level,
			Format: cfg.Logging.Format,
			Output: cfg.Logging.Output,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}
		resources.logger = logger
		logger.Info("Mail server starting", "hostname", cfg.Server.Hostname)

		// Open database with proper error handling
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to open database: %w", err)
		}
		resources.db = db
		logger.Info("Database opened", "path", cfg.Storage.DatabasePath)

		// Run migrations with timeout
		migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := db.Migrate(migrateCtx); err != nil {
			migrateCancel()
			cleanup()
			return fmt.Errorf("failed to run migrations: %w", err)
		}
		migrateCancel()
		logger.Info("Database migrations complete")

		// Initialize TLS with validation
		tlsManager, err := security.NewTLSManager(cfg)
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to initialize TLS: %w", err)
		}
		if tlsManager.HasTLS() {
			logger.Info("TLS configured")
		} else {
			logger.Warn("TLS not configured - server will run without encryption")
		}

		// Initialize authenticator
		authenticator := auth.NewAuthenticator(db.DB)

		// Initialize maildir store
		store, err := maildir.NewStore(db.DB, cfg.Storage.MaildirPath)
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to initialize maildir store: %w", err)
		}
		logger.Info("Maildir store initialized", "path", cfg.Storage.MaildirPath)

		// Initialize Redis queue with connection validation
		retryMaxAge, _ := time.ParseDuration(cfg.Queue.RetryMaxAge)
		if retryMaxAge == 0 {
			retryMaxAge = 7 * 24 * time.Hour
		}
		redisQueue, err := queue.NewRedisQueue(queue.Config{
			RedisURL:    cfg.Queue.RedisURL,
			Prefix:      cfg.Queue.Prefix,
			MaxRetries:  cfg.Queue.MaxRetries,
			RetryMaxAge: retryMaxAge,
		})
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to initialize Redis queue: %w", err)
		}
		resources.redisQueue = redisQueue
		logger.Info("Redis queue connected", "url", cfg.Queue.RedisURL)

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
		resources.deliveryEngine = deliveryEngine
		deliveryEngine.Start()
		logger.Info("Delivery engine started", "workers", cfg.Delivery.Workers)

		// Create IMAP server
		imapAddr := fmt.Sprintf(":%d", cfg.Server.IMAPPort)
		imapsAddr := fmt.Sprintf(":%d", cfg.Server.IMAPSPort)
		imapSrv := imapserver.NewServer(authenticator, store, imapAddr, imapsAddr, tlsManager.TLSConfig())
		resources.imapSrv = imapSrv

		// Create SMTP backend and server
		smtpBackend, err := smtpserver.NewBackend(cfg, authenticator, store, deliveryEngine, logger)
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to create SMTP backend: %w", err)
		}

		// Wire up SMTP -> IMAP notifications
		smtpBackend.SetLocalDeliveryNotifier(func(username, mailbox string) {
			imapSrv.NotifyMailboxUpdateByName(username, mailbox)
		})

		// Initialize Sieve executor if enabled
		var sieveStore *sieve.Store
		if cfg.Sieve.Enabled {
			sieveStore = sieve.NewStore(db.DB)
			sieveExecutor := sieve.NewExecutor(db.DB)
			smtpBackend.SetSieveExecutor(sieveExecutor)
			logger.Info("Sieve filtering enabled")
		}

		smtpSrv := smtpserver.NewServer(smtpBackend, cfg, tlsManager.TLSConfig())
		resources.smtpSrv = smtpSrv

		// Start all servers with error handling
		fmt.Printf("Mail server starting on %s\n", cfg.Server.Hostname)
		fmt.Printf("  SMTP:  %d (MX), %d (submission), %d (SMTPS)\n",
			cfg.Server.SMTPPort, cfg.Server.SubmissionPort, cfg.Server.SMTPSPort)
		fmt.Printf("  IMAP:  %d, %d (TLS)\n", cfg.Server.IMAPPort, cfg.Server.IMAPSPort)

		// Start IMAP servers
		if err := imapSrv.ListenAndServe(); err != nil {
			cleanup()
			return fmt.Errorf("failed to start IMAP server: %w", err)
		}
		logger.Info("IMAP server started", "port", cfg.Server.IMAPPort)

		if tlsManager.HasTLS() {
			if err := imapSrv.ListenAndServeTLS(tlsManager.TLSConfig()); err != nil {
				cleanup()
				return fmt.Errorf("failed to start IMAPS server: %w", err)
			}
			logger.Info("IMAPS server started", "port", cfg.Server.IMAPSPort)
		}

		// Start SMTP servers
		if err := smtpSrv.ListenAndServe(); err != nil {
			cleanup()
			return fmt.Errorf("failed to start SMTP server: %w", err)
		}
		logger.Info("SMTP MX server started", "port", cfg.Server.SMTPPort)

		if err := smtpSrv.ListenAndServeSubmission(); err != nil {
			cleanup()
			return fmt.Errorf("failed to start SMTP submission server: %w", err)
		}
		logger.Info("SMTP submission server started", "port", cfg.Server.SubmissionPort)

		if tlsManager.HasTLS() {
			if err := smtpSrv.ListenAndServeTLS(); err != nil {
				cleanup()
				return fmt.Errorf("failed to start SMTPS server: %w", err)
			}
			logger.Info("SMTPS server started", "port", cfg.Server.SMTPSPort)
		}

		// Start admin server if enabled
		if cfg.Admin.Enabled {
			adminSrv, err := admin.NewServer(cfg, db.DB, authenticator, store, sieveStore, redisQueue, logger)
			if err != nil {
				logger.Warn("Failed to initialize admin server", "error", err.Error())
			} else {
				resources.adminSrv = adminSrv
				adminAddr := fmt.Sprintf("%s:%d", cfg.Admin.Listen, cfg.Admin.Port)
				go func() {
					if err := adminSrv.Start(adminAddr); err != nil {
						logger.Error("Admin server error", "error", err.Error())
					}
				}()
				fmt.Printf("  Admin: http://%s\n", adminAddr)
				logger.Info("Admin server started", "addr", adminAddr)
			}
		}

		// Start autodiscover server if enabled
		if cfg.Autodiscover.Enabled {
			displayName := cfg.Autodiscover.DisplayName
			if displayName == "" {
				displayName = cfg.Server.Domain + " Mail"
			}
			autodiscoverSrv := autodiscover.NewServer(autodiscover.Config{
				Domain:      cfg.Server.Domain,
				Hostname:    cfg.Server.Hostname,
				IMAPPort:    cfg.Server.IMAPSPort,
				SMTPPort:    cfg.Server.SubmissionPort,
				DisplayName: displayName,
			}, logger.Logger)
			autodiscoverAddr := fmt.Sprintf("%s:%d", cfg.Autodiscover.Listen, cfg.Autodiscover.Port)
			go func() {
				if err := autodiscoverSrv.ListenAndServe(context.Background(), autodiscoverAddr); err != nil {
					logger.Error("Autodiscover server error", "error", err.Error())
				}
			}()
			fmt.Printf("  Autodiscover: http://%s\n", autodiscoverAddr)
			logger.Info("Autodiscover server started", "addr", autodiscoverAddr)
		}

		fmt.Println("\nServer is running. Press Ctrl+C to stop.")
		logger.Info("All services started successfully")

		// Setup signal handling for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

		// Wait for shutdown signal
		sig := <-sigCh
		logger.Info("Received shutdown signal", "signal", sig.String())
		fmt.Printf("\nReceived signal %s, shutting down...\n", sig)

		// Perform graceful shutdown
		cleanup()

		logger.Info("Server stopped")
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

// DNS management commands
var dnsCmd = &cobra.Command{
	Use:   "dns",
	Short: "DNS record checking and generation",
}

var dnsCheckCmd = &cobra.Command{
	Use:   "check <domain>",
	Short: "Check DNS configuration for a domain",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		domain := args[0]
		mailServer := cfg.Server.Hostname

		checker, err := dns.NewChecker(domain, mailServer)
		if err != nil {
			return fmt.Errorf("failed to create DNS checker: %w", err)
		}
		results := checker.CheckAll(context.Background())

		fmt.Printf("DNS Check for %s (mail server: %s)\n", domain, mailServer)
		fmt.Println("=" + "========================================")

		for _, r := range results {
			var icon string
			switch r.Status {
			case dns.StatusPass:
				icon = "✓"
			case dns.StatusFail:
				icon = "✗"
			case dns.StatusWarning:
				icon = "!"
			case dns.StatusMissing:
				icon = "?"
			}

			fmt.Printf("[%s] %-8s %s\n", icon, r.RecordType, r.Status)
			if r.Actual != "" {
				fmt.Printf("    Found:    %s\n", r.Actual)
			}
			if r.Expected != "" && r.Status != dns.StatusPass {
				fmt.Printf("    Expected: %s\n", r.Expected)
			}
			fmt.Printf("    %s\n\n", r.Message)
		}

		return nil
	},
}

var dnsGenerateCmd = &cobra.Command{
	Use:   "generate <domain> [server-ip]",
	Short: "Generate required DNS records for a domain",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		domain := args[0]
		mailServer := cfg.Server.Hostname
		serverIP := ""
		if len(args) > 1 {
			serverIP = args[1]
		}

		generator, err := dns.NewGenerator(domain, mailServer, serverIP)
		if err != nil {
			return fmt.Errorf("failed to create DNS generator: %w", err)
		}

		// Try to load DKIM key if configured
		for _, d := range cfg.Domains {
			if d.Name == domain && d.DKIMKeyFile != "" {
				// Read public key
				fmt.Printf("Using DKIM key from %s\n\n", d.DKIMKeyFile)
			}
		}

		records := generator.GenerateAll()

		fmt.Println(dns.FormatForProvider(records, domain))

		fmt.Println("\nZone file format:")
		fmt.Println("-----------------")
		fmt.Println(dns.FormatAsZone(records, domain))

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

	// DNS commands
	dnsCmd.AddCommand(dnsCheckCmd)
	dnsCmd.AddCommand(dnsGenerateCmd)
	rootCmd.AddCommand(dnsCmd)
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
