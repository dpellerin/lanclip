#!/usr/bin/env bash
set -euo pipefail

agent="${HOME}/Library/LaunchAgents/com.dpellerin.lanclip.plist"
indicator="${HOME}/Library/LaunchAgents/com.dpellerin.lanclip.indicator.plist"
base="${HOME}/Library/Application Support/Lanclip"
launchctl bootout "gui/${UID}/com.dpellerin.lanclip.indicator" 2>/dev/null || true
launchctl bootout "gui/${UID}/com.dpellerin.lanclip" 2>/dev/null || true
rm -f "$agent" "$indicator" "${base}/bin/lanclip"
if [[ ${1:-} == --purge ]]; then
  rm -rf "$base"
  echo "Removed Lanclip and its identity, configuration, and pairing state."
else
  echo "Removed Lanclip; identity, configuration, and pairing state were preserved."
fi
