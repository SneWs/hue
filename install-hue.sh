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
config_home=${XDG_CONFIG_HOME:-$HOME/.config}
stamp="${config_home}/hue/installer-identity"
dir=$(mktemp -d)
stamp_tmp=""
stage=""
cleanup() {
  rm -rf "$dir"
  if [ -n "$stamp_tmp" ]; then
    rm -f "$stamp_tmp"
  fi
  if [ -n "$stage" ]; then
    rm -f "$stage"
  fi
}
trap cleanup EXIT

digest() {
  sha256sum "$1" | awk 'NR==1 { print $1 }'
}

is_sha256() {
  case "$1" in
  *[!0-9a-f]* | "") return 1 ;;
  esac
  [ "${#1}" -eq 64 ]
}

gh release download "$release" --repo "$repo" --pattern "$asset" --dir "$dir"

gh release verify-asset "$release" "$dir/$asset" --repo "$repo"

new_sum=$(digest "$dir/$asset")
if ! is_sha256 "$new_sum"; then
  echo "could not digest the attested ${asset}" >&2
  exit 1
fi

# Replace an existing command only when its bytes are this attested release,
# or when they match the identity recorded by a previous run of this installer.
# A different executable, symlink, or other owner's file stays in place.
if [ -e "$dest" ] || [ -L "$dest" ]; then
  if [ -L "$dest" ] || [ ! -f "$dest" ]; then
    echo "refusing to replace ${dest}: not a regular file" >&2
    exit 1
  fi
  owner=$(stat -c %u -- "$dest")
  if [ "$owner" != "$(id -u)" ]; then
    echo "refusing to replace ${dest}: owned by uid ${owner}" >&2
    exit 1
  fi
  current_sum=$(digest "$dest")
  recorded_path=""
  recorded_sum=""
  if [ -f "$stamp" ]; then
    while IFS= read -r line || [ -n "$line" ]; do
      case "$line" in
      path=*) recorded_path=${line#path=} ;;
      sha256=*) recorded_sum=${line#sha256=} ;;
      esac
    done < "$stamp"
  fi
  if [ "$current_sum" != "$new_sum" ] && {
    [ "$recorded_path" != "$dest" ] || [ "$recorded_sum" != "$current_sum" ] || ! is_sha256 "$recorded_sum"
  }; then
    echo "refusing to replace ${dest}: existing file is not the hue command recorded by this installer" >&2
    exit 1
  fi
fi

mkdir -p "${HOME}/.local/bin" "${config_home}/hue"
stage="${dest}.installer-$$"
install -m 0755 "$dir/$asset" "$stage"
installed_sum=$(digest "$stage")
if [ "$installed_sum" != "$new_sum" ]; then
  echo "staged file digest does not match the attested ${asset}" >&2
  exit 1
fi
mv -f "$stage" "$dest"
stage=""

stamp_tmp=$(mktemp "${config_home}/hue/installer-identity.XXXXXX")
printf '%s\n' \
  "path=${dest}" \
  "sha256=${new_sum}" \
  "release=${release}" \
  "asset=${asset}" \
  > "$stamp_tmp"
chmod 0644 "$stamp_tmp"
mv -f "$stamp_tmp" "$stamp"
stamp_tmp=""

echo "installed ${dest} from ${release} (${asset})"
