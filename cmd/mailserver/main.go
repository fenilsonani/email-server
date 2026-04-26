package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
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
	"github.com/fenilsonani/email-server/internal/autodiscover"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/dav"
	"github.com/fenilsonani/email-server/internal/dns"
	"github.com/fenilsonani/email-server/internal/doctor"
	"github.com/fenilsonani/email-server/internal/features"
	"github.com/fenilsonani/email-server/internal/health"
	imapserver "github.com/fenilsonani/email-server/internal/imap"
	"github.com/fenilsonani/email-server/internal/lists"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/metrics"
	"github.com/fenilsonani/email-server/internal/migration"
	"github.com/fenilsonani/email-server/internal/org"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/recovery"
	"github.com/fenilsonani/email-server/internal/search"
	searchbleve "github.com/fenilsonani/email-server/internal/search/bleve"
	"github.com/fenilsonani/email-server/internal/search/indexer"
	searchpg "github.com/fenilsonani/email-server/internal/search/postgres"
	searchsqlite "github.com/fenilsonani/email-server/internal/search/sqlite"
	"github.com/fenilsonani/email-server/internal/security"
	"github.com/fenilsonani/email-server/internal/setup"
	"github.com/fenilsonani/email-server/internal/sieve"
	smtpserver "github.com/fenilsonani/email-server/internal/smtp"
	"github.com/fenilsonani/email-server/internal/smtp/delivery"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
	"github.com/fenilsonani/email-server/internal/storage/metadata"
	"github.com/fenilsonani/email-server/internal/tracing"
	"github.com/fenilsonani/email-server/internal/tuning"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
	db      metadata.Store
)

// dbUserStore implements indexer.UserStore using a raw SQL database connection.
type dbUserStore struct{ db *sql.DB }

func (s *dbUserStore) ListAllUsers(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM users WHERE is_active = TRUE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

const mailserverVersion = "0.1.0"

var rootCmd = &cobra.Command{
	Use:     "mailserver",
	Version: mailserverVersion,
	Short:   "Personal email server with IMAP, SMTP, CalDAV, and CardDAV",
	Long: `A personal email server supporting:
- IMAP with IDLE for Apple Mail sync
- SMTP for sending and receiving email
- CalDAV for calendar sync
- CardDAV for contacts sync
- Multiple domains with DKIM signing`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for help/version (--version handled by cobra before RunE,
		// but PreRunE still fires)
		if cmd.Name() == "help" || cmd.Name() == "version" {
			return nil
		}
		if v, _ := cmd.Flags().GetBool("version"); v {
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
			searchService    *search.SearchService
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

			// 3.5. Stop search service (flush pending indexes)
			if resources.searchService != nil {
				if resources.logger != nil {
					resources.logger.Info("Shutting down search service")
				}
				if err := resources.searchService.Close(); err != nil {
					if resources.logger != nil {
						resources.logger.Error("Search service shutdown error", "error", err.Error())
					} else {
						fmt.Fprintf(os.Stderr, "Search service shutdown error: %v\n", err)
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

		// Ensure all existing users have their required mailboxes (migration for new mailbox types)
		initCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := store.EnsureUserMailboxes(initCtx, logger); err != nil {
			cancel()
			cleanup()
			return fmt.Errorf("failed to ensure user mailboxes: %w", err)
		}
		cancel()
		logger.Info("User mailboxes initialized")

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

		// Initialize observability components
		tracer := tracing.NewTracer(true, logger)
		domainStats := metrics.NewDomainStats(time.Hour)

		// Initialize deduplication tracker using Redis
		dedupTracker := queue.NewDeliveryTracker(redisQueue.Client(), cfg.Queue.Prefix, 7*24*time.Hour)

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
			MTASTSEnabled:  cfg.Delivery.MTASTSEnabled,
			DANEEnabled:    cfg.Delivery.DANEEnabled,
			DANEDNSServer:  cfg.Delivery.DANEDNSServer,
		}, redisQueue, dkimPool, logger, db.RawDB(),
			delivery.WithTracer(tracer),
			delivery.WithDomainStats(domainStats),
			delivery.WithDedupTracker(dedupTracker),
		)
		resources.deliveryEngine = deliveryEngine
		deliveryEngine.Start()
		logger.Info("Delivery engine started",
			"workers", cfg.Delivery.Workers,
			"tracing", true,
			"domain_metrics", true,
			"deduplication", true,
		)

		// Create IMAP server with config
		imapAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.IMAPPort)
		imapsAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.IMAPSPort)

		// Parse IMAP-specific config
		imapConfig := imapserver.DefaultIMAPConfig()
		if cfg.Server.IMAP.IdleKeepaliveInterval != "" {
			if d, err := time.ParseDuration(cfg.Server.IMAP.IdleKeepaliveInterval); err == nil {
				imapConfig.IdleKeepaliveInterval = d
			}
		}
		if cfg.Server.IMAP.TCPKeepalivePeriod != "" {
			if d, err := time.ParseDuration(cfg.Server.IMAP.TCPKeepalivePeriod); err == nil {
				imapConfig.TCPKeepalivePeriod = d
			}
		}
		if cfg.Server.IMAP.MaxConnections > 0 {
			imapConfig.MaxConnections = cfg.Server.IMAP.MaxConnections
		}
		if cfg.Server.IMAP.MaxConnectionsPerIP > 0 {
			imapConfig.MaxConnectionsPerIP = cfg.Server.IMAP.MaxConnectionsPerIP
		}

		imapSrv := imapserver.NewServer(authenticator, store, imapAddr, imapsAddr, tlsManager.MailTLSConfig(), imapConfig)
		resources.imapSrv = imapSrv

		// Initialize full-text search if enabled
		if cfg.Search.Enabled {
			searchCfg := &search.Config{
				Enabled:          cfg.Search.Enabled,
				Engine:           search.EngineType(cfg.Search.Engine),
				IndexPath:        cfg.Search.IndexPath,
				Realtime:         cfg.Search.Realtime,
				BatchSize:        cfg.Search.BatchSize,
				FlushInterval:    cfg.Search.FlushInterval,
				Timeout:          cfg.Search.Timeout,
				FuzzyEnabled:     cfg.Search.FuzzyEnabled,
				FuzzyDistance:    cfg.Search.FuzzyDistance,
				HighlightEnabled: cfg.Search.HighlightEnabled,
				MaxResults:       cfg.Search.MaxResults,
				Workers:          cfg.Search.Workers,
			}

			// Apply defaults for missing values
			if searchCfg.BatchSize == 0 {
				searchCfg.BatchSize = 100
			}
			if searchCfg.Workers == 0 {
				searchCfg.Workers = 2
			}
			if searchCfg.FlushInterval == "" {
				searchCfg.FlushInterval = "100ms"
			}
			if searchCfg.Timeout == "" {
				searchCfg.Timeout = "5s"
			}
			if searchCfg.MaxResults == 0 {
				searchCfg.MaxResults = 1000
			}

			// Create search engine based on configured engine type
			var searchEngine search.SearchEngine
			switch searchCfg.Engine {
			case search.EngineSQLite:
				searchEngine, err = searchsqlite.NewEngine(db.RawDB(), searchCfg)
			case search.EnginePostgres:
				searchEngine, err = searchpg.NewEngine(db.RawDB(), searchCfg)
			default: // "bleve", "auto", or empty
				searchEngine, err = searchbleve.NewEngine(searchCfg)
			}
			if err != nil {
				cleanup()
				return fmt.Errorf("failed to initialize search engine: %w", err)
			}

			// Create indexer with user store for ReindexAll support
			userStore := &dbUserStore{db: db.RawDB()}
			searchIndexer := indexer.NewIndexer(searchEngine, store, userStore, searchCfg)

			// Wire up storage hooks for real-time indexing
			if searchCfg.Realtime {
				maildir.SetSearchAppendHook(func(ctx context.Context, mailboxID int64, uid uint32) {
					if err := searchIndexer.IndexMessage(ctx, mailboxID, uid); err != nil {
						logger.Warn("Failed to index message", "mailbox", mailboxID, "uid", uid, "error", err.Error())
					}
				})
				maildir.SetSearchExpungeHook(func(ctx context.Context, mailboxID int64, uid uint32) {
					if err := searchIndexer.DeleteMessage(ctx, mailboxID, uid); err != nil {
						logger.Warn("Failed to delete message from index", "mailbox", mailboxID, "uid", uid, "error", err.Error())
					}
				})
			}

			// Start the indexer
			if err := searchIndexer.Start(context.Background()); err != nil {
				cleanup()
				return fmt.Errorf("failed to start search indexer: %w", err)
			}

			// Set search engine on IMAP server
			imapSrv.SetSearchEngine(searchEngine)

			// Store for cleanup
			resources.searchService = search.NewSearchService(searchEngine, searchIndexer)
			logger.Info("Full-text search enabled",
				"engine", searchEngine.Name(),
				"index_path", searchCfg.IndexPath,
				"realtime", searchCfg.Realtime,
				"workers", searchCfg.Workers,
			)
		}

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

		// Initialize mailing lists manager
		listsStore := lists.NewStore(db.RawDB())
		archivePath := filepath.Join(cfg.Storage.DataDir, "archives")
		moderationPath := filepath.Join(cfg.Storage.DataDir, "moderation")
		listsManager := lists.NewManager(listsStore, archivePath, moderationPath, logger)
		listsCommandHandler := lists.NewCommandHandler(listsStore, listsManager, cfg.Server.Hostname, logger)
		smtpBackend.SetListsManager(listsManager, listsCommandHandler)
		logger.Info("Mailing lists enabled")

		// Initialize features store for SMTP backend (Screener, Aliases, etc.)
		featuresStore := features.NewStore(db.RawDB())
		smtpBackend.SetFeaturesStore(featuresStore)

		smtpSrv := smtpserver.NewServer(smtpBackend, cfg, tlsManager.MailTLSConfig())
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
			if err := imapSrv.ListenAndServeTLS(tlsManager.MailTLSConfig()); err != nil {
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
					if err := davSrv.Start(davAddr, tlsManager.HTTPTLSConfig()); err != nil {
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
				// Use the already-initialized features store
				adminSrv.SetFeaturesStore(featuresStore)
				// Set lists store for mailing list management
				adminSrv.SetListsStore(listsStore)
				// Set org store for multi-organization management
				orgStore := org.NewStore(db.RawDB())
				adminSrv.SetOrgStore(orgStore)

				// Set delivery engine for email resend functionality
				if resources.deliveryEngine != nil {
					queuePath := filepath.Join(cfg.Storage.DataDir, "queue")
					adminSrv.SetDeliveryEngine(resources.deliveryEngine, queuePath)
					logger.Info("Admin server configured with delivery engine for resend")
				}

				// Start feature scheduler for scheduled sends, snooze wake-ups, undo send
				featureScheduler := features.NewScheduler(featuresStore, logger)

				// Configure email sender for scheduled sends using delivery queue
				if resources.deliveryEngine != nil {
					queuePath := filepath.Join(cfg.Storage.DataDir, "queue")
					emailSender := features.NewQueueEmailSender(resources.deliveryEngine, queuePath)
					featureScheduler.SetEmailSender(emailSender)
					logger.Info("Feature scheduler configured with delivery queue")
				} else {
					logger.Warn("Delivery engine not available, scheduled sends disabled")
				}

				// Configure message mover for snooze wake-ups
				featureScheduler.SetMessageMover(featuresStore)
				logger.Info("Feature scheduler configured with message mover for snooze")

				featureScheduler.Start()
				resources.featureScheduler = featureScheduler
				logger.Info("Feature scheduler started")

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

		// Setup signal handling for graceful shutdown and certificate reload
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

		// Handle signals in a loop to support hot reload
		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				// Reload certificates without shutdown
				logger.Info("Received SIGHUP signal, reloading certificates")
				if err := tlsManager.ReloadCertificates(); err != nil {
					logger.Error("Failed to reload certificates", "error", err)
				} else {
					logger.Info("Certificates reloaded successfully")
				}

			case syscall.SIGINT, syscall.SIGTERM:
				// Shutdown
				logger.Info("Received shutdown signal", "signal", sig.String())
				fmt.Printf("\nReceived signal %s, shutting down...\n", sig)

				// Perform graceful shutdown
				cleanup()

				logger.Info("Server stopped")
				return nil
			}
		}

		// This should never be reached unless the signal channel is closed
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

var (
	userAddPasswordHash string
	userAddAdmin        bool
)

var userAddCmd = &cobra.Command{
	Use:   "add <email> [password]",
	Short: "Add a new user",
	Long: `Add a new user.

Password may be supplied as the second positional argument, or as a pre-hashed
bcrypt string via --password-hash (preferred for scripted use, since flags are
less likely to leak via process listings than positional args). Exactly one of
the two must be provided.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]

		var hash string
		switch {
		case userAddPasswordHash != "" && len(args) == 2:
			return fmt.Errorf("provide either a positional password or --password-hash, not both")
		case userAddPasswordHash != "":
			hash = userAddPasswordHash
		case len(args) == 2:
			h, err := auth.HashPassword(args[1])
			if err != nil {
				return fmt.Errorf("failed to hash password: %w", err)
			}
			hash = h
		default:
			return fmt.Errorf("password required: pass it as the second argument or via --password-hash")
		}

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

		// Insert user
		result, err := db.ExecContext(context.Background(),
			"INSERT INTO users (domain_id, username, password_hash, is_admin) VALUES (?, ?, ?, ?)",
			domainID, username, hash, userAddAdmin,
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

		// Create default calendar and addressbook for CalDAV/CardDAV
		davCreated := true
		caldavBackend, caldavErr := dav.NewCalDAVBackend(db.RawDB())
		if caldavErr != nil {
			fmt.Printf("Warning: failed to init CalDAV backend: %v\n", caldavErr)
			davCreated = false
		} else {
			if _, err := caldavBackend.CreateCalendar(context.Background(), userID, "Calendar", "Default calendar"); err != nil {
				fmt.Printf("Warning: failed to create default calendar: %v\n", err)
				davCreated = false
			}
		}
		carddavBackend, carddavErr := dav.NewCardDAVBackend(db.RawDB())
		if carddavErr != nil {
			fmt.Printf("Warning: failed to init CardDAV backend: %v\n", carddavErr)
			davCreated = false
		} else {
			if _, err := carddavBackend.CreateAddressBook(context.Background(), userID, "Contacts", "Default address book"); err != nil {
				fmt.Printf("Warning: failed to create default address book: %v\n", err)
				davCreated = false
			}
		}

		fmt.Printf("User '%s' added with ID %d\n", email, userID)
		fmt.Println("Default mailboxes created: INBOX, Drafts, Sent, Trash, Junk, Archive")
		if davCreated {
			fmt.Println("Default calendar and address book created")
		}
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

var userDeleteForce bool

var userDeleteCmd = &cobra.Command{
	Use:   "delete <email>",
	Short: "Delete a user",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]

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

		if !userDeleteForce {
			fmt.Printf("Are you sure you want to delete user '%s'? This cannot be undone. [y/N] ", email)
			var confirm string
			_, _ = fmt.Scanln(&confirm)
			if confirm != "y" && confirm != "Y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		result, err := db.ExecContext(context.Background(), `
			DELETE FROM users WHERE username = ? AND domain_id = (SELECT id FROM domains WHERE name = ?)
		`, username, domain)
		if err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("user not found: %s", email)
		}

		fmt.Printf("User '%s' deleted\n", email)
		return nil
	},
}

var userEnableCmd = &cobra.Command{
	Use:   "enable <email>",
	Short: "Enable a user account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]

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

		result, err := db.ExecContext(context.Background(), `
			UPDATE users SET is_active = TRUE WHERE username = ? AND domain_id = (SELECT id FROM domains WHERE name = ?)
		`, username, domain)
		if err != nil {
			return fmt.Errorf("failed to enable user: %w", err)
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("user not found: %s", email)
		}

		fmt.Printf("User '%s' enabled\n", email)
		return nil
	},
}

var userDisableCmd = &cobra.Command{
	Use:   "disable <email>",
	Short: "Disable a user account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]

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

		result, err := db.ExecContext(context.Background(), `
			UPDATE users SET is_active = FALSE WHERE username = ? AND domain_id = (SELECT id FROM domains WHERE name = ?)
		`, username, domain)
		if err != nil {
			return fmt.Errorf("failed to disable user: %w", err)
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("user not found: %s", email)
		}

		fmt.Printf("User '%s' disabled\n", email)
		return nil
	},
}

var setRoleDomain string

var userSetRoleCmd = &cobra.Command{
	Use:   "set-role <email> <role>",
	Short: "Assign a role to a user (super_admin, domain_admin, support, none)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]
		roleName := args[1]

		validRoles := map[string]bool{
			"super_admin":  true,
			"domain_admin": true,
			"support":      true,
			"none":         true,
		}
		if !validRoles[roleName] {
			return fmt.Errorf("invalid role: %s (valid: super_admin, domain_admin, support, none)", roleName)
		}

		if roleName == "domain_admin" && setRoleDomain == "" {
			return fmt.Errorf("--domain flag is required for domain_admin role")
		}

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

		parts := splitEmail(email)
		if len(parts) != 2 {
			return fmt.Errorf("invalid email format: %s", email)
		}
		username, domain := parts[0], parts[1]

		// Get user ID
		var userID int64
		err = db.QueryRowContext(context.Background(), `
			SELECT u.id FROM users u JOIN domains d ON u.domain_id = d.id
			WHERE u.username = ? AND d.name = ?
		`, username, domain).Scan(&userID)
		if err != nil {
			return fmt.Errorf("user not found: %s", email)
		}

		// Validate inputs before making changes
		if roleName != "none" {
			// Verify role exists before touching anything
			var roleID int64
			err = db.QueryRowContext(context.Background(), "SELECT id FROM roles WHERE name = ?", roleName).Scan(&roleID)
			if err != nil {
				return fmt.Errorf("role '%s' not found in database (run migrations first)", roleName)
			}

			if roleName == "domain_admin" {
				var domainID int64
				err = db.QueryRowContext(context.Background(), "SELECT id FROM domains WHERE name = ?", setRoleDomain).Scan(&domainID)
				if err != nil {
					return fmt.Errorf("domain not found: %s", setRoleDomain)
				}
			}
		}

		// All validation passed — apply changes in a transaction
		tx, txErr := db.BeginTx(context.Background(), nil)
		if txErr != nil {
			return fmt.Errorf("failed to begin transaction: %w", txErr)
		}
		defer func() {
			if txErr != nil {
				_ = tx.Rollback()
			}
		}()

		// Clear existing roles
		if _, txErr = tx.ExecContext(context.Background(), "DELETE FROM user_roles WHERE user_id = ?", userID); txErr != nil {
			return fmt.Errorf("failed to clear roles: %w", txErr)
		}

		if roleName == "none" {
			// Remove admin flag
			if _, txErr = tx.ExecContext(context.Background(), "UPDATE users SET is_admin = FALSE WHERE id = ?", userID); txErr != nil {
				return fmt.Errorf("failed to update admin flag: %w", txErr)
			}
			if txErr = tx.Commit(); txErr != nil {
				return fmt.Errorf("failed to commit: %w", txErr)
			}
			fmt.Printf("Role removed from '%s'\n", email)
			return nil
		}

		// Get role ID
		var roleID int64
		txErr = tx.QueryRowContext(context.Background(), "SELECT id FROM roles WHERE name = ?", roleName).Scan(&roleID)
		if txErr != nil {
			return fmt.Errorf("role lookup failed: %w", txErr)
		}

		if roleName == "domain_admin" {
			var domainID int64
			txErr = tx.QueryRowContext(context.Background(), "SELECT id FROM domains WHERE name = ?", setRoleDomain).Scan(&domainID)
			if txErr != nil {
				return fmt.Errorf("domain lookup failed: %w", txErr)
			}
			_, txErr = tx.ExecContext(context.Background(),
				"INSERT INTO user_roles (user_id, role_id, domain_id) VALUES (?, ?, ?)",
				userID, roleID, domainID)
		} else {
			_, txErr = tx.ExecContext(context.Background(),
				"INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
				userID, roleID)
		}
		if txErr != nil {
			return fmt.Errorf("failed to assign role: %w", txErr)
		}

		// Sync is_admin flag for backward compat
		_, _ = tx.ExecContext(context.Background(), "UPDATE users SET is_admin = TRUE WHERE id = ?", userID)

		if txErr = tx.Commit(); txErr != nil {
			return fmt.Errorf("failed to commit: %w", txErr)
		}

		fmt.Printf("Role '%s' assigned to '%s'\n", roleName, email)
		if roleName == "domain_admin" {
			fmt.Printf("  Scoped to domain: %s\n", setRoleDomain)
		}
		return nil
	},
}

// Domain management: delete, enable, disable

var domainDeleteForce bool

var domainDeleteCmd = &cobra.Command{
	Use:   "delete <domain>",
	Short: "Delete a domain and all its users",
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

		// Count users in this domain
		var userCount int
		_ = db.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM users WHERE domain_id = (SELECT id FROM domains WHERE name = ?)",
			domainName).Scan(&userCount)

		if !domainDeleteForce {
			fmt.Printf("Are you sure you want to delete domain '%s'? (%d users will be deleted) [y/N] ", domainName, userCount)
			var confirm string
			_, _ = fmt.Scanln(&confirm)
			if confirm != "y" && confirm != "Y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		// Delete users first (cascade may not be enabled)
		_, _ = db.ExecContext(context.Background(),
			"DELETE FROM users WHERE domain_id = (SELECT id FROM domains WHERE name = ?)", domainName)

		result, err := db.ExecContext(context.Background(), "DELETE FROM domains WHERE name = ?", domainName)
		if err != nil {
			return fmt.Errorf("failed to delete domain: %w", err)
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("domain not found: %s", domainName)
		}

		fmt.Printf("Domain '%s' deleted (%d users removed)\n", domainName, userCount)
		return nil
	},
}

var domainEnableCmd = &cobra.Command{
	Use:   "enable <domain>",
	Short: "Enable a domain",
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

		result, err := db.ExecContext(context.Background(),
			"UPDATE domains SET is_active = TRUE WHERE name = ?", domainName)
		if err != nil {
			return fmt.Errorf("failed to enable domain: %w", err)
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("domain not found: %s", domainName)
		}

		fmt.Printf("Domain '%s' enabled\n", domainName)
		return nil
	},
}

var domainDisableCmd = &cobra.Command{
	Use:   "disable <domain>",
	Short: "Disable a domain",
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

		result, err := db.ExecContext(context.Background(),
			"UPDATE domains SET is_active = FALSE WHERE name = ?", domainName)
		if err != nil {
			return fmt.Errorf("failed to disable domain: %w", err)
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("domain not found: %s", domainName)
		}

		fmt.Printf("Domain '%s' disabled\n", domainName)
		return nil
	},
}

// Database management commands

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup [path]",
	Short: "Create a consistent database backup using VACUUM INTO",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		outputPath := "mail-backup.db"
		if len(args) > 0 {
			outputPath = args[0]
		}

		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		fmt.Printf("Creating backup: %s\n", outputPath)
		_, err = db.ExecContext(context.Background(), "VACUUM INTO ?", outputPath)
		if err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}

		// Get file size
		info, _ := os.Stat(outputPath)
		if info != nil {
			fmt.Printf("Backup complete: %s (%.2f MB)\n", outputPath, float64(info.Size())/1024/1024)
		} else {
			fmt.Println("Backup complete")
		}
		return nil
	},
}

var dbRestoreForce bool

var dbRestoreCmd = &cobra.Command{
	Use:   "restore <path>",
	Short: "Restore database from backup (requires service stopped)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupPath := args[0]

		// Check backup exists
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			return fmt.Errorf("backup file not found: %s", backupPath)
		}

		dbPath := cfg.Storage.DatabasePath
		if dbPath == "" {
			return fmt.Errorf("database path not configured")
		}

		if !dbRestoreForce {
			fmt.Printf("This will REPLACE the current database with '%s'. Are you sure? [y/N] ", backupPath)
			var confirm string
			_, _ = fmt.Scanln(&confirm)
			if confirm != "y" && confirm != "Y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		// Copy backup file to database path
		src, err := os.Open(backupPath) // #nosec G304 -- path from validated CLI flag
		if err != nil {
			return fmt.Errorf("failed to open backup: %w", err)
		}
		defer src.Close()

		dst, err := os.Create(dbPath) // #nosec G304 -- path from server config
		if err != nil {
			return fmt.Errorf("failed to create database file: %w", err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("failed to copy backup: %w", err)
		}

		// Remove WAL/SHM files if they exist
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")

		fmt.Printf("Database restored from '%s'\n", backupPath)
		fmt.Println("Restart the mail server to use the restored database.")
		return nil
	},
}

var dbShellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open a read-only SQLite shell",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := cfg.Storage.DatabasePath
		if dbPath == "" {
			return fmt.Errorf("database path not configured")
		}

		// Check if sqlite3 is available
		sqlite3Path, err := exec.LookPath("sqlite3")
		if err != nil {
			return fmt.Errorf("sqlite3 not found in PATH - install it with: apt install sqlite3")
		}

		fmt.Printf("Opening read-only shell for: %s\n", dbPath)
		fmt.Println("Type .quit to exit")

		// Open in read-only mode
		shellCmd := exec.Command(sqlite3Path, "file:"+dbPath+"?mode=ro") // #nosec G204 -- sqlite3Path from exec.LookPath
		shellCmd.Stdin = os.Stdin
		shellCmd.Stdout = os.Stdout
		shellCmd.Stderr = os.Stderr
		return shellCmd.Run()
	},
}

var dbStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show database file size and row counts per table",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		dbPath := cfg.Storage.DatabasePath
		if dbPath == "" {
			return fmt.Errorf("database path not configured")
		}

		// Show file sizes
		fmt.Println("=== Database Files ===")
		for _, suffix := range []string{"", "-wal", "-shm"} {
			path := dbPath + suffix
			info, err := os.Stat(path)
			if err == nil {
				fmt.Printf("  %-30s  %.2f MB\n", filepath.Base(path), float64(info.Size())/1024/1024)
			}
		}
		fmt.Println()

		// Open database
		var err error
		db, err = metadata.Open(cfg.Storage.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		// Get table row counts
		fmt.Println("=== Table Row Counts ===")
		rows, err := db.QueryContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if err != nil {
			return fmt.Errorf("failed to query tables: %w", err)
		}
		defer rows.Close()

		var tables []string
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				tables = append(tables, name)
			}
		}

		for _, table := range tables {
			var count int64
			_ = db.QueryRowContext(context.Background(),
				fmt.Sprintf("SELECT COUNT(*) FROM \"%s\"", table)).Scan(&count)
			fmt.Printf("  %-30s  %d rows\n", table, count)
		}

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

		if err := validateRemoteServer(remoteServer); err != nil {
			return err
		}
		remotePath, err := validateRemotePath(remotePath)
		if err != nil {
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
		scpCmd := exec.Command("scp", tempPath, fmt.Sprintf("%s:%s/backup.tar.gz", remoteServer, remotePath)) // #nosec G204 -- remote host/path validated and no shell is used
		scpCmd.Stdout = os.Stdout
		scpCmd.Stderr = os.Stderr
		if err := scpCmd.Run(); err != nil {
			return fmt.Errorf("failed to transfer backup: %w", err)
		}
		fmt.Println("  Transfer complete")

		// Extract on remote server
		fmt.Println("Step 3: Extracting on remote server...")
		remoteCommand := fmt.Sprintf(
			"cd %s && tar -xzf backup.tar.gz && rm backup.tar.gz && echo 'Extraction complete'",
			shellQuote(remotePath),
		)
		sshCmd := exec.Command("ssh", remoteServer, remoteCommand) // #nosec G204 -- remote host/path validated and shell arguments quoted
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

	if err := os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
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

		if header.Size < 0 {
			return fmt.Errorf("invalid archive entry size for %q", header.Name)
		}

		targetPath, err := safeExtractPath(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			mode, err := safeTarFileMode(header.Mode)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(targetPath, mode); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0750); err != nil {
				return err
			}
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(outFile, tarReader, header.Size); err != nil {
				outFile.Close()
				return err
			}
			if err := outFile.Close(); err != nil {
				return err
			}
			mode, err := safeTarFileMode(header.Mode)
			if err != nil {
				return err
			}
			safeMode := mode.Perm()
			if safeMode == 0 {
				safeMode = 0o600
			}
			if err := os.Chmod(targetPath, safeMode); err != nil {
				return err
			}
		}
	}

	return nil
}

func safeExtractPath(destDir, headerName string) (string, error) {
	cleanName := filepath.Clean(headerName)
	if cleanName == "." || cleanName == "" {
		return "", fmt.Errorf("invalid archive entry path %q", headerName)
	}
	if filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("archive entry %q uses absolute path", headerName)
	}

	destRoot, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(destRoot, cleanName)
	relPath, err := filepath.Rel(destRoot, targetPath)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes destination", headerName)
	}

	return targetPath, nil
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
		fmt.Printf("mailserver v%s\n", mailserverVersion)
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

// Doctor command flags
var (
	doctorFormat   string
	doctorVerbose  bool
	doctorCategory []string
	doctorDryRun   bool
	doctorYes      bool
	doctorNoColor  bool
	doctorFixID    string
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose server health and configuration",
	Long: `Run comprehensive health checks on your mail server.

Examples:
  mailserver doctor              # Run all checks
  mailserver doctor --format json  # JSON output
  mailserver doctor check --category dns  # Check DNS only
  mailserver doctor fix --dry-run  # Preview fixes
  mailserver doctor fix --yes      # Apply all fixes`,
	RunE: runDoctor,
}

var doctorCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Run specific health checks",
	Long: `Run health checks for specific categories.

Categories:
  infrastructure (infra)  - Database, disk, memory
  network (net)           - Ports, connectivity
  security (sec)          - TLS, DKIM
  dns                     - MX, SPF, DMARC
  config                  - Configuration validation
  queue                   - Message queue health`,
	RunE: runDoctorCheck,
}

var doctorFixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Auto-fix detected issues",
	Long: `Automatically fix issues detected by the doctor.

Use --dry-run to preview what would be fixed without making changes.
Use --fix to apply a specific fix by ID.
Use --yes to apply all fixable issues without confirmation.`,
	RunE: runDoctorFix,
}

var doctorReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate detailed diagnostic report",
	Long:  `Generate a comprehensive report including health checks, config vs reality comparison, and recommendations.`,
	RunE:  runDoctorReport,
}

var doctorCompareCmd = &cobra.Command{
	Use:   "compare",
	Short: "Compare configuration to actual state",
	Long:  `Compare configured values against actual runtime state to identify mismatches.`,
	RunE:  runDoctorCompare,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// Initialize queue connection if possible (for queue checks)
	var q *queue.RedisQueue
	if cfg.Queue.RedisURL != "" {
		retryMaxAge, _ := time.ParseDuration(cfg.Queue.RetryMaxAge)
		if retryMaxAge == 0 {
			retryMaxAge = 7 * 24 * time.Hour
		}
		dialTimeout, _ := time.ParseDuration(cfg.Queue.DialTimeout)
		readTimeout, _ := time.ParseDuration(cfg.Queue.ReadTimeout)
		writeTimeout, _ := time.ParseDuration(cfg.Queue.WriteTimeout)
		q, _ = queue.NewRedisQueue(queue.Config{
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
		if q != nil {
			defer q.Close()
		}
	}

	d := doctor.New(cfg, q)
	results := d.Run(context.Background())

	doctor.PrintResults(results, doctorFormat, doctorVerbose, doctorNoColor)

	if !results.Healthy {
		os.Exit(1)
	}
	return nil
}

func runDoctorCheck(cmd *cobra.Command, args []string) error {
	// Initialize queue connection if possible
	var q *queue.RedisQueue
	if cfg.Queue.RedisURL != "" {
		retryMaxAge, _ := time.ParseDuration(cfg.Queue.RetryMaxAge)
		if retryMaxAge == 0 {
			retryMaxAge = 7 * 24 * time.Hour
		}
		dialTimeout, _ := time.ParseDuration(cfg.Queue.DialTimeout)
		readTimeout, _ := time.ParseDuration(cfg.Queue.ReadTimeout)
		writeTimeout, _ := time.ParseDuration(cfg.Queue.WriteTimeout)
		q, _ = queue.NewRedisQueue(queue.Config{
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
		if q != nil {
			defer q.Close()
		}
	}

	d := doctor.New(cfg, q)

	// If specific categories requested
	if len(doctorCategory) > 0 {
		allResults := &doctor.Results{
			StartTime: time.Now(),
			Checks:    make([]doctor.CheckResult, 0),
		}

		for _, catStr := range doctorCategory {
			cat, ok := doctor.ParseCategory(catStr)
			if !ok {
				return fmt.Errorf("unknown category: %s", catStr)
			}

			results := d.RunCategory(context.Background(), cat)
			allResults.Checks = append(allResults.Checks, results.Checks...)
			allResults.Passed += results.Passed
			allResults.Failed += results.Failed
			allResults.Warned += results.Warned
			allResults.FixableIDs = append(allResults.FixableIDs, results.FixableIDs...)
		}

		allResults.Healthy = allResults.Failed == 0
		allResults.Duration = time.Since(allResults.StartTime)

		doctor.PrintResults(allResults, doctorFormat, doctorVerbose, doctorNoColor)

		if !allResults.Healthy {
			os.Exit(1)
		}
		return nil
	}

	// Run all checks
	results := d.Run(context.Background())
	doctor.PrintResults(results, doctorFormat, doctorVerbose, doctorNoColor)

	if !results.Healthy {
		os.Exit(1)
	}
	return nil
}

func runDoctorFix(cmd *cobra.Command, args []string) error {
	// Initialize queue connection if possible
	var q *queue.RedisQueue
	if cfg.Queue.RedisURL != "" {
		retryMaxAge, _ := time.ParseDuration(cfg.Queue.RetryMaxAge)
		if retryMaxAge == 0 {
			retryMaxAge = 7 * 24 * time.Hour
		}
		dialTimeout, _ := time.ParseDuration(cfg.Queue.DialTimeout)
		readTimeout, _ := time.ParseDuration(cfg.Queue.ReadTimeout)
		writeTimeout, _ := time.ParseDuration(cfg.Queue.WriteTimeout)
		q, _ = queue.NewRedisQueue(queue.Config{
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
		if q != nil {
			defer q.Close()
		}
	}

	d := doctor.New(cfg, q)

	// Apply specific fix
	if doctorFixID != "" {
		message, err := d.ApplyFix(context.Background(), doctorFixID, doctorDryRun)
		if err != nil {
			return fmt.Errorf("failed to apply fix %s: %w", doctorFixID, err)
		}
		if doctorDryRun {
			fmt.Printf("Dry run for %s:\n  %s\n", doctorFixID, message)
		} else {
			fmt.Printf("Fix %s applied successfully\n", doctorFixID)
		}
		return nil
	}

	// Run checks first to find fixable issues
	results := d.Run(context.Background())

	if len(results.FixableIDs) == 0 {
		fmt.Println("No fixable issues found.")
		return nil
	}

	// Apply all fixes
	if !doctorYes && !doctorDryRun {
		fmt.Printf("Found %d fixable issue(s). Apply fixes? [y/N] ", len(results.FixableIDs))
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fixResults, err := d.ApplyAllFixable(context.Background(), results, doctorDryRun)
	if err != nil {
		return fmt.Errorf("failed to apply fixes: %w", err)
	}

	doctor.PrintFixResults(fixResults, doctorFormat, doctorVerbose, doctorNoColor)
	return nil
}

func runDoctorReport(cmd *cobra.Command, args []string) error {
	// Initialize queue connection if possible
	var q *queue.RedisQueue
	if cfg.Queue.RedisURL != "" {
		retryMaxAge, _ := time.ParseDuration(cfg.Queue.RetryMaxAge)
		if retryMaxAge == 0 {
			retryMaxAge = 7 * 24 * time.Hour
		}
		dialTimeout, _ := time.ParseDuration(cfg.Queue.DialTimeout)
		readTimeout, _ := time.ParseDuration(cfg.Queue.ReadTimeout)
		writeTimeout, _ := time.ParseDuration(cfg.Queue.WriteTimeout)
		q, _ = queue.NewRedisQueue(queue.Config{
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
		if q != nil {
			defer q.Close()
		}
	}

	d := doctor.New(cfg, q)

	// Run all checks
	results := d.Run(context.Background())

	// Run config comparison
	comparison := doctor.CompareConfigToReality(context.Background(), cfg, q)

	// Print results
	doctor.PrintResults(results, doctorFormat, doctorVerbose, doctorNoColor)
	doctor.PrintComparison(comparison, doctorFormat, doctorVerbose, doctorNoColor)

	if !results.Healthy {
		os.Exit(1)
	}
	return nil
}

func runDoctorCompare(cmd *cobra.Command, args []string) error {
	// Initialize queue connection if possible
	var q *queue.RedisQueue
	if cfg.Queue.RedisURL != "" {
		retryMaxAge, _ := time.ParseDuration(cfg.Queue.RetryMaxAge)
		if retryMaxAge == 0 {
			retryMaxAge = 7 * 24 * time.Hour
		}
		dialTimeout, _ := time.ParseDuration(cfg.Queue.DialTimeout)
		readTimeout, _ := time.ParseDuration(cfg.Queue.ReadTimeout)
		writeTimeout, _ := time.ParseDuration(cfg.Queue.WriteTimeout)
		q, _ = queue.NewRedisQueue(queue.Config{
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
		if q != nil {
			defer q.Close()
		}
	}

	comparison := doctor.CompareConfigToReality(context.Background(), cfg, q)
	doctor.PrintComparison(comparison, doctorFormat, doctorVerbose, doctorNoColor)

	if comparison.Mismatched > 0 {
		os.Exit(1)
	}
	return nil
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

var recoveryCmd = &cobra.Command{
	Use:   "recovery",
	Short: "Recover emails from maildir that are missing from the database",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		// Setup logging
		logger, err := logging.New(logging.Config{
			Level:  cfg.Logging.Level,
			Format: cfg.Logging.Format,
			Output: cfg.Logging.Output,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}

		// Open database
		db, err := metadata.OpenFromConfig(cfg.Database)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		// Run recovery
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		sqlDB := db.(*metadata.SQLiteDB).DB
		if err := recovery.RecoverMaildirEmails(ctx, sqlDB, cfg.Storage.MaildirPath, logger.Logger); err != nil {
			return fmt.Errorf("recovery failed: %w", err)
		}

		fmt.Println("✓ Recovery completed successfully")
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
	domainDeleteCmd.Flags().BoolVar(&domainDeleteForce, "force", false, "Skip confirmation prompt")
	domainCmd.AddCommand(domainAddCmd)
	domainCmd.AddCommand(domainListCmd)
	domainCmd.AddCommand(domainDeleteCmd)
	domainCmd.AddCommand(domainEnableCmd)
	domainCmd.AddCommand(domainDisableCmd)
	rootCmd.AddCommand(domainCmd)

	// User commands
	userAddCmd.Flags().StringVar(&userAddPasswordHash, "password-hash", "", "Pre-hashed bcrypt password (alternative to positional password)")
	userAddCmd.Flags().BoolVar(&userAddAdmin, "admin", false, "Mark the user as a server admin")
	userDeleteCmd.Flags().BoolVar(&userDeleteForce, "force", false, "Skip confirmation prompt")
	userSetRoleCmd.Flags().StringVar(&setRoleDomain, "domain", "", "Domain scope for domain_admin role")
	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userPasswdCmd)
	userCmd.AddCommand(userDeleteCmd)
	userCmd.AddCommand(userEnableCmd)
	userCmd.AddCommand(userDisableCmd)
	userCmd.AddCommand(userSetRoleCmd)
	rootCmd.AddCommand(userCmd)

	// Database management commands
	dbRestoreCmd.Flags().BoolVar(&dbRestoreForce, "force", false, "Skip confirmation prompt")
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	dbCmd.AddCommand(dbShellCmd)
	dbCmd.AddCommand(dbStatsCmd)
	rootCmd.AddCommand(dbCmd)

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

	// Doctor commands with subcommands and flags
	doctorCmd.PersistentFlags().StringVarP(&doctorFormat, "format", "f", "text", "Output format (text, json, markdown)")
	doctorCmd.PersistentFlags().BoolVarP(&doctorVerbose, "verbose", "v", false, "Show detailed output")
	doctorCmd.PersistentFlags().BoolVar(&doctorNoColor, "no-color", false, "Disable colored output")

	doctorCheckCmd.Flags().StringSliceVar(&doctorCategory, "category", nil, "Check specific categories (infra, network, security, dns, config, queue)")
	doctorCmd.AddCommand(doctorCheckCmd)

	doctorFixCmd.Flags().BoolVar(&doctorDryRun, "dry-run", false, "Preview fixes without applying")
	doctorFixCmd.Flags().BoolVarP(&doctorYes, "yes", "y", false, "Apply all fixes without confirmation")
	doctorFixCmd.Flags().StringVar(&doctorFixID, "fix", "", "Apply specific fix by ID")
	doctorCmd.AddCommand(doctorFixCmd)

	doctorCmd.AddCommand(doctorReportCmd)
	doctorCmd.AddCommand(doctorCompareCmd)

	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(recoveryCmd)

	// Database migration commands
	migrateDBAutoCmd.Flags().StringVar(&migrateTargetDSN, "target", "", "Target database DSN (e.g., postgres://user:pass@localhost/mail)")
	migrateDBAutoCmd.Flags().BoolVar(&migrateAutoBackup, "backup", true, "Create backup before migration")
	migrateDBVerifyCmd.Flags().StringVar(&migrateTargetDSN, "target", "", "Target database DSN")
	migrateDBCmd.AddCommand(migrateDBAutoCmd)
	migrateDBCmd.AddCommand(migrateDBVerifyCmd)
	migrateDBCmd.AddCommand(migrateDBBackupCmd)
	rootCmd.AddCommand(migrateDBCmd)

	// Search commands
	searchCmd.AddCommand(searchReindexCmd)
	rootCmd.AddCommand(searchCmd)
}

// Search management commands
var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Manage full-text search index",
}

var searchReindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild the full-text search index from scratch",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cfg.EnsureDirectories(); err != nil {
			return err
		}

		var err error
		db, err = metadata.OpenFromConfig(cfg.Database)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		store, err := maildir.NewStore(db.RawDB(), cfg.Storage.MaildirPath)
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}

		searchCfg := &search.Config{
			Enabled:       cfg.Search.Enabled,
			Engine:        search.EngineType(cfg.Search.Engine),
			IndexPath:     cfg.Search.IndexPath,
			BatchSize:     cfg.Search.BatchSize,
			FlushInterval: cfg.Search.FlushInterval,
			Timeout:       cfg.Search.Timeout,
			MaxResults:    cfg.Search.MaxResults,
			Workers:       cfg.Search.Workers,
		}
		if searchCfg.BatchSize == 0 {
			searchCfg.BatchSize = 100
		}
		if searchCfg.Workers == 0 {
			searchCfg.Workers = 2
		}
		if searchCfg.FlushInterval == "" {
			searchCfg.FlushInterval = "100ms"
		}
		if searchCfg.Timeout == "" {
			searchCfg.Timeout = "5s"
		}
		if searchCfg.MaxResults == 0 {
			searchCfg.MaxResults = 1000
		}

		// Create search engine based on configured type
		var searchEngine search.SearchEngine
		switch searchCfg.Engine {
		case search.EngineSQLite:
			searchEngine, err = searchsqlite.NewEngine(db.RawDB(), searchCfg)
		case search.EnginePostgres:
			searchEngine, err = searchpg.NewEngine(db.RawDB(), searchCfg)
		default:
			searchEngine, err = searchbleve.NewEngine(searchCfg)
		}
		if err != nil {
			return fmt.Errorf("failed to initialize search engine: %w", err)
		}
		defer searchEngine.Close()

		userStore := &dbUserStore{db: db.RawDB()}
		searchIndexer := indexer.NewIndexer(searchEngine, store, userStore, searchCfg)

		if err := searchIndexer.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start indexer: %w", err)
		}
		defer searchIndexer.Stop()

		fmt.Printf("Reindexing all messages using %s engine...\n", searchEngine.Name())
		if err := searchIndexer.ReindexAll(context.Background()); err != nil {
			return fmt.Errorf("reindex failed: %w", err)
		}

		stats, err := searchEngine.Stats(context.Background())
		if err != nil {
			fmt.Println("Reindex completed (could not retrieve stats)")
			return nil
		}
		fmt.Printf("Reindex completed: %d documents indexed\n", stats.DocumentCount)
		return nil
	},
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
	return generateProcessUIDValidity()
}
