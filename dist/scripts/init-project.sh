#!/usr/bin/env bash
#
# init-project.sh — Initialize new opencode-sandbox project
#
# This script creates a new opencode-sandbox project with all necessary
# directories, configuration files, and authentication setup.
#
# Usage:
#   scripts/init-project.sh --path <project-path> --name <project-name> \
#     [--git-host <host>] [--git-user <user>] [--git-email <email>] \
#     [--api-provider <provider>] [--api-token <token>]
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

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
Usage: $0 --path <project-path> --name <project-name> [options]

Required Arguments:
  --path <project-path>           Path where to create the project
  --name <project-name>           Name for the project

Options:
  --git-host <host>             Git host provider (github|gitlab|other) [default: github]
  --git-user <user>             Git user.name
  --git-email <email>           Git user.email
  --api-provider <provider>     AI API provider (gwdg-saia|other) [default: gwdg-saia]
  --api-token <token>           API token for the provider
  --custom-provider <name>      Custom provider name (required if api-provider is 'other')

Examples:
  $0 --path ~/projects/my-project --name my-project
  $0 --path ~/projects/my-project --name my-project --api-provider gwdg-saia --api-token "your-token"
  $0 --path ~/projects/my-project --name my-project --api-provider other --custom-provider "openai" --api-token "sk-..."

EOF
}

# --- Argument Parsing -----------------------------------------------------------

parse_arguments() {
    if [[ $# -eq 0 ]]; then
        log_error "Missing required arguments"
        show_usage
        exit 1
    fi

    OPT_PATH=""
    OPT_NAME=""
    OPT_GIT_HOST="github"
    OPT_GIT_USER=""
    OPT_GIT_EMAIL=""
    OPT_API_PROVIDER="gwdg-saia"
    OPT_API_TOKEN=""
    OPT_CUSTOM_PROVIDER=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --path)
                OPT_PATH="$2"
                shift 2
                ;;
            --name)
                OPT_NAME="$2"
                shift 2
                ;;
            --git-host)
                OPT_GIT_HOST="$2"
                shift 2
                ;;
            --git-user)
                OPT_GIT_USER="$2"
                shift 2
                ;;
            --git-email)
                OPT_GIT_EMAIL="$2"
                shift 2
                ;;
            --api-provider)
                OPT_API_PROVIDER="$2"
                shift 2
                ;;
            --api-token)
                OPT_API_TOKEN="$2"
                shift 2
                ;;
            --custom-provider)
                OPT_CUSTOM_PROVIDER="$2"
                shift 2
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

    # Validate required arguments
    if [[ -z "$OPT_PATH" ]]; then
        log_error "Missing required argument: --path"
        show_usage
        exit 1
    fi

    if [[ -z "$OPT_NAME" ]]; then
        log_error "Missing required argument: --name"
        show_usage
        exit 1
    fi

    # Validate git host
    case "$OPT_GIT_HOST" in
        github|gitlab|other)
            ;;
        *)
            log_error "Invalid git host: $OPT_GIT_HOST (must be github|gitlab|other)"
            exit 1
            ;;
    esac

    # Validate API provider
    case "$OPT_API_PROVIDER" in
        gwdg-saia|other)
            ;;
        *)
            log_error "Invalid API provider: $OPT_API_PROVIDER (must be gwdg-saia|other)"
            exit 1
            ;;
    esac

    # Validate custom provider requirements
    if [[ "$OPT_API_PROVIDER" == "other" ]]; then
        if [[ -z "$OPT_CUSTOM_PROVIDER" ]]; then
            log_error "Custom provider name required when --api-provider is 'other'"
            log_error "Use --custom-provider <name> to specify the provider name"
            exit 1
        fi
    fi

    # Validate API token requirement
    if [[ "$OPT_API_PROVIDER" == "gwdg-saia" || "$OPT_API_PROVIDER" == "other" ]]; then
        if [[ -z "$OPT_API_TOKEN" ]]; then
            log_error "API token is required"
            log_error "Use --api-token <token> to provide your API token"
            exit 1
        fi
    fi
}

# --- Project Structure Creation ------------------------------------------------

create_project_structure() {
    local project_path="$1"

    log_info "Creating project directory structure at: $project_path"

    # Create main project directories
    mkdir -p \
        "${project_path}/project" \
        "${project_path}/.opencode_config" \
        "${project_path}/.opencode_data" \
        "${project_path}/.ssh_local" \
        "${project_path}/.git_local" \
        "${project_path}/.cbm_cache"

    # Set restrictive permissions for sensitive directories
    chmod 700 "${project_path}/.ssh_local"
    chmod 700 "${project_path}/.git_local"
    chmod 700 "${project_path}/.opencode_data"

    log_success "Project structure created"
}

# --- Git Configuration ---------------------------------------------------------

setup_git_config() {
    local project_path="$1"
    local git_host="$2"
    local git_user="$3"
    local git_email="$4"

    log_info "Setting up git configuration..."

    local git_config="${project_path}/.git_local/gitconfig"
    
    # Create git config
    cat > "$git_config" << EOF
[init]
    defaultBranch = main
[user]
${git_user:+    name = ${git_user}}
${git_email:+    email = ${git_email}}
[core]
    excludesfile = .gitignore
    sshCommand = ssh -F "${project_path}/.ssh_local/config"
[http]
    sslVerify = true
[https]
    sslVerify = true
EOF

    # Create gitignore
    cat > "${project_path}/project/.gitignore" << 'EOF'
# Node modules
node_modules/
npm-debug.log*
yarn-debug.log*
yarn-error.log*

# Environment files
.env
.env.local
.env.*.local

# IDE
.vscode/
.idea/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# OpenCode sandbox
.opencode_data/
.cbm_cache/
.ssh_local/
.git_local/

# Build output
dist/
build/
*.log
EOF

    log_success "Git configuration created"

    # Handle git host-specific templates
    case "$git_host" in
        github)
            log_info "Setting up GitHub CLI configuration..."
            # Would copy templates/git_local/gh-cli/config.yml in production
            mkdir -p "${project_path}/.git_local/gh-cli"
            log_warn "GitHub CLI template placeholder - implement in production"
            ;;
        gitlab)
            log_info "Setting up GitLab CLI configuration..."
            # Would copy templates/git_local/glab-cli/config.yml in production
            mkdir -p "${project_path}/.git_local/glab-cli"
            log_warn "GitLab CLI template placeholder - implement in production"
            ;;
        other)
            log_info "Using basic git configuration..."
            ;;
    esac
}

setup_ssh_config() {
    local project_path="$1"

    log_info "Setting up SSH configuration..."

    local ssh_config="${project_path}/.ssh_local/config"

    cat > "$ssh_config" << 'EOF'
# OpenCode Sandbox SSH Configuration
# This file configures SSH connections for git operations

# Default settings
Host *
    StrictHostKeyChecking accept-new
    UserKnownHostsFile ~/.ssh/known_hosts
    IdentitiesOnly yes

# GitHub
Host github.com
    Hostname github.com
    User git
    Port 22
    IdentityFile ~/.ssh/id_ed25519

# GitLab
Host gitlab.com
    Hostname gitlab.com
    User git
    Port 22
    IdentityFile ~/.ssh/id_ed25519
EOF

    log_success "SSH configuration created"
}

# --- OpenCode Configuration ---------------------------------------------------

setup_opencode_config() {
    local project_path="$1"
    local api_provider="$2"
    local custom_provider="$3"

    log_info "Setting up opencode configuration..."

    local opencode_dir="${project_path}/.opencode_config"

    # Create AGENTS.md placeholder
    cat > "${opencode_dir}/AGENTS.md" << 'EOF'
# OpenCode Agents Configuration

This directory contains custom agent configurations for OpenCode.

## Usage
- Place agent skill files in the `skills/` subdirectory
- Custom agent behaviors can be configured here
EOF

    # Create skills directory
    mkdir -p "${opencode_dir}/skills"

    # Create opencode.json configuration based on API provider
    local opencode_config="${opencode_dir}/opencode.json"

    case "$api_provider" in
        gwdg-saia)
            cat > "$opencode_config" << EOF
{
  "version": "1.0.0",
  "api": {
    "provider": "gwdg-saia",
    "baseUrl": "https://api.gwdg-saia.de/v1",
    "model": "gwdg-saia/glm-4.7"
  },
  "features": {
    "codeSearch": true,
    "codeGeneration": true,
    "codeReview": true
  }
}
EOF
            ;;
        other)
            cat > "$opencode_config" << EOF
{
  "version": "1.0.0",
  "api": {
    "provider": "$custom_provider",
    "enabled": true
  },
  "features": {
    "codeSearch": true,
    "codeGeneration": true,
    "codeReview": true
  }
}
EOF
            ;;
    esac

    # Create scripts directory with start-tui.sh
    local scripts_dir="${project_path}/.opencode_sandbox/scripts"
    mkdir -p "$scripts_dir"

    # Create symlink to the main start-tui.sh
    if [[ -f "${SCRIPT_DIR}/start-tui.sh" ]]; then
        ln -sf "${SCRIPT_DIR}/start-tui.sh" "${scripts_dir}/start-tui.sh"
        log_success "Linked start-tui.sh to project scripts"
    fi

    log_success "OpenCode configuration created"
}

# --- Authentication Setup ------------------------------------------------------

setup_authentication() {
    local project_path="$1"
    local api_provider="$2"
    local api_token="$3"
    local custom_provider="$4"

    log_info "Setting up authentication..."

    local auth_file="${project_path}/.opencode_data/auth.json"

    # Determine provider name
    local provider_name="$api_provider"
    if [[ "$api_provider" == "other" ]]; then
        provider_name="$custom_provider"
    fi

    # Create auth.json with proper permissions
    cat > "$auth_file" << EOF
{
  "$provider_name": {
    "apiKey": "$api_token",
    "type": "api_key"
  }
}
EOF

    # Set restrictive permissions
    chmod 600 "$auth_file"

    log_success "Authentication configured for provider: $provider_name"

    # Display warning about security
    log_warn "Authentication file created with restricted permissions (600)"
    log_warn "Never commit auth.json to version control"
}

# --- CBM Configuration ---------------------------------------------------------

setup_cbm_config() {
    local project_path="$1"

    log_info "Setting up CBM cache configuration..."

    local cbm_dir="${project_path}/.cbm_cache"

    # Create CBM cache directory
    mkdir -p "$cbm_dir"

    # Create CBM configuration file
    cat > "${cbm_dir}/config.json" << 'EOF'
{
  "version": "1.0.0",
  "cache": {
    "enabled": true,
    "maxSize": "500m"
  },
  "indexing": {
    "autoIndex": true,
    "autoWatch": true
  }
}
EOF

    log_success "CBM cache configured"
}

# --- Project Registration ------------------------------------------------------

register_project() {
    local project_path="$1"
    local project_name="$2"

    log_info "Registering project in project registry..."

    local registry_dir="${HOME}/.config/opencode-sandbox"
    local registry_file="${registry_dir}/projects.json"

    mkdir -p "$registry_dir"

    if [[ ! -f "$registry_file" ]]; then
        echo '{}' > "$registry_file"
    fi

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Update registry
    local current
    current=$(cat "$registry_file")

    if ! echo "$current" | jq -e ".paths" &>/dev/null; then
        current='{"paths":[]}'
    fi

    echo "$current" | jq --arg path "$project_path" --arg name "$project_name" --arg ts "$timestamp" '
        if .paths | map(.path) | index($path) then
            .
        else
            .paths += [{
                "path": $path,
                "name": $name,
                "created": $ts,
                "last_used": $ts
            }]
        end
    ' > "${registry_file}.tmp"

    mv "${registry_file}.tmp" "$registry_file"

    log_success "Project registered in registry"
}

# --- Main Execution -------------------------------------------------------------

main() {
    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║           OpenCode Sandbox - Initialize Project           ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""

    # Parse command line arguments
    parse_arguments "$@"

    log_info "Initializing project:"
    log_info "  Path: $OPT_PATH"
    log_info "  Name: $OPT_NAME"
    log_info "  Git Host: $OPT_GIT_HOST"
    log_info "  API Provider: $OPT_API_PROVIDER"
    echo ""

    # Check if project path exists
    if [[ -e "$OPT_PATH" ]]; then
        log_error "Project path already exists: $OPT_PATH"
        log_error "Please choose a different path or remove the existing directory"
        exit 1
    fi

    # Create project structure
    create_project_structure "$OPT_PATH"

    # Setup git configuration
    setup_git_config "$OPT_PATH" "$OPT_GIT_HOST" "$OPT_GIT_USER" "$OPT_GIT_EMAIL"

    # Setup SSH configuration
    setup_ssh_config "$OPT_PATH"

    # Setup opencode configuration
    setup_opencode_config "$OPT_PATH" "$OPT_API_PROVIDER" "$OPT_CUSTOM_PROVIDER"

    # Setup authentication
    setup_authentication "$OPT_PATH" "$OPT_API_PROVIDER" "$OPT_API_TOKEN" "$OPT_CUSTOM_PROVIDER"

    # Setup CBM configuration
    setup_cbm_config "$OPT_PATH"

    # Register project
    register_project "$OPT_PATH" "$OPT_NAME"

    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║                 Project Initialized!                      ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Project: $OPT_NAME"
    echo "Location: $OPT_PATH"
    echo ""
    echo "Next steps:"
    echo "  1. Navigate to project: cd $OPT_PATH/project"
    echo "  2. Initialize git repository: git init"
    echo "  3. Run TUI: cd $OPT_PATH && scripts/start-tui.sh"
    echo "  4. Start the project and connect to your remote repository"
    echo ""
    echo "Important security notes:"
    echo "  - .opencode_data/auth.json contains sensitive API credentials"
    echo "  - Never commit .opencode_data/ directory to version control"
    echo "  - .ssh_local/ and .git_local/ contain sensitive SSH config"
    echo ""
}

# --- Entry Point ---------------------------------------------------------------

main "$@"