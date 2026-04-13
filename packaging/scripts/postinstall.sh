#!/bin/sh
set -e

# Create plugdev group if it doesn't exist (Debian/Ubuntu have it; others may not).
groupadd -f plugdev 2>/dev/null || true

# Create service user if it doesn't exist.
if ! id graywolf >/dev/null 2>&1; then
    useradd --system --home-dir /var/lib/graywolf --shell /usr/sbin/nologin \
        --groups audio,dialout,plugdev graywolf
else
    usermod -aG plugdev graywolf 2>/dev/null || true
fi

# Reload udev rules for CM108 HID device access.
udevadm control --reload-rules 2>/dev/null || true
udevadm trigger --subsystem-match=hidraw 2>/dev/null || true

systemctl daemon-reload
systemctl enable graywolf.service
echo "Graywolf installed. Start with:"
echo "  systemctl start graywolf"
echo ""
echo "Web UI: http://localhost:8080"
