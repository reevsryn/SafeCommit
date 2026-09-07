#!/usr/bin/env bash
# Resolve a safecommit binary for this runner, preferring a published release.
#
# Falls back to building from source, which is always correct but costs ~40s of
# every pull-request check. The fallback also covers `uses: ./` (this
# repository's own self-review), where the action ref is a branch rather than a
# version and building the working tree is exactly what we want.
#
# THE DOWNLOAD IS CHECKSUM-VERIFIED. A tool whose pitch is "we only report what
# we can prove" has no business fetching an unverified binary over the network
# and executing it. If the checksum is missing or does not match, we do not run
# the artifact -- we fall back to building from source and say why.
set -euo pipefail

REF="${SAFECOMMIT_REF:-}"
REPO="${SAFECOMMIT_ACTION_REPO:-}"
DEST="${RUNNER_TEMP}/safecommit"

fallback() {
  echo "::notice::$1 — building from source"
  echo "need_build=true" >> "$GITHUB_OUTPUT"
  exit 0
}

case "$(uname -s)" in
  Linux)  goos=linux ;;
  Darwin) goos=darwin ;;
  *)      fallback "unsupported OS $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) goarch=amd64 ;;
  arm64|aarch64) goarch=arm64 ;;
  *) fallback "unsupported architecture $(uname -m)" ;;
esac

[[ "$REF" =~ ^v[0-9] ]] || fallback "action ref '${REF:-<none>}' is not a release tag"
[[ -n "$REPO" ]] || fallback "action repository unknown"

asset="safecommit_${REF}_${goos}_${goarch}.tar.gz"
work="${RUNNER_TEMP}/safecommit-dl"
mkdir -p "$work"

# The moving `v1` release names its assets after the exact version it points
# at, so discover the real asset name rather than assuming it matches the ref.
if ! avail=$(gh release view "$REF" --repo "$REPO" --json assets \
      --jq '.assets[].name' 2>/dev/null); then
  fallback "no release '$REF' in $REPO"
fi
match=$(printf '%s\n' "$avail" | grep -E "_${goos}_${goarch}\.tar\.gz$" | head -1 || true)
[[ -n "$match" ]] || fallback "release '$REF' has no ${goos}/${goarch} binary"
asset="$match"

gh release download "$REF" --repo "$REPO" --dir "$work" \
   --pattern "$asset" --pattern "checksums.txt" --clobber 2>/dev/null \
   || fallback "could not download $asset from $REF"

[[ -f "$work/checksums.txt" ]] || fallback "release '$REF' publishes no checksums.txt"

( cd "$work"
  # Verify ONLY our asset: checksums.txt covers every platform, and the others
  # are absent, which would make a whole-file check fail.
  grep -F " $asset" checksums.txt > asset.sha256 \
    || { echo "::warning::no checksum entry for $asset"; exit 1; }
  if command -v sha256sum >/dev/null; then
    sha256sum -c asset.sha256
  else
    shasum -a 256 -c asset.sha256
  fi
) || fallback "checksum verification FAILED for $asset"

tar -xzf "$work/$asset" -C "$work"
found=$(find "$work" -type f -name safecommit -perm -u+x | head -1)
[[ -n "$found" ]] || fallback "archive $asset contained no safecommit binary"
mv "$found" "$DEST"
chmod +x "$DEST"

echo "verified and installed $asset"
"$DEST" --version
echo "need_build=false" >> "$GITHUB_OUTPUT"
