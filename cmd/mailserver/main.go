package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fenilsonani/email-server/internal/admin"
	"github.com/fenilsonani/email-server/internal/api"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/features"
	"github.com/fenilsonani/email-server/internal/autodiscover"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/dav"
	"github.com/fenilsonani/email-server/internal/dns"
	"github.com/fenilsonani/email-server/internal/health"
	imapserver "github.com/fenilsonani/email-server/internal/imap"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/migration"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/security"
	"github.com/fenilsonani/email-server/internal/setup"
	"github.com/fenilsonani/email-server/internal/sieve"
	"github.com/fenilsonani/email-server/internal/tuning"
	smtpserver "github.com/fenilsonani/email-server/internal/smtp"
	"github.com/fenilsonani/email-server/internal/smtp/delivery"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
	"github.com/fenilsonani/email-server/internal/storage/metadata"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
	db      metadata.Store
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
			db               metadata.Store
			redisQueue       *queue.RedisQueue
			deliveryEngine   *delivery.Engine
			imapSrv          *imapserver.Server
			smtpSrv          *smtpserver.Server
			davSrv           *dav.Server
			adminSrv         *admin.Server
			apiSrv           *api.Server
			logger           *logging.Logger
			healthMonitor    *health.Monitor
			featureScheduler *features.Scheduler
		}
		resources := &resourceTracker{}

		// Initialize health monitor (auto-starts background checks)
		healthMonitor := health.NewMonitor()
		resources.healthMonitor = healthMonitor

		// Auto-detect environment and apply optimizations
		env := setup.DetectEnvironment()
		if env["container"] != "none" {
			fmt.Printf("Detected container environment: %s\n", env["container"])
		}

		// Auto-tune performance based on system resources
		autoConfig := tuning.AutoTune()
		autoConfig.ApplyEnvOverrides()

		// Apply auto-tuned values where config values are not explicitly set
		if cfg.Delivery.Workers == 0 {
			cfg.Delivery.Workers = autoConfig.DeliveryWorkers
		}
		if cfg.Database.MaxOpenConns == 0 {
			cfg.Database.MaxOpenConns = autoConfig.DBMaxOpenConns
		}
		if cfg.Database.MaxIdleConns == 0 {
			cfg.Database.MaxIdleConns = autoConfig.DBMaxIdleConns
		}
		if cfg.Queue.PoolSize == 0 {
			cfg.Queue.PoolSize = autoConfig.RedisPoolSize
		}
		if cfg.Queue.MinIdleConns == 0 {
			cfg.Queue.MinIdleConns = autoConfig.RedisMinIdle
		}
		if cfg.Security.MaxMessageSize == 0 {
			cfg.Security.MaxMessageSize = int(autoConfig.MaxMessageSize)
		}

		fmt.Printf("Auto-tuned for %d CPUs, %dMB memory (%s)\n",
			autoConfig.NumCPU, autoConfig.TotalMemoryMB, autoConfig.Environment)

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
			if resources.apiSrv != nil {
				if resources.logger != nil {
					resources.logger.Info("Shutting down API server")
				}
				if err := resources.apiSrv.Shutdown(shutdownCtx); err != nil {
					if resources.logger != nil {
						resources.logger.Error("API server shutdown error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "API server shutdown error: %v\n", err)
					}
				}
			}

			// Stop feature scheduler
			if resources.featureScheduler != nil {
				if resources.logger != nil {
					resources.logger.Info("Shutting down feature scheduler")
				}
				resources.featureScheduler.Stop()
			}

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

			// 2. Stop DAV server
			if resources.davSrv != nil {
				if resources.logger != nil {
					resources.logger.Info("Shutting down DAV server")
				}
				if err := resources.davSrv.Shutdown(shutdownCtx); err != nil {
					if resources.logger != nil {
						resources.logger.Error("DAV server shutdown error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "DAV server shutdown error: %v\n", err)
					}
				}
			}

			// 3. Stop SMTP servers (no new mail)
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

			// 7. Stop health monitor
			if resources.healthMonitor != nil {
				if resources.logger != nil {
					resources.logger.Info("Stopping health monitor")
				}
				resources.healthMonitor.Stop()
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

		// Open database with proper error handling using factory
		db, err = metadata.OpenFromConfig(cfg.Database)
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to open database: %w", err)
		}
		resources.db = db
		logger.Info("Database opened", "driver", db.Driver())

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
		authenticator := auth.NewAuthenticator(db.RawDB())

		// Initialize maildir store
		store, err := maildir.NewStore(db.RawDB(), cfg.Storage.MaildirPath)
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
		dialTimeout, _ := time.ParseDuration(cfg.Queue.DialTimeout)
		readTimeout, _ := time.ParseDuration(cfg.Queue.ReadTimeout)
		writeTimeout, _ := time.ParseDuration(cfg.Queue.WriteTimeout)
		redisQueue, err := queue.NewRedisQueue(queue.Config{
			RedisURL:       cfg.Queue.RedisURL,
			Mode:           cfg.Queue.Mode,
			SentinelMaster: cfg.Queue.SentinelMaster,
			SentinelAddrs:  cfg.Queue.SentinelAddrs,
			ClusterAddrs:   cfg.Queue.ClusterAddrs,
			Password:       cfg.Queue.Password,
			DB:             cfg.Queue.DB,
			Prefix:         cfg.Queue.Prefix,
			MaxRetries:     cfg.Queue.MaxRetries,
			RetryMaxAge:    retryMaxAge,
			PoolSize:       cfg.Queue.PoolSize,
			MinIdleConns:   cfg.Queue.MinIdleConns,
			DialTimeout:    dialTimeout,
			ReadTimeout:    readTimeout,
			WriteTimeout:   writeTimeout,
		})
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to initialize Redis queue: %w", err)
		}
		resources.redisQueue = redisQueue
		logger.Info("Redis queue connected", "url", cfg.Queue.RedisURL)

		// Register components with health monitor for automatic health checks
		healthMonitor.RegisterDatabase(db.RawDB())
		healthMonitor.RegisterRedis(redisQueue.Client())
		healthMonitor.RegisterDiskSpace(cfg.Storage.DataDir, cfg.Storage.MaildirPath)

		// Set up self-healing callbacks
		healthMonitor.OnUnhealthy(func(name string, result health.CheckResult) {
			logger.Warn("Component unhealthy", "component", name, "message", result.Message)
		})
		healthMonitor.OnRecovered(func(name string, result health.CheckResult) {
			logger.Info("Component recovered", "component", name)
		})

		// Start health monitoring (runs in background)
		healthMonitor.Start()
		logger.Info("Health monitoring started")

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
		// QueuePath for bounce messages - same as SMTP backend queue path
		queuePath := filepath.Join(cfg.Storage.DataDir, "queue")
		deliveryEngine := delivery.NewEngine(delivery.Config{
			Workers:        cfg.Delivery.Workers,
			Hostname:       cfg.Server.Hostname,
			ConnectTimeout: connectTimeout,
			CommandTimeout: commandTimeout,
			MaxMessageSize: int64(cfg.Security.MaxMessageSize),
			RequireTLS:     cfg.Delivery.RequireTLS,
			VerifyTLS:      cfg.Delivery.VerifyTLS,
			RelayHost:      cfg.Delivery.RelayHost,
			QueuePath:      queuePath,
		}, redisQueue, dkimPool, logger, db.RawDB())
		resources.deliveryEngine = deliveryEngine
		deliveryEngine.Start()
		logger.Info("Delivery engine started", "workers", cfg.Delivery.Workers)

		// Create IMAP server
		imapAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.IMAPPort)
		imapsAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.IMAPSPort)
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
			sieveStore = sieve.NewStore(db.RawDB())
			sieveExecutor := sieve.NewExecutor(db.RawDB())
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

		// Start DAV server (CalDAV/CardDAV)
		if cfg.Server.DAVPort > 0 {
			davSrv, err := dav.NewServer(cfg, authenticator, db.RawDB())
			if err != nil {
				logger.Warn("Failed to initialize DAV server", "error", err.Error())
			} else {
				resources.davSrv = davSrv
				davAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.DAVPort)
				go func() {
					if err := davSrv.Start(davAddr, tlsManager.TLSConfig()); err != nil {
						logger.Error("DAV server error", "error", err.Error())
					}
				}()
				fmt.Printf("  DAV:   %d (CalDAV/CardDAV)\n", cfg.Server.DAVPort)
				logger.Info("DAV server started", "port", cfg.Server.DAVPort)
			}
		}

		// Start admin server if enabled
		if cfg.Admin.Enabled {
			adminSrv, err := admin.NewServer(cfg, db.RawDB(), authenticator, store, sieveStore, redisQueue, logger)
			if err != nil {
				logger.Warn("Failed to initialize admin server", "error", err.Error())
			} else {
				// Initialize features store for unique features (Screener, Aliases, etc.)
				featuresStore := features.NewStore(db.RawDB())
				adminSrv.SetFeaturesStore(featuresStore)

				// Start feature scheduler for scheduled sends, snooze wake-ups, undo send
				featureScheduler := features.NewScheduler(featuresStore, logger)

				// Configure email sender for scheduled sends (use local SMTP)
				emailSender := features.NewLocalEmailSender()
				featureScheduler.SetEmailSender(emailSender)

				featureScheduler.Start()
				resources.featureScheduler = featureScheduler
				logger.Info("Feature scheduler started with email sender")

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
		// Note: Autodiscover handlers dynamically extract domain from email addresses
		// for multi-domain support. These config values are defaults/fallbacks.
		if cfg.Autodiscover.Enabled {
			displayName := cfg.Autodiscover.DisplayName
			if displayName == "" {
				displayName = "Mail Service" // Generic default; handlers use email domain dynamically
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

		// Start transactional API server if enabled
		if cfg.API.Enabled {
			apiSrv, err := api.NewServer(cfg, db.RawDB(), redisQueue, deliveryEngine, logger)
			if err != nil {
				logger.Warn("Failed to initialize API server", "error", err.Error())
			} else {
				resources.apiSrv = apiSrv
				apiAddr := fmt.Sprintf("%s:%d", cfg.API.Listen, cfg.API.Port)
				go func() {
					if err := apiSrv.Start(apiAddr); err != nil {
						logger.Error("API server error", "error", err.Error())
					}
				}()
				fmt.Printf("  API: http://%s\n", apiAddr)
				logger.Info("Transactional API server started", "addr", apiAddr)
			}
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

var domainGenerateDKIM bool
var domainDKIMBits int
var domainDKIMSelector string
var domainDKIMStorage string

var domainAddCmd = &cobra.Command{
	Use:   "add <domain>",
	Short: "Add a new domain",
	Long: `Add a new domain to the mail server.

Use --generate-dkim to automatically generate a DKIM signing key for the domain.`,
	Args: cobra.ExactArgs(1),
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
			domainName, domainDKIMSelector,
		)
		if err != nil {
			return fmt.Errorf("failed to add domain: %w", err)
		}

		id, _ := result.LastInsertId()
		fmt.Printf("Domain '%s' added with ID %d\n", domainName, id)

		// Generate DKIM key if requested
		if domainGenerateDKIM {
			// Get DKIM key directory
			dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
			if cfg.Storage.MaildirPath != "" {
				dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
			}

			store := security.NewKeyStore(domainDKIMStorage, dkimPath, db.RawDB())

			fmt.Printf("Generating %d-bit DKIM key...\n", domainDKIMBits)
			_, err = security.GenerateAndSaveKey(context.Background(), store, domainName, domainDKIMSelector, domainDKIMBits)
			if err != nil {
				return fmt.Errorf("failed to generate DKIM key: %w", err)
			}

			// Get DNS record
			recordName, recordValue, err := security.GetDNSRecord(context.Background(), store, domainName)
			if err != nil {
				return fmt.Errorf("failed to get DNS record: %w", err)
			}

			fmt.Printf("\nDKIM key generated!\n\n")
			fmt.Printf("Add this DNS TXT record:\n")
			fmt.Printf("Name:  %s\n", recordName)
			fmt.Printf("Type:  TXT\n")
			fmt.Printf("Value: %s\n", recordValue)
		}

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
		authenticator := auth.NewAuthenticator(db.RawDB())
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

// DKIM management commands
var dkimCmd = &cobra.Command{
	Use:   "dkim",
	Short: "DKIM key management",
}

var dkimBits int
var dkimSelector string
var dkimStorage string
var dkimForce bool
var dkimFormat string

var dkimGenerateCmd = &cobra.Command{
	Use:   "generate <domain>",
	Short: "Generate new DKIM key for a domain",
	Long: `Generate a new DKIM signing key for a domain.

The key will be stored based on the --storage option:
  - file: Store as files in the DKIM key directory (default)
  - database: Store in the database
  - hybrid: Store in both file and database`,
	Args: cobra.ExactArgs(1),
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

		// Check if domain exists
		var domainID int64
		err = db.QueryRowContext(context.Background(),
			"SELECT id FROM domains WHERE name = ?", domainName).Scan(&domainID)
		if err != nil {
			return fmt.Errorf("domain '%s' not found. Add it first with: mailserver domain add %s", domainName, domainName)
		}

		// Get DKIM key directory
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}

		// Create key store based on storage type
		store := security.NewKeyStore(dkimStorage, dkimPath, db.RawDB())

		// Check if key already exists
		if store.KeyExists(context.Background(), domainName) && !dkimForce {
			return fmt.Errorf("DKIM key already exists for domain '%s'. Use --force to overwrite", domainName)
		}

		// Generate and save key
		fmt.Printf("Generating %d-bit DKIM key for %s...\n", dkimBits, domainName)
		_, err = security.GenerateAndSaveKey(context.Background(), store, domainName, dkimSelector, dkimBits)
		if err != nil {
			return fmt.Errorf("failed to generate key: %w", err)
		}

		// Get DNS record
		recordName, recordValue, err := security.GetDNSRecord(context.Background(), store, domainName)
		if err != nil {
			return fmt.Errorf("failed to get DNS record: %w", err)
		}

		fmt.Printf("\nDKIM key generated successfully!\n\n")
		fmt.Printf("Add this DNS TXT record to your domain:\n\n")
		fmt.Printf("Name:  %s\n", recordName)
		fmt.Printf("Type:  TXT\n")
		fmt.Printf("Value: %s\n\n", recordValue)

		return nil
	},
}

var dkimShowCmd = &cobra.Command{
	Use:   "show <domain>",
	Short: "Show DKIM public key and DNS record",
	Long: `Display the DKIM public key for a domain in various formats.

Formats:
  - dns: Full DNS TXT record format (default)
  - bind: BIND zone file format
  - raw: Just the base64-encoded public key`,
	Args: cobra.ExactArgs(1),
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

		// Get DKIM key directory
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}

		// Try to determine storage type from database
		var storageType string
		err = db.QueryRowContext(context.Background(),
			"SELECT COALESCE(dkim_storage_type, 'file') FROM domains WHERE name = ?",
			domainName).Scan(&storageType)
		if err != nil {
			storageType = "file"
		}

		store := security.NewKeyStore(storageType, dkimPath, db.RawDB())

		// Get key metadata
		meta, err := store.GetKeyMetadata(context.Background(), domainName)
		if err != nil {
			return fmt.Errorf("failed to get key metadata: %w", err)
		}

		if !meta.HasKey {
			return fmt.Errorf("no DKIM key found for domain '%s'. Generate one with: mailserver dkim generate %s", domainName, domainName)
		}

		// Get DNS record
		recordName, recordValue, err := security.GetDNSRecord(context.Background(), store, domainName)
		if err != nil {
			return fmt.Errorf("failed to get DNS record: %w", err)
		}

		switch dkimFormat {
		case "raw":
			// Extract just the public key value
			parts := strings.Split(recordValue, "p=")
			if len(parts) == 2 {
				fmt.Println(parts[1])
			} else {
				fmt.Println(recordValue)
			}
		case "bind":
			fmt.Printf("; DKIM record for %s\n", domainName)
			fmt.Printf("%s. IN TXT \"%s\"\n", recordName, recordValue)
		default: // dns
			fmt.Printf("DKIM DNS Record for %s\n", domainName)
			fmt.Printf("============================\n\n")
			fmt.Printf("Selector:   %s\n", meta.Selector)
			fmt.Printf("Algorithm:  %s\n", meta.Algorithm)
			if !meta.CreatedAt.IsZero() {
				fmt.Printf("Created:    %s\n", meta.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			fmt.Printf("Storage:    %s\n\n", meta.StorageType)
			fmt.Printf("DNS Record Name:\n  %s\n\n", recordName)
			fmt.Printf("DNS Record Type:\n  TXT\n\n")
			fmt.Printf("DNS Record Value:\n  %s\n", recordValue)
		}

		return nil
	},
}

var dkimRotateCmd = &cobra.Command{
	Use:   "rotate <domain>",
	Short: "Rotate DKIM key with a new selector",
	Long: `Generate a new DKIM key with a new selector for key rotation.

This command:
1. Generates a new key with a timestamp-based selector
2. Keeps the old key configuration for reference
3. Shows both old and new DNS records for transition`,
	Args: cobra.ExactArgs(1),
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

		// Get DKIM key directory
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}

		// Get current storage type
		var storageType string
		err = db.QueryRowContext(context.Background(),
			"SELECT COALESCE(dkim_storage_type, 'file') FROM domains WHERE name = ?",
			domainName).Scan(&storageType)
		if err != nil {
			return fmt.Errorf("domain '%s' not found", domainName)
		}

		store := security.NewKeyStore(storageType, dkimPath, db.RawDB())

		fmt.Printf("Rotating DKIM key for %s...\n", domainName)

		// Rotate key
		newSelector, _, err := security.RotateKey(context.Background(), store, domainName, dkimBits)
		if err != nil {
			return fmt.Errorf("failed to rotate key: %w", err)
		}

		// Get new DNS record
		recordName, recordValue, err := security.GetDNSRecord(context.Background(), store, domainName)
		if err != nil {
			return fmt.Errorf("failed to get DNS record: %w", err)
		}

		fmt.Printf("\nDKIM key rotated successfully!\n")
		fmt.Printf("New selector: %s\n\n", newSelector)
		fmt.Printf("Add this NEW DNS TXT record:\n\n")
		fmt.Printf("Name:  %s\n", recordName)
		fmt.Printf("Type:  TXT\n")
		fmt.Printf("Value: %s\n\n", recordValue)
		fmt.Printf("IMPORTANT: Keep the old DKIM record active for 24-48 hours\n")
		fmt.Printf("to allow in-flight emails to be verified.\n")

		return nil
	},
}

var dkimListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all domains and their DKIM key status",
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

		// Get DKIM key directory
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}

		store := security.NewFileKeyStore(dkimPath, db.RawDB())
		domains, err := store.ListDomains(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list domains: %w", err)
		}

		fmt.Printf("%-30s %-10s %-10s %-10s %s\n", "DOMAIN", "SELECTOR", "HAS KEY", "STORAGE", "CREATED")
		fmt.Println("-------------------------------------------------------------------------------")

		for _, meta := range domains {
			hasKey := "no"
			if meta.HasKey {
				hasKey = "yes"
			}
			created := "-"
			if !meta.CreatedAt.IsZero() {
				created = meta.CreatedAt.Format("2006-01-02")
			}
			fmt.Printf("%-30s %-10s %-10s %-10s %s\n",
				meta.Domain, meta.Selector, hasKey, meta.StorageType, created)
		}

		return nil
	},
}

var dkimAutoRotateDays int

var dkimAutoRotateCmd = &cobra.Command{
	Use:   "auto-rotate",
	Short: "Automatically rotate DKIM keys older than specified days",
	Long: `Check all domains and rotate DKIM keys that are older than the specified number of days.

This command is designed to be run via cron or systemd timer for automatic key rotation.
Default rotation period is 90 days.`,
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

		// Get DKIM key directory
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}

		store := security.NewFileKeyStore(dkimPath, db.RawDB())
		domains, err := store.ListDomains(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list domains: %w", err)
		}

		rotationThreshold := time.Now().AddDate(0, 0, -dkimAutoRotateDays)
		rotatedCount := 0

		for _, meta := range domains {
			if !meta.HasKey {
				continue
			}

			// Check if key is older than threshold
			if meta.CreatedAt.IsZero() || meta.CreatedAt.After(rotationThreshold) {
				continue
			}

			fmt.Printf("Rotating key for %s (created: %s)...\n", meta.Domain, meta.CreatedAt.Format("2006-01-02"))

			// Get storage type for this domain
			var storageType string
			err = db.QueryRowContext(context.Background(),
				"SELECT COALESCE(dkim_storage_type, 'file') FROM domains WHERE name = ?",
				meta.Domain).Scan(&storageType)
			if err != nil {
				storageType = "file"
			}

			domainStore := security.NewKeyStore(storageType, dkimPath, db.RawDB())
			newSelector, _, err := security.RotateKey(context.Background(), domainStore, meta.Domain, 2048)
			if err != nil {
				fmt.Printf("  ERROR: Failed to rotate key for %s: %v\n", meta.Domain, err)
				continue
			}

			fmt.Printf("  SUCCESS: New selector: %s\n", newSelector)

			// Get new DNS record
			recordName, recordValue, err := security.GetDNSRecord(context.Background(), domainStore, meta.Domain)
			if err == nil {
				fmt.Printf("  DNS Record: %s\n", recordName)
				fmt.Printf("  Value: %s\n\n", recordValue)
			}

			rotatedCount++
		}

		if rotatedCount == 0 {
			fmt.Println("No keys needed rotation.")
		} else {
			fmt.Printf("\nRotated %d key(s). Remember to update DNS records!\n", rotatedCount)
		}

		return nil
	},
}

// Backup command
var backupCmd = &cobra.Command{
	Use:   "backup <output-path>",
	Short: "Create a full backup of all mail server data",
	Long: `Create a complete backup including:
  - Database (users, domains, settings)
  - All emails (maildir)
  - DKIM keys
  - Configuration

The backup is created as a compressed tar.gz archive.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outputPath := args[0]

		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		fmt.Println("=== Mail Server Backup ===")
		fmt.Printf("Output: %s\n\n", outputPath)

		// Create temp directory for backup
		tempDir, err := os.MkdirTemp("", "mailserver-backup-")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer os.RemoveAll(tempDir)

		// Backup database
		fmt.Print("Backing up database... ")
		if cfg.Storage.DatabasePath != "" {
			if _, err := os.Stat(cfg.Storage.DatabasePath); err == nil {
				srcDB, err := os.Open(cfg.Storage.DatabasePath)
				if err != nil {
					return fmt.Errorf("failed to open database: %w", err)
				}
				dstDB, err := os.Create(filepath.Join(tempDir, "metadata.db"))
				if err != nil {
					srcDB.Close()
					return fmt.Errorf("failed to create backup database: %w", err)
				}
				_, err = io.Copy(dstDB, srcDB)
				srcDB.Close()
				dstDB.Close()
				if err != nil {
					return fmt.Errorf("failed to copy database: %w", err)
				}
				fmt.Println("done")
			} else {
				fmt.Println("skipped (not found)")
			}
		}

		// Backup maildir
		fmt.Print("Backing up emails... ")
		if cfg.Storage.MaildirPath != "" {
			if _, err := os.Stat(cfg.Storage.MaildirPath); err == nil {
				if err := copyDir(cfg.Storage.MaildirPath, filepath.Join(tempDir, "maildir")); err != nil {
					return fmt.Errorf("failed to backup maildir: %w", err)
				}
				fmt.Println("done")
			} else {
				fmt.Println("skipped (not found)")
			}
		}

		// Backup DKIM keys
		fmt.Print("Backing up DKIM keys... ")
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}
		if _, err := os.Stat(dkimPath); err == nil {
			if err := copyDir(dkimPath, filepath.Join(tempDir, "dkim")); err != nil {
				return fmt.Errorf("failed to backup DKIM keys: %w", err)
			}
			fmt.Println("done")
		} else {
			fmt.Println("skipped (not found)")
		}

		// Create tar.gz archive
		fmt.Print("Creating archive... ")
		if err := createTarGz(outputPath, tempDir); err != nil {
			return fmt.Errorf("failed to create archive: %w", err)
		}
		fmt.Println("done")

		// Get file size
		fi, _ := os.Stat(outputPath)
		fmt.Printf("\nBackup complete: %s (%s)\n", outputPath, formatBytes(fi.Size()))

		return nil
	},
}

// Restore command
var restoreCmd = &cobra.Command{
	Use:   "restore <backup-path>",
	Short: "Restore mail server data from a backup",
	Long: `Restore all data from a backup archive created with 'mailserver backup'.

WARNING: This will overwrite existing data!`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupPath := args[0]
		force, _ := cmd.Flags().GetBool("force")

		if !force {
			fmt.Print("WARNING: This will overwrite existing data. Continue? [y/N] ")
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		fmt.Println("=== Mail Server Restore ===")
		fmt.Printf("Source: %s\n\n", backupPath)

		// Create temp directory for extraction
		tempDir, err := os.MkdirTemp("", "mailserver-restore-")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer os.RemoveAll(tempDir)

		// Extract archive
		fmt.Print("Extracting archive... ")
		if err := extractTarGz(backupPath, tempDir); err != nil {
			return fmt.Errorf("failed to extract archive: %w", err)
		}
		fmt.Println("done")

		// Restore database
		fmt.Print("Restoring database... ")
		dbBackup := filepath.Join(tempDir, "metadata.db")
		if _, err := os.Stat(dbBackup); err == nil {
			if err := copyFile(dbBackup, cfg.Storage.DatabasePath); err != nil {
				return fmt.Errorf("failed to restore database: %w", err)
			}
			fmt.Println("done")
		} else {
			fmt.Println("skipped (not in backup)")
		}

		// Restore maildir
		fmt.Print("Restoring emails... ")
		maildirBackup := filepath.Join(tempDir, "maildir")
		if _, err := os.Stat(maildirBackup); err == nil {
			if err := copyDir(maildirBackup, cfg.Storage.MaildirPath); err != nil {
				return fmt.Errorf("failed to restore maildir: %w", err)
			}
			fmt.Println("done")
		} else {
			fmt.Println("skipped (not in backup)")
		}

		// Restore DKIM keys
		fmt.Print("Restoring DKIM keys... ")
		dkimBackup := filepath.Join(tempDir, "dkim")
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}
		if _, err := os.Stat(dkimBackup); err == nil {
			if err := copyDir(dkimBackup, dkimPath); err != nil {
				return fmt.Errorf("failed to restore DKIM keys: %w", err)
			}
			fmt.Println("done")
		} else {
			fmt.Println("skipped (not in backup)")
		}

		fmt.Println("\nRestore complete! Restart the server with: systemctl restart mailserver")

		return nil
	},
}

// Export command for migration
var exportCmd = &cobra.Command{
	Use:   "export <remote-server>",
	Short: "Export and transfer all data to a remote server",
	Long: `Export all mail server data and transfer it to a remote server via SSH.

This command will:
1. Create a backup of all data
2. Transfer it to the remote server
3. Extract it on the remote server

Example:
  mailserver export root@newserver.example.com
  mailserver export root@192.168.1.100 --remote-path /var/mailserver`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteServer := args[0]
		remotePath, _ := cmd.Flags().GetString("remote-path")

		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		fmt.Println("=== Mail Server Export ===")
		fmt.Printf("Destination: %s:%s\n\n", remoteServer, remotePath)

		// Create temp backup
		tempFile, err := os.CreateTemp("", "mailserver-export-*.tar.gz")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tempPath := tempFile.Name()
		tempFile.Close()
		defer os.Remove(tempPath)

		// Create backup
		fmt.Println("Step 1: Creating backup...")
		tempDir, err := os.MkdirTemp("", "mailserver-backup-")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer os.RemoveAll(tempDir)

		// Backup database
		if cfg.Storage.DatabasePath != "" {
			if _, err := os.Stat(cfg.Storage.DatabasePath); err == nil {
				copyFile(cfg.Storage.DatabasePath, filepath.Join(tempDir, "metadata.db"))
			}
		}

		// Backup maildir
		if cfg.Storage.MaildirPath != "" {
			if _, err := os.Stat(cfg.Storage.MaildirPath); err == nil {
				copyDir(cfg.Storage.MaildirPath, filepath.Join(tempDir, "maildir"))
			}
		}

		// Backup DKIM keys
		dkimPath := filepath.Join(cfg.Storage.DataDir, "dkim")
		if cfg.Storage.MaildirPath != "" {
			dkimPath = filepath.Join(filepath.Dir(cfg.Storage.MaildirPath), "dkim")
		}
		if _, err := os.Stat(dkimPath); err == nil {
			copyDir(dkimPath, filepath.Join(tempDir, "dkim"))
		}

		// Create archive
		if err := createTarGz(tempPath, tempDir); err != nil {
			return fmt.Errorf("failed to create archive: %w", err)
		}

		fi, _ := os.Stat(tempPath)
		fmt.Printf("  Backup created: %s\n\n", formatBytes(fi.Size()))

		// Transfer to remote server
		fmt.Println("Step 2: Transferring to remote server...")
		scpCmd := exec.Command("scp", tempPath, fmt.Sprintf("%s:%s/backup.tar.gz", remoteServer, remotePath))
		scpCmd.Stdout = os.Stdout
		scpCmd.Stderr = os.Stderr
		if err := scpCmd.Run(); err != nil {
			return fmt.Errorf("failed to transfer backup: %w", err)
		}
		fmt.Println("  Transfer complete")

		// Extract on remote server
		fmt.Println("Step 3: Extracting on remote server...")
		sshCmd := exec.Command("ssh", remoteServer,
			fmt.Sprintf("cd %s && tar -xzf backup.tar.gz && rm backup.tar.gz && echo 'Extraction complete'", remotePath))
		sshCmd.Stdout = os.Stdout
		sshCmd.Stderr = os.Stderr
		if err := sshCmd.Run(); err != nil {
			return fmt.Errorf("failed to extract on remote: %w", err)
		}

		fmt.Println("\n=== Export Complete ===")
		fmt.Printf("Data has been transferred to %s:%s\n", remoteServer, remotePath)
		fmt.Println("\nNext steps on the remote server:")
		fmt.Println("1. Update config.yaml with correct paths")
		fmt.Println("2. Run: mailserver migrate")
		fmt.Println("3. Run: systemctl start mailserver")

		return nil
	},
}

// Helper functions for backup/restore
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

func createTarGz(outputPath, sourceDir string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(tarWriter, file)
			return err
		}

		return nil
	})
}

func extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
			os.Chmod(targetPath, os.FileMode(header.Mode))
		}
	}

	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
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

// Setup commands
var forceSetup bool

var preflightCmd = &cobra.Command{
	Use:   "preflight",
	Short: "Check if server is ready for mail server setup",
	Long:  `Runs preflight checks to verify the server meets all requirements before installation.`,
	Run: func(cmd *cobra.Command, args []string) {
		results := setup.RunPreflightWithOptions(forceSetup)
		results.Print()
		if !results.Ready {
			os.Exit(1)
		}
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard for new installations",
	Long: `Guides you through setting up a new mail server installation step by step.

Use --force to skip non-critical checks (root, OS, systemd) for development/testing.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := setup.RunSetupWithOptions(forceSetup); err != nil {
			fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
			os.Exit(1)
		}
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check health of running mail server",
	Long:  `Runs health checks to diagnose issues with your mail server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		results := setup.RunDoctor(cfg)
		results.Print()
		if !results.Healthy {
			os.Exit(1)
		}
		return nil
	},
}

// Database migration commands
var migrateDBCmd = &cobra.Command{
	Use:   "migrate-db",
	Short: "Database migration tools",
	Long: `Tools for migrating data between databases.

Supports automatic migration from SQLite to PostgreSQL with:
- Automatic detection of source and target databases
- Data integrity verification
- Automatic backup before migration`,
}

var migrateTargetDSN string
var migrateAutoBackup bool

var migrateDBAutoCmd = &cobra.Command{
	Use:   "auto",
	Short: "Automatically migrate data to configured database",
	Long: `Automatically detect and migrate data from the source database to the target.

This command is safe to run on every startup - it will skip migration if:
- Target database already has data
- Source database is empty

The migration process:
1. Creates a backup of the source database
2. Copies all data to the target database
3. Verifies data integrity
4. Reports any issues

Example:
  mailserver migrate-db auto --target "postgres://user:pass@localhost/mail"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		fmt.Println("=== Automatic Database Migration ===")
		fmt.Println()

		// Open source database (SQLite)
		fmt.Printf("Opening source database: %s\n", cfg.Storage.DatabasePath)
		sourceDB, err := metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open source database: %w", err)
		}
		defer sourceDB.Close()

		// Determine target DSN
		targetDSN := migrateTargetDSN
		if targetDSN == "" {
			targetDSN = cfg.Database.DSN
		}
		if targetDSN == "" {
			return fmt.Errorf("target database not specified. Use --target flag or set database.dsn in config")
		}

		fmt.Printf("Opening target database: %s\n", maskDSN(targetDSN))

		// Open target database (PostgreSQL)
		targetDB, err := metadata.OpenPostgres(metadata.PostgresConfig{
			DSN:          targetDSN,
			MaxOpenConns: cfg.Database.MaxOpenConns,
			MaxIdleConns: cfg.Database.MaxIdleConns,
		})
		if err != nil {
			return fmt.Errorf("failed to open target database: %w", err)
		}
		defer targetDB.Close()

		// Run migrations on target first
		fmt.Println("Running schema migrations on target...")
		if err := targetDB.Migrate(context.Background()); err != nil {
			return fmt.Errorf("failed to migrate target schema: %w", err)
		}

		// Create backup if requested
		if migrateAutoBackup {
			fmt.Println()
			fmt.Print("Creating backup of source database... ")
			backupDir := filepath.Join(cfg.Storage.DataDir, "backups")
			backupPath, err := migration.BackupDatabase(cfg.Storage.DatabasePath, backupDir)
			if err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}
			fmt.Printf("saved to %s\n", backupPath)
		}

		// Create migrator and run
		fmt.Println()
		migrator := migration.NewAutoMigrator(sourceDB.RawDB(), targetDB.RawDB(), migration.DefaultLogger{})

		result, err := migrator.DetectAndMigrate(context.Background())
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		// Print results
		fmt.Println()
		fmt.Println("=== Migration Results ===")
		fmt.Printf("Status: %s\n", boolToStatus(result.Success))
		fmt.Printf("Duration: %v\n", result.Duration)
		fmt.Printf("Tables: %d\n", result.TablesCount)

		if len(result.RowsCopied) > 0 {
			fmt.Println()
			fmt.Println("Rows copied:")
			for table, count := range result.RowsCopied {
				if count > 0 {
					fmt.Printf("  %s: %d\n", table, count)
				}
			}
		}

		if len(result.Errors) > 0 {
			fmt.Println()
			fmt.Println("Errors:")
			for _, err := range result.Errors {
				fmt.Printf("  - %s\n", err)
			}
		}

		// Verify migration
		fmt.Println()
		fmt.Print("Verifying migration... ")
		verifyResult, err := migrator.VerifyMigration(context.Background())
		if err != nil {
			fmt.Printf("error: %v\n", err)
		} else if verifyResult.Success {
			fmt.Println("passed")
		} else {
			fmt.Println("MISMATCH DETECTED")
			for table, tv := range verifyResult.Tables {
				if !tv.Match {
					fmt.Printf("  %s: source=%d, target=%d\n", table, tv.SourceRows, tv.TargetRows)
				}
			}
		}

		if !result.Success {
			return fmt.Errorf("migration completed with errors")
		}

		fmt.Println()
		fmt.Println("Migration completed successfully!")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("1. Update config.yaml to use the new database:")
		fmt.Println("   database:")
		fmt.Println("     driver: postgres")
		fmt.Printf("     dsn: %s\n", maskDSN(targetDSN))
		fmt.Println("2. Restart the mail server")
		fmt.Println("3. Monitor logs for any issues")
		fmt.Println("4. Keep the SQLite backup for 30 days")

		return nil
	},
}

var migrateDBVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify migration integrity between source and target",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		// Open source database
		sourceDB, err := metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open source database: %w", err)
		}
		defer sourceDB.Close()

		// Determine target DSN
		targetDSN := migrateTargetDSN
		if targetDSN == "" {
			targetDSN = cfg.Database.DSN
		}
		if targetDSN == "" {
			return fmt.Errorf("target database not specified")
		}

		// Open target database
		targetDB, err := metadata.OpenPostgres(metadata.PostgresConfig{
			DSN: targetDSN,
		})
		if err != nil {
			return fmt.Errorf("failed to open target database: %w", err)
		}
		defer targetDB.Close()

		// Verify
		migrator := migration.NewAutoMigrator(sourceDB.RawDB(), targetDB.RawDB(), nil)
		result, err := migrator.VerifyMigration(context.Background())
		if err != nil {
			return err
		}

		fmt.Println("=== Migration Verification ===")
		fmt.Printf("%-20s %10s %10s %8s\n", "TABLE", "SOURCE", "TARGET", "STATUS")
		fmt.Println("---------------------------------------------------")

		for table, tv := range result.Tables {
			status := "OK"
			if !tv.Match {
				status = "MISMATCH"
			}
			if tv.SourceRows == -1 {
				status = "MISSING"
			}
			fmt.Printf("%-20s %10d %10d %8s\n", table, tv.SourceRows, tv.TargetRows, status)
		}

		fmt.Println()
		if result.Success {
			fmt.Println("Verification: PASSED")
		} else {
			fmt.Println("Verification: FAILED")
			return fmt.Errorf("verification failed")
		}

		return nil
	},
}

var migrateDBBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create a backup of the source database",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		backupDir := filepath.Join(cfg.Storage.DataDir, "backups")
		backupPath, err := migration.BackupDatabase(cfg.Storage.DatabasePath, backupDir)
		if err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}

		fmt.Printf("Backup created: %s\n", backupPath)
		return nil
	},
}

func maskDSN(dsn string) string {
	// Mask password in DSN for display
	// postgres://user:password@host/db -> postgres://user:****@host/db
	if strings.Contains(dsn, "://") {
		parts := strings.SplitN(dsn, "://", 2)
		if len(parts) == 2 {
			rest := parts[1]
			if atIdx := strings.Index(rest, "@"); atIdx > 0 {
				userPass := rest[:atIdx]
				hostDB := rest[atIdx:]
				if colonIdx := strings.Index(userPass, ":"); colonIdx > 0 {
					user := userPass[:colonIdx]
					return parts[0] + "://" + user + ":****" + hostDB
				}
			}
		}
	}
	return dsn
}

func boolToStatus(b bool) string {
	if b {
		return "SUCCESS"
	}
	return "FAILED"
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "config.yaml", "config file path")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(versionCmd)

	// Domain commands
	domainAddCmd.Flags().BoolVar(&domainGenerateDKIM, "generate-dkim", false, "Generate DKIM key for the domain")
	domainAddCmd.Flags().IntVar(&domainDKIMBits, "dkim-bits", 2048, "DKIM key size in bits")
	domainAddCmd.Flags().StringVar(&domainDKIMSelector, "dkim-selector", "mail", "DKIM selector")
	domainAddCmd.Flags().StringVar(&domainDKIMStorage, "dkim-storage", "file", "DKIM key storage (file, database, hybrid)")
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

	// DKIM commands
	dkimGenerateCmd.Flags().IntVarP(&dkimBits, "bits", "b", 2048, "Key size in bits (2048 or 4096)")
	dkimGenerateCmd.Flags().StringVarP(&dkimSelector, "selector", "s", "mail", "DKIM selector")
	dkimGenerateCmd.Flags().StringVar(&dkimStorage, "storage", "file", "Storage type (file, database, hybrid)")
	dkimGenerateCmd.Flags().BoolVarP(&dkimForce, "force", "f", false, "Overwrite existing key")

	dkimShowCmd.Flags().StringVarP(&dkimFormat, "format", "f", "dns", "Output format (dns, bind, raw)")

	dkimRotateCmd.Flags().IntVarP(&dkimBits, "bits", "b", 2048, "Key size in bits (2048 or 4096)")

	dkimAutoRotateCmd.Flags().IntVar(&dkimAutoRotateDays, "days", 90, "Rotate keys older than this many days")

	dkimCmd.AddCommand(dkimGenerateCmd)
	dkimCmd.AddCommand(dkimShowCmd)
	dkimCmd.AddCommand(dkimRotateCmd)
	dkimCmd.AddCommand(dkimListCmd)
	dkimCmd.AddCommand(dkimAutoRotateCmd)
	rootCmd.AddCommand(dkimCmd)

	// Backup/Restore/Export commands
	restoreCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")
	exportCmd.Flags().String("remote-path", "/var/mailserver", "Remote directory path")
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(exportCmd)

	// Setup commands
	preflightCmd.Flags().BoolVarP(&forceSetup, "force", "f", false, "Skip non-critical checks (root, OS, systemd)")
	setupCmd.Flags().BoolVarP(&forceSetup, "force", "f", false, "Skip non-critical checks (root, OS, systemd)")
	rootCmd.AddCommand(preflightCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(doctorCmd)

	// Database migration commands
	migrateDBAutoCmd.Flags().StringVar(&migrateTargetDSN, "target", "", "Target database DSN (e.g., postgres://user:pass@localhost/mail)")
	migrateDBAutoCmd.Flags().BoolVar(&migrateAutoBackup, "backup", true, "Create backup before migration")
	migrateDBVerifyCmd.Flags().StringVar(&migrateTargetDSN, "target", "", "Target database DSN")
	migrateDBCmd.AddCommand(migrateDBAutoCmd)
	migrateDBCmd.AddCommand(migrateDBVerifyCmd)
	migrateDBCmd.AddCommand(migrateDBBackupCmd)
	rootCmd.AddCommand(migrateDBCmd)
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
