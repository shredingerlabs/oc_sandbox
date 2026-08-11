#!/usr/bin/env bash
#
# start.sh — Start opencode-sandbox project
#
# This script is called by start-tui.sh to start an opencode-sandbox project.
# It handles container startup, environment setup, and opencode initialization.
#
# Usage:
#   scripts/start.sh <project-path> [--edition <edition>] [--use_proxy] [--offline] [--hil_mode] [--cbm_ui]
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Configuration -------------------------------------------------------------

DEFAULT_EDITION="full"
DEFAULT_WEB_ACCESS="unrestricted"
DEFAULT_CBM_UI=false

# --- Colors for output ---------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# --- Helper Functions ----------------------------------------------------------

log_info()  { printf '\033[1;34m[info]\033[0m %s\n'  "$*" >&2; }
log_warn()  { printf '\033[1;33m[warn]\033[0m %s\n'  "$*" >&2; }
log_error() { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; }
log_success() { printf '\033[0;32m✓\033[0m %s\n' "$*" >&2; }

show_usage() {
    cat << EOF
Usage: $0 <project-path> [options]

Arguments:
  project-path    Path to the opencode-sandbox project

Options:
  --edition <edition>    Sandbox edition (web|embedded|full) [default: full]
  --use_proxy           Use proxy for web access
  --offline              Work offline mode
  --hil_mode             Enable HIL mode
  --cbm_ui               Enable CBM UI

Examples:
  $0 ~/projects/my-project
  $0 ~/projects/my-project --edition web --use_proxy
  $0 ~/projects/my-project --edition embedded --offline --cbm_ui

EOF
}

# --- Argument Parsing -----------------------------------------------------------

parse_arguments() {
    if [[ $# -eq 0 ]]; then
        log_error "Missing project path argument"
        show_usage
        exit 1
    fi

    PROJECT_PATH="$1"
    shift

    OPT_EDITION="$DEFAULT_EDITION"
    OPT_USE_PROXY=false
    OPT_OFFLINE=false
    OPT_HIL_MODE=false
    OPT_CBM_UI="$DEFAULT_CBM_UI"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --edition)
                OPT_EDITION="$2"
                shift 2
                ;;
            --use_proxy)
                OPT_USE_PROXY=true
                shift
                ;;
            --offline)
                OPT_OFFLINE=true
                shift
                ;;
            --hil_mode)
                OPT_HIL_MODE=true
                shift
                ;;
            --cbm_ui)
                OPT_CBM_UI=true
                shift
                ;;
            --help|-h)
                show_usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
        esac
    done

    # Validate edition
    case "$OPT_EDITION" in
        web|embedded|full)
            ;;
        *)
            log_error "Invalid edition: $OPT_EDITION (must be web|embedded|full)"
            exit 1
            ;;
    esac

    log_info "Starting project with configuration:"
    log_info "  Edition: $OPT_EDITION"
    log_info "  Use proxy: $OPT_USE_PROXY"
    log_info "  Offline: $OPT_OFFLINE"
    log_info "  HIL mode: $OPT_HIL_MODE"
    log_info "  CBM UI: $OPT_CBM_UI"
}

# --- Project Validation ---------------------------------------------------------

validate_project() {
    local project_path="$1"

    if [[ ! -d "$project_path" ]]; then
        log_error "Project path does not exist: $project_path"
        exit 1
    fi

    # Check for expected project structure
    local required_dirs=(
        "project"
        ".opencode_config"
        ".opencode_data"
    )

    for dir in "${required_dirs[@]}"; do
        if [[ ! -d "$project_path/$dir" ]]; then
            log_warn "Missing expected directory: $dir"
            log_warn "Project structure may be incomplete"
        fi
    done

    # Check for auth.json
    if [[ ! -f "$project_path/.opencode_data/auth.json" ]]; then
        log_error "Missing auth.json: $project_path/.opencode_data/auth.json"
        log_error "Please run init-project.sh first to set up authentication"
        exit 1
    fi
}

# --- Container Management -------------------------------------------------------

detect_container_runtime() {
    if command -v podman &>/dev/null; then
        echo "podman"
    elif command -v docker &>/dev/null; then
        echo "docker"
    else
        echo ""
    fi
}

get_container_name() {
    local project_name
    project_name="$(basename "$PROJECT_PATH")"
    echo "opencode-sandbox-${project_name}"
}

is_container_running() {
    local runtime="$1"
    local container_name="$2"

    case "$runtime" in
        podman)
            podman container exists "${container_name}" 2>/dev/null && \
            podman inspect -f '{{.State.Running}}' "${container_name}" 2>/dev/null | grep -q true
            ;;
        docker)
            docker container exists "${container_name}" 2>/dev/null && \
            docker inspect -f '{{.State.Running}}' "${container_name}" 2>/dev/null | grep -q true
            ;;
        *)
            false
            ;;
    esac
}

start_container() {
    local runtime="$1"
    local container_name="$2"

    log_info "Checking container status..."

    if is_container_running "$runtime" "$container_name"; then
        log_success "Container is already running: $container_name"
        return 0
    fi

    log_info "Starting container: $container_name"

    # This is a placeholder for container startup logic
    # In production, this would use the actual opencode-sandbox container
    log_warn "Container startup not yet implemented"
    log_info "Replace this with your container startup logic"

    # Example of what the actual implementation might look like:
    # case "$runtime" in
    #     podman)
    #         podman start "$container_name"
    #         ;;
    #     docker)
    #         docker start "$container_name"
    #         ;;
    # esac

    return 0
}

# --- OpenCode Initialization ---------------------------------------------------

setup_opencode_environment() {
    local project_path="$1"

    log_info "Setting up opencode environment..."

    # Set environment variables based on options
    export OPENCODE_DIR="$project_path/.opencode_config"
    export OPENCODE_DATA_DIR="$project_path/.opencode_data"
    export OPENCODE_PROJECT_DIR="$project_path/project"
    export OPENCODE_SSH_DIR="$project_path/.ssh_local"
    export OPENCODE_GIT_DIR="$project_path/.git_local"
    export OPENCODE_CBM_CACHE_DIR="$project_path/.cbm_cache"

    # Set CBM UI flag
    if [[ "$OPT_CBM_UI" == "true" ]]; then
        export OPENCODE_CBM_UI=true
    fi

    # Set edition-specific configuration
    case "$OPT_EDITION" in
        web)
            export OPENCODE_EDITION=web
            ;;
        embedded)
            export OPENCODE_EDITION=embedded
            ;;
        full)
            export OPENCODE_EDITION=full
            ;;
    esac

    # Set web access mode
    if [[ "$OPT_USE_PROXY" == "true" ]]; then
        export OPENCODE_WEB_ACCESS=proxy
    elif [[ "$OPT_OFFLINE" == "true" ]]; then
        export OPENCODE_WEB_ACCESS=offline
    else
        export OPENCODE_WEB_ACCESS=unrestricted
    fi

    # Set HIL mode
    if [[ "$OPT_HIL_MODE" == "true" ]]; then
        export OPENCODE_HIL_MODE=true
    fi

    log_success "Environment configured successfully"
}

start_opencode() {
    local project_path="$1"

    log_info "Starting opencode..."

    if ! command -v opencode &>/dev/null; then
        log_error "opencode command not found"
        log_error "Please install opencode first"
        exit 1
    fi

    # Check if opencode is already running for this project
    if opencode status "$project_path" &>/dev/null; then
        log_success "opencode is already running for this project"
        return 0
    fi

    # Start opencode in the project directory
    cd "$project_path/project" || {
        log_error "Failed to change to project directory: $project_path/project"
        exit 1
    }

    log_info "Starting opencode in directory: $(pwd)"

    # Placeholder for actual opencode startup
    log_warn "OpenCode startup not yet implemented"
    log_info "Replace this with your opencode startup logic"

    # Example of what the actual implementation might look like:
    # opencode start \
    #     --config "$ OPENCODE_DIR" \
    #     --data-dir "$OPENCODE_DATA_DIR" \
    #     --auth "$OPENCODE_DATA_DIR/auth.json"

    cd - >/dev/null || true

    return 0
}

# --- Main Execution -------------------------------------------------------------

main() {
    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║              OpenCode Sandbox - Start Project             ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""

    # Parse command line arguments
    parse_arguments "$@"

    # Validate project structure
    validate_project "$PROJECT_PATH"

    # Detect container runtime
    local runtime
    runtime=$(detect_container_runtime)

    if [[ -z "$runtime" ]]; then
        log_error "No container runtime found (podman or docker required)"
        log_info "Please install podman or docker before continuing"
        exit 1
    fi

    log_info "Using container runtime: $runtime"

    # Get container name
    local container_name
    container_name=$(get_container_name)

    # Start container if needed
    start_container "$runtime" "$container_name"

    # Setup opencode environment
    setup_opencode_environment "$PROJECT_PATH"

    # Start opencode
    start_opencode "$PROJECT_PATH"

    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║                    Project Started!                        ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Project: $PROJECT_PATH"
    echo "Container: $container_name"
    echo "Edition: $OPT_EDITION"
    echo ""
    echo "You can now use opencode from this project directory."
    echo ""
}

# --- Entry Point ---------------------------------------------------------------

main "$@"