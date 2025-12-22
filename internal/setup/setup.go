package setup

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// SetupConfig holds the configuration gathered during setup
type SetupConfig struct {
	Domain      string
	Hostname    string
	AdminEmail  string
	AdminPass   string
	TLSEmail    string
	DataDir     string
	ConfigDir   string
	UseExisting bool
}

// Step represents a setup step
type Step struct {
	Name     string
	Action   func(*SetupConfig) error
	Verify   func(*SetupConfig) error
	Rollback func(*SetupConfig) error
}

// RunSetup runs the interactive setup wizard
func RunSetup() error {
	return RunSetupWithOptions(false)
}

// RunSetupWithOptions runs setup with optional force mode
func RunSetupWithOptions(force bool) error {
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("              MAIL SERVER SETUP WIZARD")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// First run preflight
	fmt.Println("Running preflight checks...\n")
	preflight := RunPreflightWithOptions(force)

	if !preflight.Ready {
		preflight.Print()
		if force {
			return fmt.Errorf("critical preflight checks failed (ports/redis/disk)")
		}
		return fmt.Errorf("preflight checks failed, fix issues or use --force to skip non-critical checks")
	}

	if force && preflight.Failed > 0 {
		preflight.Print()
		fmt.Println("\033[33m! Some checks failed but --force was used, continuing...\033[0m\n")
	} else {
		fmt.Println("\033[32m✓ Preflight checks passed!\033[0m\n")
	}

	// Gather configuration
	cfg := &SetupConfig{
		DataDir:   "/var/lib/mailserver",
		ConfigDir: "/etc/mailserver",
	}

	reader := bufio.NewReader(os.Stdin)

	// Domain
	fmt.Print("Enter your domain (e.g., example.com): ")
	domain, _ := reader.ReadString('\n')
	cfg.Domain = strings.TrimSpace(domain)
	if cfg.Domain == "" {
		return fmt.Errorf("domain is required")
	}

	// Hostname
	defaultHostname := "mail." + cfg.Domain
	fmt.Printf("Enter mail server hostname [%s]: ", defaultHostname)
	hostname, _ := reader.ReadString('\n')
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = defaultHostname
	}
	cfg.Hostname = hostname

	// Admin email
	defaultAdmin := "admin@" + cfg.Domain
	fmt.Printf("Enter admin email [%s]: ", defaultAdmin)
	adminEmail, _ := reader.ReadString('\n')
	adminEmail = strings.TrimSpace(adminEmail)
	if adminEmail == "" {
		adminEmail = defaultAdmin
	}
	cfg.AdminEmail = adminEmail

	// Admin password
	fmt.Print("Enter admin password: ")
	adminPass, _ := reader.ReadString('\n')
	cfg.AdminPass = strings.TrimSpace(adminPass)
	if cfg.AdminPass == "" {
		return fmt.Errorf("admin password is required")
	}

	// TLS email for Let's Encrypt
	fmt.Printf("Enter email for TLS certificates (Let's Encrypt) [%s]: ", cfg.AdminEmail)
	tlsEmail, _ := reader.ReadString('\n')
	tlsEmail = strings.TrimSpace(tlsEmail)
	if tlsEmail == "" {
		tlsEmail = cfg.AdminEmail
	}
	cfg.TLSEmail = tlsEmail

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Configuration Summary:")
	fmt.Printf("  Domain:    %s\n", cfg.Domain)
	fmt.Printf("  Hostname:  %s\n", cfg.Hostname)
	fmt.Printf("  Admin:     %s\n", cfg.AdminEmail)
	fmt.Printf("  TLS Email: %s\n", cfg.TLSEmail)
	fmt.Printf("  Data Dir:  %s\n", cfg.DataDir)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fmt.Print("\nProceed with setup? [Y/n]: ")
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))
	if confirm != "" && confirm != "y" && confirm != "yes" {
		return fmt.Errorf("setup cancelled")
	}

	// Run setup steps
	steps := []Step{
		{Name: "Create system user", Action: createSystemUser, Verify: verifySystemUser},
		{Name: "Create directories", Action: createDirectories, Verify: verifyDirectories},
		{Name: "Generate configuration", Action: generateConfig, Verify: verifyConfig},
		{Name: "Generate DKIM keys", Action: generateDKIM, Verify: verifyDKIM},
		{Name: "Initialize database", Action: initDatabase, Verify: verifyDatabase},
		{Name: "Create admin user", Action: createAdminUser, Verify: verifyAdminUser},
		{Name: "Install systemd service", Action: installSystemd, Verify: verifySystemd},
		{Name: "Start service", Action: startService, Verify: verifyService},
	}

	fmt.Println("\n")

	for i, step := range steps {
		fmt.Printf("[%d/%d] %s...\n", i+1, len(steps), step.Name)

		if err := step.Action(cfg); err != nil {
			fmt.Printf("\033[31m    ✗ Failed: %s\033[0m\n", err)
			return fmt.Errorf("setup failed at step '%s': %w", step.Name, err)
		}

		if step.Verify != nil {
			if err := step.Verify(cfg); err != nil {
				fmt.Printf("\033[31m    ✗ Verification failed: %s\033[0m\n", err)
				return fmt.Errorf("verification failed at step '%s': %w", step.Name, err)
			}
		}

		fmt.Printf("\033[32m    ✓ Done\033[0m\n")
	}

	// Print success and next steps
	printSuccess(cfg)

	return nil
}

func createSystemUser(cfg *SetupConfig) error {
	// Check if user exists
	if _, err := user.Lookup("mailserver"); err == nil {
		return nil // User already exists
	}

	cmd := exec.Command("useradd", "--system", "--home-dir", cfg.DataDir, "--shell", "/usr/sbin/nologin", "mailserver")
	return cmd.Run()
}

func verifySystemUser(cfg *SetupConfig) error {
	_, err := user.Lookup("mailserver")
	return err
}

func createDirectories(cfg *SetupConfig) error {
	dirs := []string{
		cfg.DataDir,
		cfg.DataDir + "/maildir",
		cfg.DataDir + "/acme",
		cfg.ConfigDir,
		cfg.ConfigDir + "/dkim",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
	}

	// Set ownership
	u, err := user.Lookup("mailserver")
	if err != nil {
		return err
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	for _, dir := range []string{cfg.DataDir, cfg.DataDir + "/maildir", cfg.DataDir + "/acme"} {
		if err := os.Chown(dir, uid, gid); err != nil {
			return err
		}
	}

	return nil
}

func verifyDirectories(cfg *SetupConfig) error {
	dirs := []string{cfg.DataDir, cfg.DataDir + "/maildir", cfg.ConfigDir}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return fmt.Errorf("directory %s not created", dir)
		}
	}
	return nil
}

func generateConfig(cfg *SetupConfig) error {
	configPath := cfg.ConfigDir + "/config.yaml"

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		cfg.UseExisting = true
		return nil
	}

	config := map[string]interface{}{
		"server": map[string]interface{}{
			"hostname": cfg.Hostname,
			"domain":   cfg.Domain,
		},
		"domains": []string{cfg.Domain},
		"storage": map[string]interface{}{
			"database_path": cfg.DataDir + "/mail.db",
			"maildir_path":  cfg.DataDir + "/maildir",
		},
		"tls": map[string]interface{}{
			"auto_cert":      true,
			"acme_email":     cfg.TLSEmail,
			"acme_cache_dir": cfg.DataDir + "/acme",
		},
		"dkim": map[string]interface{}{
			"selector": "mail",
			"key_path": cfg.ConfigDir + "/dkim/" + cfg.Domain + ".key",
		},
		"queue": map[string]interface{}{
			"redis_url": "redis://localhost:6379/0",
		},
		"admin": map[string]interface{}{
			"enabled": true,
			"port":    8080,
			"listen":  "127.0.0.1",
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0640)
}

func verifyConfig(cfg *SetupConfig) error {
	configPath := cfg.ConfigDir + "/config.yaml"
	_, err := os.Stat(configPath)
	return err
}

func generateDKIM(cfg *SetupConfig) error {
	keyPath := cfg.ConfigDir + "/dkim/" + cfg.Domain + ".key"

	// Check if key already exists
	if _, err := os.Stat(keyPath); err == nil {
		return nil
	}

	// Generate RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Encode private key
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	if err := os.WriteFile(keyPath, privateKeyPEM, 0600); err != nil {
		return err
	}

	// Generate public key for DNS
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	})

	pubKeyPath := cfg.ConfigDir + "/dkim/" + cfg.Domain + ".pub"
	return os.WriteFile(pubKeyPath, publicKeyPEM, 0644)
}

func verifyDKIM(cfg *SetupConfig) error {
	keyPath := cfg.ConfigDir + "/dkim/" + cfg.Domain + ".key"
	_, err := os.Stat(keyPath)
	return err
}

func initDatabase(cfg *SetupConfig) error {
	// Run migrations
	cmd := exec.Command("mailserver", "migrate", "--config", cfg.ConfigDir+"/config.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func verifyDatabase(cfg *SetupConfig) error {
	dbPath := cfg.DataDir + "/mail.db"
	_, err := os.Stat(dbPath)
	return err
}

func createAdminUser(cfg *SetupConfig) error {
	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Use mailserver CLI to add domain and user
	// First add domain
	cmd := exec.Command("mailserver", "domain", "add", cfg.Domain, "--config", cfg.ConfigDir+"/config.yaml")
	cmd.Run() // Ignore error if domain already exists

	// Add user
	cmd = exec.Command("mailserver", "user", "add", cfg.AdminEmail, "--password-hash", string(hash), "--admin", "--config", cfg.ConfigDir+"/config.yaml")
	return cmd.Run()
}

func verifyAdminUser(cfg *SetupConfig) error {
	// Just verify the command ran - actual verification would need DB check
	return nil
}

func installSystemd(cfg *SetupConfig) error {
	serviceContent := `[Unit]
Description=Personal Mail Server
After=network-online.target redis.service
Wants=network-online.target

[Service]
Type=simple
User=mailserver
Group=mailserver
WorkingDirectory=/var/lib/mailserver
ExecStart=/usr/local/bin/mailserver serve --config /etc/mailserver/config.yaml
Restart=on-failure
RestartSec=5

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/mailserver
ReadOnlyPaths=/etc/mailserver

AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
`

	if err := os.WriteFile("/etc/systemd/system/mailserver.service", []byte(serviceContent), 0644); err != nil {
		return err
	}

	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("systemctl", "enable", "mailserver")
	return cmd.Run()
}

func verifySystemd(cfg *SetupConfig) error {
	_, err := os.Stat("/etc/systemd/system/mailserver.service")
	return err
}

func startService(cfg *SetupConfig) error {
	cmd := exec.Command("systemctl", "start", "mailserver")
	return cmd.Run()
}

func verifyService(cfg *SetupConfig) error {
	// Check if service is running by checking ports
	ports := []int{25, 143}
	for _, port := range ports {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("service not listening on port %d", port)
		}
		conn.Close()
	}
	return nil
}

func printSuccess(cfg *SetupConfig) {
	fmt.Println("\n")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\033[32m           ✓ SETUP COMPLETE!\033[0m")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Your mail server is now running!")
	fmt.Println()
	fmt.Println("Admin Panel: http://127.0.0.1:8080/admin")
	fmt.Printf("Admin Login: %s\n", cfg.AdminEmail)
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("                    DNS RECORDS NEEDED")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Printf("Add these DNS records for %s:\n\n", cfg.Domain)

	// Get server IP
	serverIP := getServerIP()

	fmt.Printf("  A     mail.%s    →  %s\n", cfg.Domain, serverIP)
	fmt.Printf("  MX    %s         →  mail.%s (priority 10)\n", cfg.Domain, cfg.Domain)
	fmt.Printf("  TXT   %s         →  v=spf1 mx -all\n", cfg.Domain)
	fmt.Printf("  TXT   _dmarc.%s  →  v=DMARC1; p=quarantine; rua=mailto:postmaster@%s\n", cfg.Domain, cfg.Domain)
	fmt.Println()
	fmt.Println("For DKIM record, run:")
	fmt.Printf("  mailserver dkim show --domain %s\n", cfg.Domain)
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("                    NEXT STEPS")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("1. Add DNS records above to your domain")
	fmt.Println("2. Wait for DNS propagation (5-60 minutes)")
	fmt.Println("3. Verify DNS: mailserver doctor")
	fmt.Println("4. Access admin panel to manage users")
	fmt.Println()
	fmt.Println("Need help? Check: mailserver doctor")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func getServerIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "<YOUR_SERVER_IP>"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
