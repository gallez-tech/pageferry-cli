#!/bin/sh
set -eu

repository="gallez-tech/pageferry-cli"
install_dir="${PAGEFERRY_INSTALL_DIR:-${HOME}/.local/bin}"

case "$(uname -s)" in
  Linux) platform="linux" ;;
  Darwin) platform="darwin" ;;
  *) echo "pageferry: this installer supports Linux and macOS" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) architecture="amd64" ;;
  arm64|aarch64) architecture="arm64" ;;
  *) echo "pageferry: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "pageferry: curl is required" >&2
  exit 1
fi

version="${PAGEFERRY_VERSION:-}"
if [ -z "$version" ]; then
  release_url=$(curl --fail --silent --show-error --location --output /dev/null --write-out '%{url_effective}' \
    "https://github.com/${repository}/releases/latest")
  version=${release_url##*/}
fi
case "$version" in
  v*) ;;
  *) version="v${version}" ;;
esac

archive="pageferry-${platform}-${architecture}.tar.gz"
base_url="https://github.com/${repository}/releases/download/${version}"
temporary_dir=$(mktemp -d 2>/dev/null || mktemp -d -t pageferry)
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

curl --fail --silent --show-error --location "${base_url}/${archive}" -o "${temporary_dir}/${archive}"
curl --fail --silent --show-error --location "${base_url}/checksums.txt" -o "${temporary_dir}/checksums.txt"

expected=$(awk -v name="$archive" '$2 == name { print $1 }' "${temporary_dir}/checksums.txt")
if [ -z "$expected" ]; then
  echo "pageferry: release checksum is missing for ${archive}" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "${temporary_dir}/${archive}" | awk '{ print $1 }')
else
  actual=$(shasum -a 256 "${temporary_dir}/${archive}" | awk '{ print $1 }')
fi
if [ "$actual" != "$expected" ]; then
  echo "pageferry: checksum verification failed" >&2
  exit 1
fi

tar -xzf "${temporary_dir}/${archive}" -C "$temporary_dir"
mkdir -p "$install_dir"
install -m 0755 "${temporary_dir}/pageferry-${platform}-${architecture}" "${install_dir}/pageferry"

echo "Installed pageferry ${version#v} to ${install_dir}/pageferry"
case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *) echo "Add ${install_dir} to PATH to run pageferry." ;;
esac
