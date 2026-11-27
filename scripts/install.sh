#!/usr/bin/env bash
#
# Installs opencode-sandbox automatically from GitHub releases.
# Runs via the curl/wget one-liner or when invoked directly.
#
set -euo pipefail

# --- Configuration -------------------------------------------------------------
REPO_OWNER="shredingerlabs"
REPO_NAME="oc_sandbox"
DEFAULT_INSTALL_PATH="$HOME/.oc-sandbox"
MIN_DISK_SPACE_MB=500
GITHUB_API_BASE="https://api.github.com"
TEMP_DIR=""

# --- Global variables ----------------------------------------------------------
INSTALL_PATH="$DEFAULT_INSTALL_PATH"
VERSION=""
FORCE=false
SYMLINKS=true
SHORTCUT=true
SHORTCUT_CREATED=false
SYMLINK_CREATED=false
VERBOSE=false
DOWNLOADED_VERSION=""
USER_AGENT="opencode-sandbox-install-script"
PRESERVE_ALLOWLIST=true
BINARY_NAME="oc-sandbox"

# --- Binary Selection ----------------------------------------------------------
# Returns the release binary's file name for the platform per the matrix
# scheme oc-sandbox_<goos>_<goarch> (linux/darwin × amd64/arm64), not the
# PLATFORM_* values (Linux/x86_64).
go_binary_platform_name() {
  local goos goarch
  case "$PLATFORM_OS" in
    Linux*)  goos="linux" ;;
    Darwin*) goos="darwin" ;;
    *)       return 1 ;;
  esac
  case "$PLATFORM_ARCH" in
    x86_64|amd64)   goarch="amd64" ;;
    arm64|aarch64)  goarch="arm64" ;;
    *)              return 1 ;;
  esac
  printf 'oc-sandbox_%s_%s\n' "$goos" "$goarch"
}

# Source: the platform-matching binary in the release extraction directory.
install_dir_binary() {
  local install_dir="$1"
  local subpath="bin"
  if [[ ! -d "${install_dir}/bin" && -d "${install_dir}/dist/bin" ]]; then
    subpath="dist/bin"
  fi
  printf '%s/%s/%s\n' "$install_dir" "$subpath" "$(go_binary_platform_name)"
}

# Entry-point path: the Go binary (default since the parity-gate flip, #98).
entry_point_path() {
  local install_dir="$1"
  printf '%s/bin/%s\n' "$install_dir" "$BINARY_NAME"
}

# --- Platform variables --------------------------------------------------------
PLATFORM_OS=""
PLATFORM_ARCH=""

# --- Signal handling -----------------------------------------------------------
cleanup() {
  if [[ -n "$TEMP_DIR" && -d "$TEMP_DIR" ]]; then
    if $VERBOSE; then
      echo "Cleaning up temp directory: $TEMP_DIR"
    fi
    rm -rf "$TEMP_DIR"
  fi
}

trap cleanup EXIT INT TERM

# --- Helper functions ----------------------------------------------------------
log_verbose() {
  if $VERBOSE; then
    echo "$1" >&2
  fi
}

log_error() {
  echo "Error: $1" >&2
}

log_info() {
  echo "$1"
}

log_warn() {
  echo "Warning: $1" >&2
}

exit_with_error() {
  log_error "$1"
  exit 1
}

exit_with_usage_error() {
  log_error "$1"
  show_help
  exit 1
}

# Reads one confirmation character (Y/N) for update prompts.
# Prefers stdin when it is a terminal; otherwise /dev/tty — important for the
# bash one-liner ('curl ... | bash'), where stdin is the script pipe and a
# 'read' from stdin would swallow script bytes.
# Returns 1 when no interactive input is possible (no TTY).
ask_confirm() {
  local prompt_text="$1"
  local reply=""

  if [[ -t 0 ]]; then
    read -p "$prompt_text" -n 1 -r reply
    echo
    REPLY="$reply"
    return 0
  fi

  if { exec 3</dev/tty; } 2>/dev/null; then
    read -u 3 -p "$prompt_text" -n 1 -r reply
    exec 3<&-
    echo >&2
    REPLY="$reply"
    return 0
  fi

  return 1
}

# --- Dependency checking -------------------------------------------------------
check_dependencies() {
  local missing_deps=()

  # Check curl or wget
  if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
    missing_deps+=("curl or wget")
  fi

  # Check the other required tools
  for cmd in tar grep awk sed jq; do
    if ! command -v "$cmd" &> /dev/null; then
      missing_deps+=("$cmd")
    fi
  done

  if [[ ${#missing_deps[@]} -gt 0 ]]; then
    log_error "Missing dependencies: ${missing_deps[*]}"
    echo "Install these tools and try again." >&2
    exit 3
  fi
}

# --- Platform detection --------------------------------------------------------
detect_platform() {
  local kernel arch

  kernel=$(uname -s)
  arch=$(uname -m)

  case "$kernel" in
    Linux*)  PLATFORM_OS="Linux" ;;
    Darwin*) PLATFORM_OS="Darwin" ;;
    MINGW*|MSYS*|CYGWIN*)
      log_error "Native Windows without WSL is not supported."
      return 1
      ;;
    *)
      log_error "Unsupported operating system: $kernel"
      return 1
      ;;
  esac

  case "$arch" in
    x86_64|amd64)   PLATFORM_ARCH="x86_64" ;;
    arm64|aarch64)  PLATFORM_ARCH="arm64" ;;
    *)
      log_error "Unsupported architecture: $arch"
      return 1
      ;;
  esac
}

# --- Disk-space checking -------------------------------------------------------
check_disk_space() {
  local path="$1"
  local required_mb="$2"

  local available_kb
  available_kb=$(df -k "$path" 2>/dev/null | awk 'NR==2 {print $4}')

  if [[ -z "$available_kb" ]]; then
    log_error "Cannot check disk space for: $path"
    return 1
  fi

  local available_mb=$((available_kb / 1024))

  if [[ $available_mb -lt $required_mb ]]; then
    log_error "Not enough disk space available."
    echo "  Required: ${required_mb}MB" >&2
    echo "  Available: ${available_mb}MB" >&2
    exit 1
  fi

  log_verbose "Disk-space check passed: ${available_mb}MB available"
}

# --- Network operations with retry ---------------------------------------------
download_with_retry() {
  local url="$1"
  local output="$2"
  local max_attempts=3
  local attempt=1

  while [[ $attempt -le $max_attempts ]]; do
    log_verbose "Download attempt $attempt/$max_attempts: $url"

    if command -v curl &> /dev/null; then
      if curl -fsSL -A "$USER_AGENT" -o "$output" "$url"; then
        return 0
      fi
    elif command -v wget &> /dev/null; then
      if wget -q --user-agent="$USER_AGENT" -O "$output" "$url"; then
        return 0
      fi
    fi

    if [[ $attempt -lt $max_attempts ]]; then
      local wait_time=$((2 ** (attempt - 1)))
      log_verbose "Download failed, waiting ${wait_time}s..."
      sleep "$wait_time"
    fi

    ((attempt++))
  done

  log_error "Download failed after $max_attempts attempts: $url"
  return 1
}

# --- GitHub release detection --------------------------------------------------
get_latest_release() {
  local api_url="${GITHUB_API_BASE}/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
  local temp_file
  temp_file=$(mktemp)

  if download_with_retry "$api_url" "$temp_file"; then
    local tag
    tag=$(grep -oP '"tag_name":\s*"\K[^"]+' "$temp_file" | head -1)
    if [[ -n "$tag" ]]; then
      rm -f "$temp_file"
      echo "$tag"
      return 0
    fi
  fi

  rm -f "$temp_file"

  # Fallback: HTML scraping
  log_verbose "GitHub API failed, using HTML scraping as fallback"
  local html_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest"
  local html_file
  html_file=$(mktemp)

  if download_with_retry "$html_url" "$html_file"; then
    local tag
    tag=$(grep -oP 'tag/\K[^"]+' "$html_file" | head -1)
    rm -f "$html_file"
    if [[ -n "$tag" ]]; then
      echo "$tag"
      return 0
    fi
  fi

  rm -f "$html_file"
  return 1
}

# --- Download methods ----------------------------------------------------------
download_via_git() {
  local tag="$1"
  local target_dir="$2"

  local repo_url="https://github.com/${REPO_OWNER}/${REPO_NAME}.git"
  local max_attempts=3
  local attempt=1

  log_verbose "Cloning repository at tag: $tag"

  while [[ $attempt -le $max_attempts ]]; do
    log_verbose "Git clone attempt $attempt/$max_attempts"

    # Remove an existing directory if present
    if [[ -d "$target_dir" ]]; then
      rm -rf "$target_dir"
    fi

    if git -c advice.detachedHead=false clone --depth 1 --branch "$tag" "$repo_url" "$target_dir" 2>&1; then
      return 0
    fi

    if [[ $attempt -lt $max_attempts ]]; then
      local wait_time=$((2 ** (attempt - 1)))
      log_verbose "Git clone failed, waiting ${wait_time}s..."
      sleep "$wait_time"
    fi

    ((attempt++))
  done

  log_verbose "Git clone failed after $max_attempts attempts"
  return 1
}

download_via_tarball() {
  local tag="$1"
  local target_dir="$2"

  local tarball_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/archive/refs/tags/${tag}.tar.gz"
  local temp_tarball
  temp_tarball=$(mktemp)

  log_verbose "Downloading tarball: $tarball_url"

  if download_with_retry "$tarball_url" "$temp_tarball"; then
    mkdir -p "$target_dir"

    # Extraction with retry logic
    local max_attempts=3
    local attempt=1

    while [[ $attempt -le $max_attempts ]]; do
      log_verbose "Extracting tarball (attempt $attempt/$max_attempts)"

      if tar -xzf "$temp_tarball" -C "$(dirname "$target_dir")" 2>/dev/null; then
        local extracted_dir
        extracted_dir=$(find "$(dirname "$target_dir")" -maxdepth 1 -type d -name "${REPO_NAME}-${tag}" 2>/dev/null | head -1)

        if [[ -n "$extracted_dir" && -d "$extracted_dir" ]]; then
          # Move contents into target_dir
          mv "${extracted_dir}"/* "$target_dir/"
          mv "${extracted_dir}"/.[^.]* "$target_dir/" 2>/dev/null || true
          rm -rf "$extracted_dir"
          rm -f "$temp_tarball"
          log_verbose "Tarball extracted successfully"
          return 0
        else
          log_verbose "Extracted directory not found: ${extracted_dir:-'null'}"
        fi
      fi

      if [[ $attempt -lt $max_attempts ]]; then
        local wait_time=$((2 ** (attempt - 1)))
        log_verbose "Extraction failed, waiting ${wait_time}s..."
        sleep "$wait_time"
      fi

      ((attempt++))
    done

    log_verbose "Tarball extraction failed after $max_attempts attempts"
  fi

  rm -f "$temp_tarball"
  return 1
}

# --- Installation logic --------------------------------------------------------
prepare_installation() {
  local version="$1"

  TEMP_DIR=$(mktemp -d)
  log_verbose "Temp directory: $TEMP_DIR"

  local source_dir="${TEMP_DIR}/opencode-sandbox"
  mkdir -p "$source_dir"

  # Try git clone first
  if command -v git &> /dev/null; then
    echo "Using git clone method"
    if download_via_git "$version" "$source_dir"; then
      DOWNLOADED_VERSION="$version"
      return 0
    fi
    echo "Git method failed, using tarball fallback"
  else
    echo "Git not found, using the tarball method directly"
  fi

  # Fallback to tarball
  log_verbose "Using the tarball method"
  if download_via_tarball "$version" "$source_dir"; then
    DOWNLOADED_VERSION="$version"
    return 0
  fi

  exit_with_error "Download for version $version failed"
}

# Locate a special file's preserve path in the target: the flattened runtime
# layout (root level — the real release-branch case) wins over the legacy
# dist/ prefix. The old dist/-only checks missed flattened installs and
# clobbered their user files.
preserve_source_path() {
  local target_dir="$1" rel="$2"
  if [[ -f "$target_dir/$rel" ]]; then
    printf '%s\n' "$target_dir/$rel"
  elif [[ -f "$target_dir/dist/$rel" ]]; then
    printf '%s\n' "$target_dir/dist/$rel"
  fi
  return 0
}

install_files() {
  local source_dir="$1"
  local target_dir="$2"

  log_verbose "Copying files to: $target_dir"

  # Check whether allowlist.txt must be preserved (per the user's choice)
  local allowlist_src allowlist_backup=""
  if $PRESERVE_ALLOWLIST; then
    allowlist_src=$(preserve_source_path "$target_dir" "proxy/allowlist.txt")
    if [[ -n "$allowlist_src" ]]; then
      allowlist_backup=$(mktemp)
      cp "$allowlist_src" "$allowlist_backup"
      log_verbose "Backing up ${allowlist_src#"$target_dir"/} (user choice: preserved)"
    fi
  else
    log_verbose "proxy/allowlist.txt will be overwritten (user choice: not preserved)"
  fi

  # Back up searxng/settings.yml (user-editable; a missing one is re-seeded
  # from the release template after the copy)
  local searxng_settings_src searxng_settings=""
  searxng_settings_src=$(preserve_source_path "$target_dir" "searxng/settings.yml")
  if [[ -n "$searxng_settings_src" ]]; then
    searxng_settings=$(mktemp)
    cp "$searxng_settings_src" "$searxng_settings"
    log_verbose "Backing up ${searxng_settings_src#"$target_dir"/} (user settings preserved)"
  fi

  # Back up toolchains/custom/ + toolchains/snapshots/ (always-keep, §11):
  # user recipes and recipe-editor snapshots survive updates completely;
  # a missing one is re-seeded from the release template after the copy.
  local toolchains_backup=""
  if [[ -d "$target_dir/toolchains/custom" || -d "$target_dir/toolchains/snapshots" ]]; then
    toolchains_backup=$(mktemp -d)
    if [[ -d "$target_dir/toolchains/custom" ]]; then
      cp -r "$target_dir/toolchains/custom" "$toolchains_backup/custom"
    fi
    if [[ -d "$target_dir/toolchains/snapshots" ]]; then
      cp -r "$target_dir/toolchains/snapshots" "$toolchains_backup/snapshots"
    fi
    log_verbose "Backing up toolchains/custom/ + toolchains/snapshots/ (always kept)"
  fi

  # Create the target directory
  mkdir -p "$target_dir"

  # Remove stale scripts/files from earlier releases:
  # - the pre-rename scripts init-project.sh / configure-project.sh exist
  #   after the update only under the new names (Workspace rename, #148);
  # - build-container.sh stopped shipping at the toolchain-sets cutover
  #   (#215 — the toolchain-sets builder build-images.sh replaces it);
  # - the stage-era Dockerfile stopped shipping at the same cutover — the
  #   stale root file joins this removal (dist/Dockerfile.custom also stopped
  #   shipping, but it is NEVER removed: the porting warning reads it).
  for stale in init-project.sh configure-project.sh build-container.sh; do
    if [[ -f "$target_dir/scripts/$stale" ]]; then
      rm -f "$target_dir/scripts/$stale"
      log_verbose "Removed stale script $stale"
    fi
  done
  for stale in Dockerfile dist/Dockerfile; do
    if [[ -f "$target_dir/$stale" ]]; then
      rm -f "$target_dir/$stale"
      log_verbose "Removed stale file $stale (toolchain-sets cutover)"
    fi
  done

  # Copy all files
  cp -r "$source_dir"/* "$target_dir/"

  # Restore allowlist.txt when requested
  if [[ -n "$allowlist_backup" && -f "$allowlist_backup" ]]; then
    mkdir -p "$(dirname "$allowlist_src")"
    cp "$allowlist_backup" "$allowlist_src"
    rm -f "$allowlist_backup"
    log_verbose "${allowlist_src#"$target_dir"/} restored"
  fi

  # Restore the SearXNG settings when previously present
  if [[ -n "$searxng_settings" ]]; then
    mkdir -p "$(dirname "$searxng_settings_src")"
    cp "$searxng_settings" "$searxng_settings_src"
    rm -f "$searxng_settings"
    log_verbose "${searxng_settings_src#"$target_dir"/} restored (user settings preserved)"
  fi

  # Restore toolchains/custom/ + toolchains/snapshots/ (always kept; merged
  # per content — user files win against templates)
  if [[ -n "$toolchains_backup" ]]; then
    if [[ -d "$toolchains_backup/custom" ]]; then
      mkdir -p "$target_dir/toolchains/custom"
      cp -r "$toolchains_backup/custom/." "$target_dir/toolchains/custom/"
    fi
    if [[ -d "$toolchains_backup/snapshots" ]]; then
      mkdir -p "$target_dir/toolchains/snapshots"
      cp -r "$toolchains_backup/snapshots/." "$target_dir/toolchains/snapshots/"
    fi
    rm -rf "$toolchains_backup"
    log_verbose "toolchains/custom/ + toolchains/snapshots/ restored (always kept)"
  fi

  # Remove the .git directory to save space
  if [[ -d "$target_dir/.git" ]]; then
    rm -rf "$target_dir/.git"
    log_verbose "Removed the .git directory"
  fi
}

# Copy the platform-matching oc-sandbox binary to <install>/bin/oc-sandbox —
# the Go binary is the entry point. A release without the binary aborts the
# installation (the convenience artifacts would otherwise dangle).
extract_go_binary() {
  local install_dir="$1"
  local source_binary

  if [[ ! -d "${install_dir}/bin" && ! -d "${install_dir}/dist/bin" ]]; then
    exit_with_error "No Go binary in the release (bin/ missing)."
  fi

  source_binary=$(install_dir_binary "$install_dir")
  if [[ ! -f "$source_binary" ]]; then
    exit_with_error "Go binary for this platform missing in the release: ${source_binary}"
  fi

  mkdir -p "${install_dir}/bin"
  # Remove the target before copying: a TUI update runs while the TUI (this
  # very binary) is still executing — cp cannot overwrite a running program
  # ("Text file busy", exit 1). Unlink + fresh copy replaces the file; the
  # running process keeps its old inode (Linux).
  rm -f "${install_dir}/bin/${BINARY_NAME}"
  cp "$source_binary" "${install_dir}/bin/${BINARY_NAME}"
  chmod +x "${install_dir}/bin/${BINARY_NAME}"
  log_verbose "Go binary installed: ${install_dir}/bin/${BINARY_NAME}"
}

set_executable_permissions() {
  local install_dir="$1"

  log_verbose "Setting executable permissions on .sh files"

  while IFS= read -r -d '' file; do
    chmod +x "$file"
    log_verbose "Permission set: $file"
  done < <(find "$install_dir" -type f -name "*.sh" -print0 2>/dev/null)
}

create_symlinks() {
  local install_dir="$1"
  local bin_dir="$HOME/.local/bin"

  log_verbose "Creating symlinks in: $bin_dir"

  mkdir -p "$bin_dir"

  # Remove stale symlinks pointing into the installation
  while IFS= read -r -d '' link; do
    local target
    target=$(readlink "$link")
    if [[ "$target" == "$install_dir"* ]]; then
      rm "$link" 2>/dev/null || true
      log_verbose "Removing stale symlink: $link -> $target"
    fi
  done < <(find "$bin_dir" -maxdepth 1 -type l -print0 2>/dev/null)

  # Create the single entry-point symlink (Go binary)
  local script
  script=$(entry_point_path "$install_dir")

  if [[ ! -e "$script" ]]; then
    log_warn "Symlink skipped: entry point not found: $script"
    return 0
  fi

  local link_target="${bin_dir}/oc-sandbox"

  # Remove an existing symlink
  if [[ -L "$link_target" ]]; then
    rm "$link_target" 2>/dev/null || true
    log_verbose "Removed existing symlink: $link_target"
  fi

  # Create the new symlink (fail-soft: a convenience artifact never aborts
  # the installation)
  if ! ln -s "$script" "$link_target" 2>/dev/null; then
    log_warn "Symlink directory not writable: $bin_dir"
    echo "  Manual fix: create it by hand:" >&2
    echo "  ln -s ${script} ${link_target}" >&2
    return 0
  fi
  SYMLINK_CREATED=true
  log_verbose "Created symlink: $link_target -> $script"
}

# --- Desktop shortcut creation -------------------------------------------------
SHORTCUT_COMMENT="Interactive OpenCode-Sandbox TUI"

# Creates the platform-specific desktop shortcut for the TUI.
# Failures warn only — the installation itself never fails.
create_shortcut() {
  local install_dir="$1"

  log_verbose "Creating desktop shortcut for $PLATFORM_OS"

  local ok=false
  case "$PLATFORM_OS" in
    Linux)
      if is_wsl; then
        create_shortcut_wsl "$install_dir" && ok=true
      else
        create_shortcut_linux "$install_dir" && ok=true
      fi
      ;;
    Darwin)
      create_shortcut_macos "$install_dir" && ok=true
      ;;
  esac

  if $ok; then
    SHORTCUT_CREATED=true
    log_verbose "Desktop shortcut created."
  fi
  return 0
}

is_wsl() {
  grep -qi microsoft /proc/version 2>/dev/null
}

# Installs the bundled Hicolor icons to $HOME/.local/share/icons/hicolor so
# the .desktop file resolves via the theme name (Icon=oc-sandbox).
install_hicolor_icons() {
  local install_dir="$1"
  local icon_source="${install_dir}/icons/linux/share/icons/hicolor"
  local icon_target="$HOME/.local/share/icons/hicolor"

  if [[ ! -d "$icon_source" ]]; then
    log_verbose "No Hicolor icons found (${icon_source}) — the shortcut is created without an icon."
    return 1
  fi

  if ! mkdir -p "$icon_target" 2>/dev/null; then
    log_verbose "Directory not writable: $icon_target"
    return 1
  fi

  if ! cp -R "${icon_source}/." "$icon_target/" 2>/dev/null; then
    log_verbose "Hicolor icons could not be copied to: $icon_target"
    return 1
  fi

  # Best effort: refresh the icon cache (not every environment ships the tool)
  if command -v gtk-update-icon-cache &>/dev/null; then
    gtk-update-icon-cache -f -t "$icon_target" >/dev/null 2>&1 || true
  fi

  log_verbose "Hicolor icons installed: $icon_target"
  return 0
}

create_shortcut_linux() {
  local install_dir="$1"
  local apps_dir="$HOME/.local/share/applications"
  local desktop_file="${apps_dir}/oc-sandbox.desktop"
  local script
  script=$(entry_point_path "$install_dir")

  if [[ ! -e "$script" ]]; then
    log_warn "Desktop shortcut skipped: entry point not found: $script"
    return 1
  fi

  if ! mkdir -p "$apps_dir" 2>/dev/null; then
    log_warn "Desktop shortcut skipped: directory not writable: $apps_dir"
    return 1
  fi

  local icon_line="Icon=oc-sandbox"
  if ! install_hicolor_icons "$install_dir"; then
    log_verbose "Hicolor icons could not be installed — the shortcut is created without an icon."
    icon_line=""
  fi

  {
    cat << EOF
[Desktop Entry]
Type=Application
Name=OC Sandbox
Comment=${SHORTCUT_COMMENT}
Exec=${script}
Terminal=true
Categories=Development;
EOF
    [[ -z "$icon_line" ]] || echo "$icon_line"
  } > "$desktop_file" 2>/dev/null || {
    log_warn "Desktop shortcut skipped: file not writable: $desktop_file"
    return 1
  }

  log_info "Desktop shortcut created: $desktop_file"
  return 0
}

# Translates a WSL path into a UNC path (\\wsl$\<distro>\... or
# \\wsl.localhost\<distro>\...) that Windows can reach regardless of the
# tmpfs mount drive letter. `wslpath -w` returns a drive path (e.g. D:\...)
# for tmpfs paths, which is invalid in the Start-menu context — so the UNC
# prefix is built by hand here.
icon_wsl_unc_path() {
  local path="$1"
  local distro="${WSL_DISTRO_NAME:-}"
  if [[ -z "$distro" && -r /etc/os-release ]]; then
    # No grep/awk: PATH can be empty in test/minimal environments.
    distro=${WSL_DISTRO_NAME:-}
    if [[ -z "$distro" ]]; then
      local line
      while IFS= read -r line || [[ -n "$line" ]]; do
        if [[ "$line" == NAME=* ]]; then
          distro=${line#NAME=}
          distro=${distro#\"}
          distro=${distro%\"}
          break
        fi
      done < /etc/os-release
    fi
  fi
  if [[ -z "$distro" ]]; then
    return 1
  fi
  # / -> \ via parameter expansion (no sed: PATH can be empty in test/minimal
  # environments).
  local win_rel="${path//\//\\}"
  printf '\\\\wsl.localhost\\%s%s\n' "$distro" "$win_rel"
  return 0
}

# Copies the icon over to Windows (%LOCALAPPDATA%\oc-sandbox\oc-sandbox.ico).
# The Start menu cannot render .lnk icons from UNC paths
# (\\wsl.localhost\...) — IconLocation needs a Windows-native file. Returns
# the copy's Windows path, empty on failure.
install_icon_windows_side() {
  local icon_file="$1"
  local win_localappdata localappdata_unix
  win_localappdata=$(powershell.exe -NoProfile -Command "[Environment]::GetFolderPath('LocalApplicationData')" 2>/dev/null | tr -d '\r')
  win_localappdata=${win_localappdata%%$'\n'*}
  if [[ -z "$win_localappdata" ]]; then
    return 1
  fi
  localappdata_unix=$(wslpath -u "$win_localappdata" 2>/dev/null)
  if [[ -z "$localappdata_unix" || ! -d "$localappdata_unix" ]]; then
    return 1
  fi
  if ! mkdir -p "${localappdata_unix}/oc-sandbox" 2>/dev/null; then
    return 1
  fi
  if ! cp "$icon_file" "${localappdata_unix}/oc-sandbox/oc-sandbox.ico" 2>/dev/null; then
    return 1
  fi
  printf '%s\\oc-sandbox\\oc-sandbox.ico\n' "$win_localappdata"
  return 0
}

create_shortcut_wsl() {
  local install_dir="$1"
  local script
  script=$(entry_point_path "$install_dir")

  if [[ ! -e "$script" ]]; then
    log_warn "Desktop shortcut skipped: entry point not found: $script"
    return 1
  fi

  # The binary entry point runs directly via wsl.exe (bash <elf> would fail).

  if ! command -v powershell.exe &>/dev/null; then
    log_warn "Desktop shortcut skipped: powershell.exe not available (WSL interop disabled?)."
    echo "  Manual fix: create a Start-menu shortcut by hand that runs:" >&2
    echo "  wsl.exe -e ${script}" >&2
    return 1
  fi

  # Windows resolves the Start-menu directory itself (locale-, profile-path-
  # and USERNAME-independent). Never guess via /mnt/c/Users/<user>.
  local win_start_menu
  win_start_menu=$(powershell.exe -NoProfile -Command "[Environment]::GetFolderPath('StartMenu')" 2>/dev/null | tr -d '\r')
  win_start_menu=${win_start_menu%%$'\n'*}

  if [[ -z "$win_start_menu" ]]; then
    log_warn "Desktop shortcut skipped: Windows Start-menu directory not found."
    echo "  Manual fix: create a Start-menu shortcut by hand that runs:" >&2
    echo "  wsl.exe -e ${script}" >&2
    return 1
  fi

  local lnk_path_win="${win_start_menu}\\Programs\\OC Sandbox.lnk"

  local icon_block=""
  local icon_file="${install_dir}/icons/windows/oc-sandbox.ico"
  if [[ -f "$icon_file" ]]; then
    local icon_path_win
    # The Start menu cannot render icons from UNC paths (\\wsl.localhost\...)
    # and tmpfs drive paths (D:\...) are invalid in the Start-menu context —
    # IconLocation needs a Windows-native file. So the icon is copied to
    # %LOCALAPPDATA%\oc-sandbox\; the UNC path is only a fallback.
    icon_path_win=$(install_icon_windows_side "$icon_file")
    if [[ -n "$icon_path_win" ]]; then
      log_verbose "Icon copied to Windows: $icon_path_win"
    else
      log_verbose "Icon copy to Windows failed — trying the UNC path as fallback."
      icon_path_win=$(icon_wsl_unc_path "$icon_file")
      if [[ -z "$icon_path_win" ]]; then
        icon_path_win=$(wslpath -w "$icon_file" 2>/dev/null)
      fi
    fi
    if [[ -n "$icon_path_win" ]]; then
      icon_block="\$sc.IconLocation = '${icon_path_win}'; "
      log_verbose "Icon referenced: $icon_path_win"
    else
      log_verbose "Icon path could not be translated — the shortcut is created without an icon."
    fi
  else
    log_verbose "No icon found (${icon_file}) — the shortcut is created without an icon."
  fi
  local ps_block
  # Go binary: run directly (bash <elf> would fail)
  ps_block='$sc = (New-Object -ComObject WScript.Shell).CreateShortcut('"'"''"${lnk_path_win}"''"'"'); $sc.TargetPath = '"'"'%SystemRoot%\System32\wsl.exe'"'"'; $sc.Arguments = '"'"'-e '"${script}"''"'"'; '"$icon_block"'$sc.Save()'

  log_verbose "Creating .lnk via powershell.exe: $lnk_path_win"
  if ! powershell.exe -NoProfile -Command "$ps_block" 2>/dev/null; then
    log_warn "Desktop shortcut skipped: powershell.exe could not create the shortcut."
    return 1
  fi

  log_info "Windows Start-menu shortcut created: OC Sandbox.lnk"
  return 0
}

create_shortcut_macos() {
  local install_dir="$1"
  local script
  script=$(entry_point_path "$install_dir")
  local app_dir="$HOME/Applications/OC Sandbox.app"
  local contents="${app_dir}/Contents"

  if [[ ! -e "$script" ]]; then
    log_warn "Desktop shortcut skipped: entry point not found: $script"
    return 1
  fi

  if ! mkdir -p "${contents}/MacOS" "${contents}/Resources" 2>/dev/null; then
    log_warn "Desktop shortcut skipped: directory not writable: $app_dir"
    return 1
  fi

  if ! command -v osascript &>/dev/null; then
    log_warn "Desktop shortcut skipped: osascript not found."
    echo "  Manual fix: create the app bundle under ${app_dir} by hand, opening ${script} in a terminal." >&2
    return 1
  fi

  local stub="${contents}/MacOS/OC Sandbox"
  {
    cat << APPLESCRIPT
#!/usr/bin/env bash
exec /usr/bin/osascript -e 'tell application "Terminal" to do script "${script}"'
APPLESCRIPT
  } > "$stub" 2>/dev/null || {
    log_warn "Desktop shortcut skipped: the app bundle could not be written: $app_dir"
    return 1
  }
  chmod +x "$stub"

  {
    cat << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>OC Sandbox</string>
  <key>CFBundleDisplayName</key>
  <string>OC Sandbox</string>
  <key>CFBundleIdentifier</key>
  <string>io.github.oc-sandbox.tui</string>
  <key>CFBundleExecutable</key>
  <string>OC Sandbox</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
</dict>
PLIST
  } > "${contents}/Info.plist" 2>/dev/null || {
    log_warn "Desktop shortcut skipped: Info.plist could not be written: $app_dir"
    return 1
  }

  local iconset_dir="${install_dir}/icons/macos/oc-sandbox.iconset"
  if [[ -d "$iconset_dir" ]]; then
    local generated_icns="${TEMP_DIR}/oc-sandbox.icns"
    if command -v iconutil &>/dev/null \
      && iconutil -c icns "$iconset_dir" -o "$generated_icns" 2>/dev/null \
      && cp "$generated_icns" "${contents}/Resources/AppIcon.icns" 2>/dev/null; then
      printf '  <key>CFBundleIconFile</key>\n  <string>AppIcon</string>\n' >> "${contents}/Info.plist"
    else
      log_verbose "Icon could not be generated from the iconset — the app is created without an icon."
    fi
  else
    log_verbose "No iconset found (${iconset_dir}) — the shortcut is created without an icon."
  fi

  log_info "App bundle created: $app_dir"
  return 0
}

remove_shortcut() {
  local install_dir="$1"
  log_verbose "Removing desktop shortcuts for $PLATFORM_OS"

  local removed=0
  case "$PLATFORM_OS" in
    Linux)
      if is_wsl; then
        # WSL: try to remove the .lnk file in the Windows Start menu
        if command -v powershell.exe &>/dev/null; then
          local win_start_menu
          win_start_menu=$(powershell.exe -NoProfile -Command "[Environment]::GetFolderPath('StartMenu')" 2>/dev/null | tr -d '\r')
          win_start_menu=${win_start_menu%%$'\n'*}
          if [[ -n "$win_start_menu" ]]; then
            local lnk_unix
            lnk_unix=$(wslpath -u "${win_start_menu}\\Programs\\OC Sandbox.lnk" 2>/dev/null || true)
            if [[ -n "$lnk_unix" && -f "$lnk_unix" ]]; then
              rm -f "$lnk_unix"
              removed=$((removed + 1))
            fi
          fi
        fi
      else
        local desktop_file="$HOME/.local/share/applications/oc-sandbox.desktop"
        if [[ -f "$desktop_file" ]]; then
          rm -f "$desktop_file"
          removed=$((removed + 1))
        fi
      fi
      ;;
    Darwin)
      local app_dir="$HOME/Applications/OC Sandbox.app"
      if [[ -d "$app_dir" ]]; then
        rm -rf "$app_dir"
        removed=$((removed + 1))
      fi
      ;;
  esac

  if [[ $removed -gt 0 ]]; then
    log_info "Desktop shortcut(s) removed."
  fi
}

validate_installation() {
  local install_dir="$1"

  log_verbose "Validating installation"

  local errors=0

  # Check the essential files
  local required_files=(
    "scripts/start.sh"
    "scripts/init-workspace.sh"
    "scripts/configure-workspace.sh"
    "scripts/uninstall.sh"
    "toolchains/base.Dockerfile"
    "toolchains/manifest.json"
  )

  for file in "${required_files[@]}"; do
    local file_path="${install_dir}/${file}"
    if [[ ! -f "$file_path" ]]; then
      log_error "Missing file: ${file_path}"
      ((errors++))
    elif [[ "$file" == *.sh && ! -x "$file_path" ]]; then
      log_error "File not executable: ${file_path}"
      ((errors++))
    fi
  done

  # Check the Go binary (entry point)
  local binary="${install_dir}/bin/${BINARY_NAME}"
  if [[ ! -x "$binary" ]]; then
    log_error "Go binary missing or not executable: ${binary}"
    ((errors++))
  fi

  if [[ $errors -gt 0 ]]; then
    exit_with_error "Validation failed: found $errors errors"
  fi

  log_verbose "Installation validated"
}

check_existing_installation() {
  local install_path="$1"

  if [[ -d "$install_path" ]]; then
    if $FORCE; then
      log_verbose "Overwriting the existing installation (--force set)"
      return 0
    fi

    echo "Existing installation found: $install_path"

    if ! ask_confirm "Overwrite? [y/N] "; then
      log_error "Existing installation found, but no interactive terminal is available (e.g. 'curl ... | bash' without a terminal)."
      log_error "Append --force to overwrite, or run the script interactively in a terminal:"
      log_error "  curl -sL https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/scripts/install.sh | bash -s -- --force"
      exit 2
    fi

    if [[ $REPLY =~ ^[Yy]$ ]]; then
      # Ask about allowlist.txt
      if [[ -f "$install_path/proxy/allowlist.txt" ]]; then
        echo "Existing proxy/allowlist.txt found."

        if ! ask_confirm "Keep the proxy configuration? [Y/n] "; then
          log_error "No interactive terminal available — please use --force."
          exit 2
        fi

        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
          log_verbose "proxy/allowlist.txt will be preserved"
          PRESERVE_ALLOWLIST=true
        else
          log_verbose "proxy/allowlist.txt will be overwritten"
          PRESERVE_ALLOWLIST=false
        fi
      fi
      return 0
    else
      echo "Installation aborted."
      exit 2
    fi
  fi

  return 0
}

# --- Help and usage ------------------------------------------------------------
show_help() {
  cat << EOF
Usage: $0 [options]

Installs opencode-sandbox automatically from GitHub releases.

Options:
  --install_path <path>  Installation path (default: $DEFAULT_INSTALL_PATH)
  --version <tag>        Install a specific version (default: latest)
  --force                Overwrite an existing installation without asking
  --yes                  Non-interactive update: prompts are pre-confirmed
                         (e.g. from the TUI's update menu; alias of --force)
  --no_symlinks          Skip the symlink in \$HOME/.local/bin
                         (default: the <oc-sandbox> symlink is created)
  --no_shortcut          Skip the desktop shortcut (default: created;
                         Linux: .desktop, WSL: Windows Start-menu .lnk,
                         macOS: .app in ~/Applications)
  --symlinks             No effect — the symlink is now the default
  --shortcut             No effect — the desktop shortcut is now the default
  --bin                  (no effect — the Go binary is now the default)
  --verbose              Enable verbose output
  --help                 Show this help and exit

Examples:
  # Install with the default path, latest version, symlink + shortcut
  $0

  # Install without convenience artifacts
  $0 --no_symlinks --no_shortcut

  # Install with a custom path
  $0 --install_path ~/my-sandbox

  # Install a specific version
  $0 --version v1.0.0

  # Force an update
  $0 --force

  # Combined options
  $0 --install_path ~/sandbox --version v1.0.0 --verbose

Bash one-liner:
  curl -sL https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash

  or, when curl is unavailable:
  wget -qO- https://raw.githubusercontent.com/shredingerlabs/oc_sandbox/main/scripts/install.sh | bash

EOF
}

# --- Argument parsing ----------------------------------------------------------
parse_arguments() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --install_path)
        INSTALL_PATH="${2:-}"
        if [[ -z "$INSTALL_PATH" ]]; then
          exit_with_usage_error "--install_path requires a path"
        fi
        shift 2
        ;;
      --version)
        VERSION="${2:-}"
        if [[ -z "$VERSION" ]]; then
          exit_with_usage_error "--version requires a tag"
        fi
        shift 2
        ;;
      --force)
        FORCE=true
        shift
        ;;
      --yes)
        # Non-interactive update (TUI invocation): prompts pre-confirmed,
        # allowlist preservation stays the default (PRESERVE_ALLOWLIST=true).
        FORCE=true
        shift
        ;;
      --no_symlinks)
        SYMLINKS=false
        shift
        ;;
      --no_shortcut)
        SHORTCUT=false
        shift
        ;;
      --symlinks)
        # No effect: the symlink is now the default (opt out via --no_symlinks)
        shift
        ;;
      --shortcut)
        # No effect: the shortcut is now the default (opt out via --no_shortcut)
        shift
        ;;
      --bin)
        # No effect: the Go binary has been the default since the parity-gate flip (#98)
        shift
        ;;
      --verbose)
        VERBOSE=true
        shift
        ;;
      --help)
        show_help
        exit 0
        ;;
      *)
        exit_with_usage_error "Unknown argument: $1"
        ;;
    esac
  done
}

# --- Main program --------------------------------------------------------------
main() {
  echo "==> opencode-sandbox installation"

  parse_arguments "$@"
  check_dependencies

  # Detect the platform (PLATFORM_OS/PLATFORM_ARCH), e.g. for shortcut creation
  detect_platform || exit_with_error "Could not detect the platform"

  # Check disk space
  check_disk_space "$(dirname "$INSTALL_PATH")" "$MIN_DISK_SPACE_MB"

  # Determine the version
  if [[ -z "$VERSION" ]]; then
    echo "Determining the latest release version..."
    VERSION=$(get_latest_release)
    if [[ -z "$VERSION" ]]; then
      exit_with_error "Could not determine the latest release version"
    fi
    echo "Found version: $VERSION"
  else
    echo "Installing specific version: $VERSION"
  fi

  # Check the existing installation
  check_existing_installation "$INSTALL_PATH"

  # Prepare the installation
  echo "Downloading opencode-sandbox $VERSION..."
  prepare_installation "$VERSION"

  local source_dir="${TEMP_DIR}/opencode-sandbox"

  # Install the files
  echo "Installing to: $INSTALL_PATH"
  install_files "$source_dir" "$INSTALL_PATH"

  # Extract the Go binary (--bin), before validation
  extract_go_binary "$INSTALL_PATH"

  # Set permissions
  set_executable_permissions "$INSTALL_PATH"

  # Validate the installation
  validate_installation "$INSTALL_PATH"

  # Version marker (the TUI's update menu reads it): rewrite the installed
  # version after every install/update
  printf '%s\n' "$DOWNLOADED_VERSION" > "$INSTALL_PATH/VERSION"

  # Create the convenience artifacts unless opted out via a flag
  if $SYMLINKS; then
    create_symlinks "$INSTALL_PATH"
  fi

  # Create the desktop shortcut unless opted out via a flag (failures never
  # abort the installation)
  if $SHORTCUT; then
    create_shortcut "$INSTALL_PATH"
  fi

  # Success message
  echo ""
  echo "==> Installation successful!"
  echo "    Path: $INSTALL_PATH"
  echo "    Version: $DOWNLOADED_VERSION"
  echo ""
  echo "Next steps:"
  echo "  1. Install podman (if not present yet):"
  echo "     sudo apt-get install -y podman pasta fuse-overlayfs slirp4netns"
  echo ""
  echo "  2. Start the interactive TUI:"
  echo "     via the Start menu  -> OC Sandbox"
  echo "     in a terminal       -> oc-sandbox  or from the install directory"
  echo "     ${INSTALL_PATH}/bin/${BINARY_NAME}"
  echo ""
  echo "  The TUI walks you through image builds and workspace creation."
  echo ""

  if $SYMLINK_CREATED; then
    echo ""
    echo "Symlink created in \$HOME/.local/bin/oc-sandbox (opt out: --no_symlinks)."
    echo "You can now start the sandbox from anywhere:"
    echo "  oc-sandbox"
  elif $SYMLINKS; then
    echo ""
    echo "The symlink could not be created — details above."
  else
    echo ""
    echo "Symlink not created (--no_symlinks). Start via:"
    echo "  ${INSTALL_PATH}/bin/${BINARY_NAME}"
  fi

  if $SHORTCUT_CREATED; then
    echo ""
    echo "Desktop shortcut created (opt out: --no_shortcut) — the TUI is now"
    echo "available via your Start menu/launcher (entry \"OC Sandbox\")."
  elif $SHORTCUT; then
    echo ""
    echo "The desktop shortcut could not be created — details above."
  else
    echo ""
    echo "Desktop shortcut not created (--no_shortcut)."
  fi
}

# --- Start ----------------------------------------------------------------------
# Start via file: BASH_SOURCE[0] == $0. Start via 'curl ... | bash': the script
# comes from stdin, BASH_SOURCE[0] is empty (and $0 is 'bash').
if [[ "${BASH_SOURCE[0]:-}" == "$0" ]] || [[ -z "${BASH_SOURCE[0]:-}" ]]; then
  main "$@"
fi
