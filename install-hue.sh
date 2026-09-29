#!/bin/sh
# Install hue from an immutable GitHub release chosen by the caller.
# gh release verify checks that tag's signed attestation. gh release
# verify-asset checks the downloaded file against that attestation.
# install runs only after both succeed. set -e stops on any failure.
set -eu

repo=SneWs/hue

if [ "$#" -ne 1 ]; then
  echo "usage: install-hue.sh vX.Y.Z" >&2
  exit 1
fi

release=$1
case "$release" in
*[!A-Za-z0-9._-]*)
  echo "invalid release tag: ${release}" >&2
  exit 1
  ;;
v[0-9]*.[0-9]*.[0-9]*) ;;
*)
  echo "pass an immutable release tag such as v1.2.3" >&2
  exit 1
  ;;
esac

if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI (gh) is required to verify the release attestation" >&2
  exit 1
fi

case "$(uname -m)" in
x86_64 | amd64) asset=hue-linux-amd64 ;;
aarch64 | arm64) asset=hue-linux-arm64 ;;
*)
  echo "unsupported architecture: $(uname -m)" >&2
  exit 1
  ;;
esac

# Mutable releases, including v1.0.0 and the moving latest tag, have no attestation.
gh release verify "$release" --repo "$repo"

dest="${HOME}/.local/bin/hue"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

gh release download "$release" --repo "$repo" --pattern "$asset" --dir "$dir"

gh release verify-asset "$release" "$dir/$asset" --repo "$repo"

mkdir -p "${HOME}/.local/bin"
install -m 0755 "$dir/$asset" "$dest"
echo "installed ${dest} from ${release} (${asset})"
