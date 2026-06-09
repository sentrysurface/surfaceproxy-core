#!/bin/sh
set -e

# SurfaceProxy Installer Script
# Downloads the latest release binary and installs it to /usr/local/bin

OWNER="sentrysurface"
REPO="surfaceproxy-core"

# 1. Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS}" in
  darwin)  PLATFORM="darwin" ;;
  linux)   PLATFORM="linux" ;;
  *)
    echo "Error: Unsupported operating system: ${OS}"
    echo "SurfaceProxy currently supports macOS (darwin) and Linux."
    exit 1
    ;;
esac

# 2. Detect CPU Architecture
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64)   ARCH_NAME="amd64" ;;
  arm64|aarch64)  ARCH_NAME="arm64" ;;
  *)
    echo "Error: Unsupported CPU architecture: ${ARCH}"
    exit 1
    ;;
esac

# 3. Get latest release version from GitHub API
echo "Finding latest version of SurfaceProxy..."
if [ -z "${VERSION}" ]; then
  LATEST_RELEASE_URL="https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"
  # Try curl, fallback to wget
  if command -v curl >/dev/null 2>&1; then
    VERSION=$(curl -s "${LATEST_RELEASE_URL}" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  elif command -v wget >/dev/null 2>&1; then
    VERSION=$(wget -qO- "${LATEST_RELEASE_URL}" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  fi
fi

if [ -z "${VERSION}" ]; then
  # Fallback version if GitHub API request fails or is rate-limited
  VERSION="v0.1.0-alpha"
  echo "Warning: Could not fetch latest release tag from GitHub API (rate-limited?). Falling back to ${VERSION}."
fi

# Strip 'v' prefix if user specified it without, but release URL expects the tag name.
# Ensure VERSION starts with 'v'
case "${VERSION}" in
  v*) ;;
  *) VERSION="v${VERSION}" ;;
esac

echo "Target version: ${VERSION}"
echo "Platform:       ${PLATFORM}"
echo "Architecture:   ${ARCH_NAME}"

# 4. Download and Extract Binary
TARBALL_NAME="surface-proxy-${VERSION}-${PLATFORM}-${ARCH_NAME}.tar.gz"
DOWNLOAD_URL="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}/${TARBALL_NAME}"

TMP_DIR=$(mktemp -d)
clean_up() {
  rm -rf "${TMP_DIR}"
}
trap clean_up EXIT

echo "Downloading from: ${DOWNLOAD_URL}..."
if command -v curl >/dev/null 2>&1; then
  curl -sSL -o "${TMP_DIR}/${TARBALL_NAME}" "${DOWNLOAD_URL}"
elif command -v wget >/dev/null 2>&1; then
  wget -q -O "${TMP_DIR}/${TARBALL_NAME}" "${DOWNLOAD_URL}"
else
  echo "Error: Either curl or wget is required to run this installer."
  exit 1
fi

echo "Extracting..."
tar -xzf "${TMP_DIR}/${TARBALL_NAME}" -C "${TMP_DIR}"

# Inside the tarball, the binary is named: surface-proxy-${VERSION}-${PLATFORM}-${ARCH_NAME}
EXTRACTED_BINARY="surface-proxy-${VERSION}-${PLATFORM}-${ARCH_NAME}"

if [ ! -f "${TMP_DIR}/${EXTRACTED_BINARY}" ]; then
  # Fallback check in case archive structure is slightly different
  if [ -f "${TMP_DIR}/surface-proxy" ]; then
    EXTRACTED_BINARY="surface-proxy"
  else
    echo "Error: Extracted binary not found in download archive."
    exit 1
  fi
fi

# 5. Install Binary
INSTALL_DIR="/usr/local/bin"
TARGET_PATH="${INSTALL_DIR}/surface-proxy"

echo "Installing to ${TARGET_PATH}..."
if [ -w "${INSTALL_DIR}" ]; then
  mv "${TMP_DIR}/${EXTRACTED_BINARY}" "${TARGET_PATH}"
  chmod +x "${TARGET_PATH}"
else
  echo "Write permission denied for ${INSTALL_DIR}. Attempting with sudo..."
  if command -v sudo >/dev/null 2>&1; then
    sudo mv "${TMP_DIR}/${EXTRACTED_BINARY}" "${TARGET_PATH}"
    sudo chmod +x "${TARGET_PATH}"
  else
    echo "Error: Cannot write to ${INSTALL_DIR} and sudo is not available."
    echo "Copying to local directory instead..."
    mkdir -p ./bin
    mv "${TMP_DIR}/${EXTRACTED_BINARY}" "./bin/surface-proxy"
    chmod +x "./bin/surface-proxy"
    TARGET_PATH="./bin/surface-proxy"
    echo "Installed to ${TARGET_PATH}"
  fi
fi

echo ""
echo "================================================================="
echo " 🎉 SurfaceProxy has been successfully installed to:"
echo "    ${TARGET_PATH}"
echo "================================================================="
echo ""
echo "To get started:"
echo "  1. Run 'surface-proxy' to launch the daemon."
echo "  2. Open http://localhost:8080 to view the savings dashboard."
echo "  3. See the README.md or docs/ for IDE / framework integrations."
echo ""
