#!/bin/sh
set -e

# Create service user if it doesn't exist.
if ! id graywolf >/dev/null 2>&1; then
    useradd --system --home-dir /var/lib/graywolf --shell /usr/sbin/nologin \
        --groups audio,dialout graywolf
fi

systemctl daemon-reload
systemctl enable graywolf.service
echo "Graywolf installed. Start with:"
echo "  systemctl start graywolf"
echo ""
echo "Web UI: http://localhost:8080"
