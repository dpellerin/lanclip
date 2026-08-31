#!/usr/bin/env bash
set -euo pipefail

if [[ $(uname -s) != Darwin ]]; then
  echo "This installer is for macOS." >&2
  exit 1
fi
command -v go >/dev/null || { echo "Missing dependency: Go" >&2; exit 1; }

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
version=${LANCLIP_VERSION:-0.1.0-dev}
base="${HOME}/Library/Application Support/Lanclip"
binary="${base}/bin/lanclip"
agent="${HOME}/Library/LaunchAgents/com.dpellerin.lanclip.plist"
mkdir -p "${base}/bin" "${base}/logs" "${HOME}/Library/LaunchAgents"
(cd "$repo_dir" && go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=${version}" -o "$binary" ./cmd/lanclip)
sed -e "s|@BINARY@|${binary}|g" -e "s|@LOGDIR@|${base}/logs|g" "${repo_dir}/install/macos/com.dpellerin.lanclip.plist" > "$agent"
plutil -lint "$agent" >/dev/null
launchctl bootout "gui/${UID}/com.dpellerin.lanclip" 2>/dev/null || true
started=false
for _ in {1..50}; do
  if launchctl bootstrap "gui/${UID}" "$agent" 2>/dev/null; then
    started=true
    break
  fi
  sleep 0.1
done
if [[ $started != true ]]; then
  # Repeat without suppressing stderr so launchd's final diagnostic is visible.
  launchctl bootstrap "gui/${UID}" "$agent"
fi
for _ in {1..50}; do
  "$binary" status >/dev/null 2>&1 && break
  sleep 0.1
done
"$binary" status >/dev/null
echo "Installed ${binary}"
"$binary" version
echo "Check it with: '${binary}' doctor"
