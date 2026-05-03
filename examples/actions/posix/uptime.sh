#!/usr/bin/env bash
# Action: uptime
# Grammar:  @@<otp>#uptime
# Args:     none
# Reply:    "up <duration> load <1m> root <pct>"
# Useful for remote-monitoring an unattended station.
set -eu

up=$(uptime | sed -E 's/.*up[[:space:]]+([^,]+(,[[:space:]]+[0-9]+:[0-9]+)?),[[:space:]]+[0-9]+ user.*/\1/' \
       | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
[[ -z "$up" ]] && up="?"

# 1-minute load, portable across Linux + macOS.
load=$(uptime | sed -E 's/.*load average[s]*:[[:space:]]+([0-9.]+).*/\1/')
[[ -z "$load" ]] && load="?"

root=$(df -h / | awk 'NR==2 {print $5}')
[[ -z "$root" ]] && root="?"

echo "up $up load $load root $root"
