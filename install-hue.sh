#!/bin/sh
# Install the hue command from release v1.0.0.
# The digest is checksums/v1.0.0.txt in this repository, reviewed with the
# plugin source. The release SHA256SUMS asset is not consulted: that release
# can be edited, so its checksum can change together with the binary.
# set -e stops the script on any failure. install runs only after the check.
set -eu

release=v1.0.0
base="https://github.com/SneWs/hue/releases/download/${release}"
root=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
sums="${root}/checksums/${release}.txt"

case "$(uname -m)" in
x86_64 | amd64) asset=hue-linux-amd64 ;;
aarch64 | arm64) asset=hue-linux-arm64 ;;
*)
  echo "unsupported architecture: $(uname -m)" >&2
  exit 1
  ;;
esac

if [ ! -f "$sums" ]; then
  echo "missing reviewed digest file: ${sums}" >&2
  exit 1
fi

dest="${HOME}/.local/bin/hue"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

if ! grep -E "^[0-9a-f]{64}  ${asset}\$" "$sums" > "$dir/SHA256SUMS"; then
  echo "no reviewed digest for ${asset} in ${sums}" >&2
  exit 1
fi
if [ "$(wc -l < "$dir/SHA256SUMS")" -ne 1 ]; then
  echo "expected one reviewed digest for ${asset} in ${sums}" >&2
  exit 1
fi

curl -fsSL -o "$dir/$asset" "$base/$asset"

(
  cd "$dir"
  sha256sum -c --strict SHA256SUMS
)

mkdir -p "${HOME}/.local/bin"
install -m 0755 "$dir/$asset" "$dest"
echo "installed ${dest} from ${release} (${asset})"
