#!/bin/sh
# SPDX-FileCopyrightText: Copyright (c) 2026 the whaleshell authors
# SPDX-License-Identifier: MIT
#
# Install the whaleshell CLI from a GitHub release (OpenShell-style one-liner).
#
# Usage:
#   curl -LsSf https://raw.githubusercontent.com/whaleshell/osg-cli/main/install.sh | sh
#
# Environment:
#   WHALESHELL_VERSION      Release tag (default: latest non-draft release; use
#                           "nightly" for the moving nightly build)
#   WHALESHELL_INSTALL_DIR  Install directory (default: ~/.local/bin)
#   WHALESHELL_REPO         Override owner/name (default: whaleshell/osg-cli)
#
set -eu

APP_NAME="whaleshell"
REPO="${WHALESHELL_REPO:-whaleshell/osg-cli}"
GITHUB_URL="https://github.com/${REPO}"
API_URL="https://api.github.com/repos/${REPO}"

info() { printf '%s: %s\n' "$APP_NAME" "$*" >&2; }
error() { printf '%s: error: %s\n' "$APP_NAME" "$*" >&2; exit 1; }

has_cmd() { command -v "$1" >/dev/null 2>&1; }

require_cmd() {
  has_cmd "$1" || error "'$1' is required"
}

download() {
  _url="$1"
  _out="$2"
  if has_cmd curl; then
    curl -fLsS --retry 3 --max-redirs 5 -o "$_out" "$_url"
  elif has_cmd wget; then
    wget -q -O "$_out" "$_url"
  else
    error "need curl or wget"
  fi
}

# Map uname to GoReleaser archive names: whaleshell_<Os>_<Arch>.tar.gz
detect_target() {
  _os="$(uname -s)"
  _arch="$(uname -m)"
  case "$_os" in
    Linux)  _goos="Linux" ;;
    Darwin) _goos="Darwin" ;;
    MINGW*|MSYS*|CYGWIN*) _goos="Windows" ;;
    *) error "unsupported OS: $_os" ;;
  esac
  case "$_arch" in
    x86_64|amd64) _goarch="x86_64" ;;
    aarch64|arm64) _goarch="arm64" ;;
    armv7l) _goarch="armv7" ;;
    i386|i686) _goarch="i386" ;;
    *) error "unsupported arch: $_arch" ;;
  esac
  printf '%s_%s\n' "$_goos" "$_goarch"
}

resolve_version() {
  if [ -n "${WHALESHELL_VERSION:-}" ]; then
    printf '%s\n' "$WHALESHELL_VERSION"
    return
  fi
  # Latest published release (stable or prerelease marked latest=false — prefer
  # /releases/latest which skips prereleases; fall back to first release list entry).
  _json="$(mktemp)"
  if download "${API_URL}/releases/latest" "$_json" 2>/dev/null; then
    _tag="$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$_json" | head -1)"
    rm -f "$_json"
    if [ -n "$_tag" ]; then
      printf '%s\n' "$_tag"
      return
    fi
  fi
  rm -f "$_json"
  # Include prereleases (alpha) when no stable "latest" exists yet.
  _json="$(mktemp)"
  download "${API_URL}/releases?per_page=5" "$_json" || error "cannot list releases"
  _tag="$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$_json" | head -1)"
  rm -f "$_json"
  [ -n "$_tag" ] || error "no releases found on ${REPO}; build from source or wait for CI"
  printf '%s\n' "$_tag"
}

install_dir() {
  if [ -n "${WHALESHELL_INSTALL_DIR:-}" ]; then
    printf '%s\n' "$WHALESHELL_INSTALL_DIR"
    return
  fi
  printf '%s\n' "${HOME}/.local/bin"
}

is_on_path() {
  case ":${PATH}:" in
    *:"$1":*) return 0 ;;
    *) return 1 ;;
  esac
}

main() {
  require_cmd uname
  require_cmd tar
  require_cmd install
  require_cmd mktemp
  require_cmd sed

  _version="$(resolve_version)"
  _target="$(detect_target)"
  _archive="${APP_NAME}_${_target}.tar.gz"
  if [ "$_target" = "Windows_x86_64" ] || [ "$_target" = "Windows_arm64" ]; then
    _archive="${APP_NAME}_${_target}.zip"
  fi
  _url="${GITHUB_URL}/releases/download/${_version}/${_archive}"
  _checksums_url="${GITHUB_URL}/releases/download/${_version}/checksums.txt"
  _dir="$(install_dir)"

  info "installing ${_version} (${_target}) → ${_dir}"
  _tmpdir="$(mktemp -d)"
  trap 'rm -rf "$_tmpdir"' EXIT

  if ! download "$_url" "${_tmpdir}/${_archive}"; then
    error "download failed: ${_url}"
  fi

  if download "$_checksums_url" "${_tmpdir}/checksums.txt" 2>/dev/null; then
    info "verifying checksum"
    (
      cd "$_tmpdir"
      if has_cmd sha256sum; then
        grep " ${_archive}\$" checksums.txt | sha256sum -c -
      elif has_cmd shasum; then
        grep " ${_archive}\$" checksums.txt | shasum -a 256 -c -
      else
        info "sha256 tool missing; skipping checksum verify"
      fi
    )
  else
    info "checksums.txt not found for ${_version}; continuing without verify"
  fi

  case "$_archive" in
    *.zip)
      require_cmd unzip
      unzip -q "${_tmpdir}/${_archive}" -d "$_tmpdir"
      ;;
    *)
      tar -xzf "${_tmpdir}/${_archive}" -C "$_tmpdir"
      ;;
  esac

  _bin="${_tmpdir}/${APP_NAME}"
  [ -f "$_bin" ] || _bin="$(find "$_tmpdir" -type f -name "$APP_NAME" -o -name "${APP_NAME}.exe" | head -1)"
  [ -n "$_bin" ] && [ -f "$_bin" ] || error "binary not found in archive"

  mkdir -p "$_dir" 2>/dev/null || true
  if [ -w "$_dir" ] || mkdir -p "$_dir" 2>/dev/null; then
    install -m 755 "$_bin" "${_dir}/${APP_NAME}"
  else
    info "elevated permissions required for ${_dir}"
    sudo mkdir -p "$_dir"
    sudo install -m 755 "$_bin" "${_dir}/${APP_NAME}"
  fi

  info "installed $("${_dir}/${APP_NAME}" version 2>/dev/null || echo "$_version") → ${_dir}/${APP_NAME}"

  if ! is_on_path "$_dir"; then
    info "${_dir} is not on PATH; add it, e.g.:"
    info "  export PATH=\"${_dir}:\$PATH\""
  fi

  info "next:  ${APP_NAME} --help"
  info "       ${APP_NAME} sandbox create --name demo --workspace . --policy …"
}

main "$@"
