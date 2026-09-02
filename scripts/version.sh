#!/usr/bin/env bash
set -euo pipefail

if [[ -n ${LANCLIP_VERSION:-} ]]; then
  printf '%s\n' "$LANCLIP_VERSION"
  exit 0
fi

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [[ -n $(git -C "$repo_dir" status --porcelain 2>/dev/null) ]]; then
  printf '%s\n' '0.1.0-dev'
elif tag=$(git -C "$repo_dir" describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null); then
  printf '%s\n' "${tag#v}"
else
  printf '%s\n' '0.1.0-dev'
fi
