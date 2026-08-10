# fzf Availability Research

**Issue**: #3  
**Date**: 2026-08-10  
**Status**: Complete

## Executive Summary

fzf is **widely available** across all target distributions (Debian, Ubuntu, Fedora, openSUSE) in their default repositories. However, scripts should implement graceful degradation for systems without fzf installed, particularly in minimal/container environments and CI/CD pipelines.

---

## Distribution Availability

### Default Repository Status

| Distribution | Package Name | Available Since | Current Version (2026) |
|-------------|--------------|-----------------|------------------------|
| **Debian** | `fzf` | Debian 9+ (Stretch) | 0.74.1 (Sid/Trixie) |
| **Ubuntu** | `fzf` | 19.10+ (Eoan) | 0.60.3 (24.04 LTS) |
| **Fedora** | `fzf` | Long-standing | 0.74.2 (Rawhide) |
| **openSUSE** | `fzf` | Long-standing | 0.74.0 (Tumbleweed) |

### Installation Commands

```bash
# Debian/Ubuntu
sudo apt install fzf

# Fedora
sudo dnf install fzf

# openSUSE
sudo zypper install fzf

# Arch Linux
sudo pacman -S fzf

# Alpine Linux
sudo apk add fzf
```

---

## Minimum Version Requirements

### For Menu Selection (`--select-1`, `--exit-0`, `-m`)

**Minimum version: 0.17.0** (2017)

All target distributions exceed this requirement:
- Debian 11 (Bullseye): 0.24.3 ✓
- Debian 12 (Bookworm): 0.38.0 ✓
- Ubuntu 22.04 LTS: 0.35.1 ✓
- Ubuntu 24.04 LTS: 0.60.3 ✓
- Fedora 39+: 0.42.0+ ✓
- openSUSE Leap 15.5+: 0.37.0+ ✓

**Conclusion**: No version concerns for basic menu selection on supported distributions.

---

## Known Issues in Non-Interactive/CI Environments

### 1. TTY Detection

fzf requires a TTY by default. In CI environments or when stdin is piped without a terminal:

```bash
# Will fail in CI without TTY
echo -e "option1\noption2" | fzf

# Solution: Use --height or detect TTY
echo -e "option1\noption2" | fzf --height 100% || echo "fallback"
```

### 2. Exit Codes

- **Exit 0**: Selection made
- **Exit 1**: No match or aborted (Ctrl-C)
- **Exit 130**: Interrupted

Scripts must handle non-zero exits gracefully.

### 3. Common CI Issues

| Issue | Symptom | Workaround |
|-------|---------|------------|
| No TTY | "failed to get terminal size" | Use `--height` option or `script -qec` |
| Non-interactive | Immediate exit | Check `$FZF_DEFAULT_COMMAND` not set to interactive tools |
| Color codes | ANSI escape in logs | Use `--color=-1` to disable |

### 4. Best Practices for CI

```bash
# Detect if fzf is available and running interactively
if command -v fzf >/dev/null 2>&1 && [[ -t 0 ]]; then
    # Interactive with fzf
    selection=$(echo "$options" | fzf)
else
    # Fallback for CI or no fzf
    selection=$(echo "$options" | head -1)
fi
```

---

## Fallback Patterns

### Pattern 1: Pure Bash `select` (Recommended)

**Pros**: POSIX-compatible, no dependencies, works everywhere  
**Cons**: Less UX, numbered list only

```bash
select_option() {
    local options=("$@")
    local prompt="${1:-Select an option:}"
    local result
    
    # Try fzf first
    if command -v fzf >/dev/null 2>&1 && [[ -t 0 ]]; then
        result=$(printf '%s\n' "${options[@]}" | fzf --height 40% --reverse)
    else
        # Fallback to bash select
        PS3="$prompt "
        select opt in "${options[@]}"; do
            [[ -n $opt ]] && result="$opt" && break
        done
    fi
    
    echo "$result"
}
```

### Pattern 2: dialog/whiptail

**Pros**: TUI menus, works over SSH  
**Cons**: Requires dialog package, more verbose

```bash
# Requires: dialog or whiptail
if command -v dialog >/dev/null 2>&1; then
    options=$(printf '%s\n' "${items[@]}")
    result=$(echo "$options" | dialog --menu "$prompt" 0 0 0 2>&1 >/dev/tty)
fi
```

### Pattern 3: Graceful Degradation (Recommended for Scripts)

```bash
#!/usr/bin/env bash

# Detection function
has_fzf() {
    command -v fzf >/dev/null 2>&1 && [[ -t 0 ]]
}

# Menu function with fallback
menu_select() {
    local prompt="$1"
    shift
    local options=("$@")
    
    if has_fzf; then
        printf '%s\n' "${options[@]}" | fzf \
            --height 40% \
            --reverse \
            --border \
            --prompt="$prompt " \
            --exit-0 \
            --select-1
    else
        # Pure bash fallback
        echo "$prompt" >&2
        for i in "${!options[@]}"; do
            echo "  $((i+1))) ${options[$i]}" >&2
        done
        local choice
        read -r -p "Enter choice (1-${#options[@]}): " choice >&2
        [[ $choice =~ ^[0-9]+$ ]] && echo "${options[$((choice-1))]}"
    fi
}
```

### Pattern 4: Environment Variable Override

```bash
# Allow users to override the selection method
MENU_TOOL="${MENU_TOOL:-auto}"

case "$MENU_TOOL" in
    fzf)
        use_fzf=true
        ;;
    bash|select)
        use_fzf=false
        ;;
    auto|*)
        use_fzf=$(command -v fzf >/dev/null 2>&1 && [[ -t 0 ]])
        ;;
esac
```

---

## Recommended Detection Code

```bash
#!/usr/bin/env bash
# fzf detection and fallback helper

# Check if fzf is available and usable
fzf_available() {
    # Check command exists
    command -v fzf >/dev/null 2>&1 || return 1
    
    # Check running interactively (not in CI)
    [[ -t 0 ]] || return 1
    
    # Optional: check minimum version
    local version
    version=$(fzf --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
    [[ -n $version ]] || return 0  # Accept if can't parse version
    
    # Compare versions (requires bash 4+)
    local min_version="0.17.0"
    [[ "$(printf '%s\n' "$min_version" "$version" | sort -V | head -1)" == "$min_version" ]]
}

# Usage example
if fzf_available; then
    FZF_CMD="fzf --height 40% --reverse --border"
else
    FZF_CMD=""
    echo "Note: fzf not available, using fallback mode" >&2
fi

# Later in script:
if [[ -n $FZF_CMD ]]; then
    selection=$(echo "$options" | eval "$FZF_CMD")
else
    # Fallback to bash select
    PS3="Select option: "
    select opt in $options; do
        [[ -n $opt ]] && selection="$opt" && break
    done
fi
```

---

## Recommendations

### For Interactive Scripts
1. **Default to fzf** when available and running interactively
2. **Fallback to bash `select`** for maximum portability
3. **Document the fallback** in script help output

### For CI/CD Scripts
1. **Never require fzf** in CI environments
2. **Use `--exit-0 --select-1`** for single-choice automation
3. **Provide command-line flags** to bypass interactive selection

### For Distribution Packages
1. **Recommend fzf** in package dependencies (not required)
2. **Suggest installation** in post-install message
3. **Ensure graceful degradation** without fzf installed

---

## References

- fzf GitHub: https://github.com/junegunn/fzf
- Repology fzf versions: https://repology.org/project/fzf/versions
- ArchWiki fzf: https://wiki.archlinux.org/title/fzf
- Debian Package: https://packages.debian.org/sid/fzf
- Fedora Package: https://packages.fedoraproject.org/pkgs/fzf/

---

## Conclusion

fzf is **safe to use as a preferred interface** on Debian, Ubuntu, Fedora, and openSUSE, with all target distributions shipping recent versions in their default repositories. However, production scripts **must implement fallback patterns** for:

1. Minimal/container environments without fzf
2. CI/CD pipelines without TTY
3. Users who choose not to install optional dependencies

The **pure bash `select` fallback** provides the best balance of portability and functionality for graceful degradation.
