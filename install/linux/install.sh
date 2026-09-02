#!/usr/bin/env bash
set -euo pipefail

if [[ $(uname -s) != Linux ]]; then
  echo "This installer is for Linux." >&2
  exit 1
fi
for command_name in go wl-paste wl-copy systemctl; do
  command -v "$command_name" >/dev/null || { echo "Missing dependency: $command_name" >&2; exit 1; }
done

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
version=$("$repo_dir/scripts/version.sh")
binary_dir="${HOME}/.local/bin"
unit_dir="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user"
mkdir -p "$binary_dir" "$unit_dir"
(cd "$repo_dir" && go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=${version}" -o "${binary_dir}/lanclip" ./cmd/lanclip)
sed "s|@BINARY@|${binary_dir}/lanclip|g" "${repo_dir}/install/linux/lanclip.service" > "${unit_dir}/lanclip.service"
systemctl --user daemon-reload
systemctl --user import-environment WAYLAND_DISPLAY XDG_RUNTIME_DIR
systemctl --user enable lanclip.service
systemctl --user restart lanclip.service
for _ in {1..50}; do
  "${binary_dir}/lanclip" status >/dev/null 2>&1 && break
  sleep 0.1
done
"${binary_dir}/lanclip" status >/dev/null
echo "Installed ${binary_dir}/lanclip"
"${binary_dir}/lanclip" version
echo "Check it with: systemctl --user status lanclip.service && lanclip doctor"
