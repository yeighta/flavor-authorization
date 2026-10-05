#!/usr/bin/env bash
# Registers the X API credentials in .env.x as GitHub Actions secrets.
# Values are read from the file and never printed.
set -euo pipefail

cd "$(dirname "$0")/.."
file=.env.x
repo=yeighta/flavor-authorization
keys=(X_API_KEY X_API_SECRET X_ACCESS_TOKEN X_ACCESS_TOKEN_SECRET)

if [ ! -f "$file" ]; then
  echo "$file がありません。" >&2
  exit 1
fi

missing=()
for k in "${keys[@]}"; do
  if ! grep -qE "^${k}=.+" "$file"; then
    missing+=("$k")
  fi
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "$file に値が入っていません: ${missing[*]}" >&2
  exit 1
fi

gh secret set -f "$file" -R "$repo"
echo "登録済みの X シークレット:"
gh secret list -R "$repo" | grep -E '^X_' | cut -f1
