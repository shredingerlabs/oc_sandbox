#!/usr/bin/env bash

set -euo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

error() {
    echo -e "${RED}Error:${NC} $1" >&2
    exit 1
}

success() {
    echo -e "${GREEN}Success:${NC} $1"
}

warn() {
    echo -e "${YELLOW}Warning:${NC} $1"
}

# Check gh CLI authentication
check_gh_auth() {
    if ! command -v gh &> /dev/null; then
        error "GitHub CLI (gh) not installed. Install from https://cli.github.com/"
    fi
    
    if ! gh auth status &> /dev/null; then
        error "GitHub CLI not authenticated. Run 'gh auth login' first."
    fi
    
    success "GitHub CLI authenticated"
}

# Check dist folder
check_dist_folder() {
    if [ ! -d "dist" ]; then
        error "dist/ directory not found in current directory"
    fi
    
    if [ -z "$(ls -A dist)" ]; then
        error "dist/ directory is empty"
    fi
    
    local file_count=$(find dist -type f | wc -l)
    success "dist/ directory found with $file_count files"
}

# Check git status
check_git_status() {
    local status=$(git status --porcelain 2>/dev/null || echo "")
    
    if [ -n "$status" ]; then
        warn "Working directory has uncommitted changes:"
        echo "$status"
        echo ""
        
        read -p "Do you want to continue anyway? (y/N): " proceed
        if [[ ! "$proceed" =~ ^[Yy]$ ]]; then
            error "Release cancelled by user"
        fi
    fi
    
    local current_branch=$(git branch --show-current)
    success "Current branch: $current_branch"
}

# Get current directory for cleanup
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Find project root (look for .git directory or go up until we find dist/scripts)
if [[ "$SCRIPT_DIR" == *"dist/scripts"* ]]; then
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../" && pwd)"
else
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
fi

# Check for existing tags
check_existing_tags() {
    local existing_tags=$(git tag --list | grep "^$version$" || true)
    
    if [ -n "$existing_tags" ]; then
        error "Tag $version already exists. Use a different version."
    fi
}

# Clean up old release branches
cleanup_old_branches() {
    local current_branch=$(git branch --show-current)
    
    # Get all release-dist branches, excluding current one
    local branches=$(git branch --list 'release-dist-*' 2>/dev/null | grep -v "$current_branch" || true)
    
    if [ -n "$branches" ]; then
        local branch_count=$(echo "$branches" | wc -l)
        warn "Found $branch_count old release branch(es):"
        echo "$branches"
        echo ""
        
        read -p "Clean up old release branches? (y/N): " cleanup
        if [[ "$cleanup" =~ ^[Yy]$ ]]; then
            while IFS= read -r branch; do
                if [ -n "$branch" ]; then
                    echo "Deleting branch: $branch"
                    git branch -D "$branch" 2>/dev/null || warn "Failed to delete $branch"
                fi
            done <<< "$branches"
            success "Cleaned up old branches"
        fi
    fi
}

# Main execution
main() {
    cd "$PROJECT_ROOT"
    
    # Pre-flight checks
    check_gh_auth
    check_dist_folder
    check_git_status
    
    # Get version tag
    if [ -n "${1:-}" ]; then
        version="$1"
    else
        read -p "Enter version tag (e.g., v1.0.0): " version
    fi
    
    # Validate version format
    if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
        error "Invalid version format. Expected format: v1.2.3"
    fi
    
    check_existing_tags
    success "Version tag: $version"
    
    # Get release title
    if [ -n "${2:-}" ]; then
        title="$2"
    else
        read -p "Enter release title (optional, press Enter to use version): " title
    fi
    if [ -z "$title" ]; then
        title="Release $version"
    fi
    
    # Get release notes
    if [ -n "${3:-}" ]; then
        release_notes="$3"
    else
        echo "Enter release notes (Ctrl+D when done):"
        release_notes=""
        while IFS= read -r line; do
            release_notes+="$line"$'\n'
        done
    fi
    
    if [ -z "$release_notes" ]; then
        warn "No release notes provided"
        release_notes="Release $version from dist/ folder"
    fi
    
    # Pre-release check
    local is_pre_release=false
    local pre_release=""
    
    if [ -n "${4:-}" ]; then
        is_pre_release="$4"
    else
        read -p "Mark as pre-release? (y/N): " pre_release
        
        if [[ "$pre_release" =~ ^[Yy]$ ]]; then
            is_pre_release=true
            warn "This will be marked as a pre-release"
        fi
    fi
    
    # Clean up old branches first
    cleanup_old_branches
    
    # Confirm before proceeding
    echo ""
    echo "=== Release Summary ==="
    echo "Version: $version"
    echo "Title: $title"
    echo "Pre-release: $is_pre_release"
    echo "Source: dist/ folder"
    echo ""
    
    read -p "Proceed with release creation? (y/N): " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        error "Release cancelled by user"
    fi
    
    # Create temporary orphan branch
    local temp_branch="release-dist-${version}"
    success "Creating temporary orphan branch: $temp_branch"
    
    git checkout --orphan "$temp_branch"
    git rm -rf . 2>/dev/null || true
    
    # Copy dist contents from project root
    cp -r "${PROJECT_ROOT}/dist/"* . 2>/dev/null || true
    cp -r "${PROJECT_ROOT}/dist/".* . 2>/dev/null || true
    rm -rf dist 2>/dev/null || true
    
    git add .
    
    # Commit with release info
    local commit_msg="Release $version from dist/ folder
    
Release generated from dist/ contents."
    
    git commit -m "$commit_msg"
    success "Created release commit"
    
    # Tag the commit
    git tag "$version"
    success "Created tag: $version"
    
    # Push to GitHub
    success "Pushing to GitHub..."
    git push origin "$temp_branch"
    git push origin "$version"
    success "Pushed branch and tag"
    
    # Create GitHub release
    local release_flags=("--draft" "--title" "$title" "--notes" "$release_notes")
    
    if [ "$is_pre_release" = true ]; then
        release_flags+=("--prerelease")
    fi
    
    success "Creating GitHub release..."
    local release_url=$(gh release create "$version" "${release_flags[@]}")
    
    # Return to original branch
    local original_branch=$(git rev-parse --abbrev-ref HEAD@{1} 2>/dev/null || echo "main")
    git checkout "$original_branch"
    
    # Clean up temporary branch locally
    git branch -D "$temp_branch" 2>/dev/null || true
    success "Cleaned up temporary branch"
    
    # Final summary
    echo ""
    echo "=== Release Created Successfully ==="
    echo "Version: $version"
    echo "Release URL: $release_url"
    echo "Status: Draft (needs manual publishing)"
    echo ""
    echo "Next steps:"
    echo "1. Review the release at: $release_url"
    echo "2. Click 'Publish release' when ready"
    echo "3. The release will contain only files from the dist/ folder"
}

# Run main function with arguments
main "$@"