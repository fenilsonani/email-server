#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Mail Server Installation Script${NC}"
echo "================================="

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Please run as root${NC}"
    exit 1
fi

# Variables
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/mailserver"
DATA_DIR="/var/lib/mailserver"
USER="mailserver"
GROUP="mailserver"

# Create user and group
echo -e "${YELLOW}Creating mailserver user...${NC}"
if ! id "$USER" &>/dev/null; then
    useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$USER"
fi

# Create directories
echo -e "${YELLOW}Creating directories...${NC}"
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR/maildir"
mkdir -p "$DATA_DIR/acme"
mkdir -p "$CONFIG_DIR/dkim"

# Set permissions
chown -R "$USER:$GROUP" "$DATA_DIR"
chown -R root:$GROUP "$CONFIG_DIR"
chmod 750 "$CONFIG_DIR"
chmod 750 "$CONFIG_DIR/dkim"

# Check if binary exists
if [ -f "./mailserver" ]; then
    echo -e "${YELLOW}Installing binary...${NC}"
    cp ./mailserver "$INSTALL_DIR/mailserver"
    chmod 755 "$INSTALL_DIR/mailserver"
else
    echo -e "${YELLOW}Building from source...${NC}"
    if command -v go &>/dev/null; then
        go build -o "$INSTALL_DIR/mailserver" ./cmd/mailserver
        chmod 755 "$INSTALL_DIR/mailserver"
    else
        echo -e "${RED}Go is not installed. Please build the binary first.${NC}"
        exit 1
    fi
fi

# Copy config if not exists
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    echo -e "${YELLOW}Creating default config...${NC}"
    cp configs/config.example.yaml "$CONFIG_DIR/config.yaml"
    chown root:$GROUP "$CONFIG_DIR/config.yaml"
    chmod 640 "$CONFIG_DIR/config.yaml"
fi

# Install systemd service
echo -e "${YELLOW}Installing systemd service...${NC}"
cp deploy/mailserver.service /etc/systemd/system/
systemctl daemon-reload

# Initialize database
echo -e "${YELLOW}Initializing database...${NC}"
sudo -u "$USER" "$INSTALL_DIR/mailserver" migrate --config "$CONFIG_DIR/config.yaml"

echo ""
echo -e "${GREEN}Installation complete!${NC}"
echo ""
echo "Next steps:"
echo "1. Edit the config file: $CONFIG_DIR/config.yaml"
echo "2. Generate DKIM keys: $INSTALL_DIR/mailserver dkim generate --domain yourdomain.com"
echo "3. Add your domain: $INSTALL_DIR/mailserver domain add yourdomain.com"
echo "4. Create a user: $INSTALL_DIR/mailserver user add user@yourdomain.com"
echo "5. Start the service: systemctl start mailserver"
echo "6. Enable on boot: systemctl enable mailserver"
echo ""
echo "DNS Records needed:"
echo "  - MX record: @ -> mail.yourdomain.com (priority 10)"
echo "  - A record: mail -> your.server.ip"
echo "  - SPF: @ TXT \"v=spf1 mx -all\""
echo "  - DKIM: See output of dkim generate command"
echo "  - DMARC: _dmarc TXT \"v=DMARC1; p=quarantine; rua=mailto:postmaster@yourdomain.com\""
