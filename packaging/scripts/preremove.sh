#!/bin/sh
set -e
systemctl stop graywolf.service 2>/dev/null || true
systemctl disable graywolf.service 2>/dev/null || true
