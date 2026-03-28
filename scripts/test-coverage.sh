#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

merged_profile="$repo_root/coverage.out"
printf 'mode: atomic\n' > "$merged_profile"

while IFS= read -r pkg; do
  safe_name="$(printf '%s' "$pkg" | tr '/[:space:]' '__')"
  package_profile="$temp_dir/${safe_name}.out"
  go test "$pkg" -covermode=atomic -coverprofile="$package_profile" -outputdir="$temp_dir"
  if [[ -f "$package_profile" ]]; then
    tail -n +2 "$package_profile" >> "$merged_profile"
  fi
done < <(go list ./...)

go tool cover -func="$merged_profile"
