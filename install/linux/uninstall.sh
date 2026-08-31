#!/usr/bin/env bash
set -euo pipefail

systemctl --user disable --now lanclip.service 2>/dev/null || true
unit="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user/lanclip.service"
binary="${HOME}/.local/bin/lanclip"
rm -f "$unit" "$binary"
systemctl --user daemon-reload
if [[ ${1:-} == --purge ]]; then
  rm -rf "${XDG_CONFIG_HOME:-${HOME}/.config}/lanclip" "${XDG_DATA_HOME:-${HOME}/.local/share}/lanclip"
  echo "Removed Lanclip and its identity, configuration, and pairing state."
else
  echo "Removed Lanclip; identity, configuration, and pairing state were preserved."
fi
