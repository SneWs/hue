#!/bin/sh
# Install the hue command from the immutable v1.0.0 release.
# Checksum file first, then the binary. The binary is installed only after
# sha256sum accepts it. set -e stops the script on any failure.
set -eu

release=v1.0.0
base="https://github.com/SneWs/hue/releases/download/${release}"

case "$(uname -m)" in
x86_64 | amd64) asset=hue-linux-amd64 ;;
aarch64 | arm64) asset=hue-linux-arm64 ;;
*)
  echo "unsupported architecture: $(uname -m)" >&2
  exit 1
  ;;
esac

dest="${HOME}/.local/bin/hue"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

curl -fsSL -o "$dir/SHA256SUMS" "$base/SHA256SUMS"
curl -fsSL -o "$dir/$asset" "$base/$asset"

if ! grep -q "  ${asset}\$" "$dir/SHA256SUMS"; then
  echo "SHA256SUMS from ${release} has no entry for ${asset}" >&2
  exit 1
fi

(
  cd "$dir"
  sha256sum -c --ignore-missing SHA256SUMS
)

mkdir -p "${HOME}/.local/bin"
install -m 0755 "$dir/$asset" "$dest"
echo "installed ${dest} from ${release} (${asset})"
