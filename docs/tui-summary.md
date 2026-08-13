# TUI Design Session Summary

## Overview
Comprehensive specification created for OpenCode Sandbox TUI system using grill-with-docs skill. The specification addresses user interface design, configuration management, container operations, and implementation details.

## Key Architectural Decisions

### 1. **Three-Tier Configuration Architecture**
- **global_config.json**: User preferences and default paths
- **projects.json**: Project registry with metadata and ordering
- **sandbox_config.json**: Per-project settings and state tracking

### 2. **Concurrent Container Support**
- Projects run in isolated containers (opencode-sandbox-PROJECTNAME)
- Running containers accessed via podman exec
- Multiple projects can operate simultaneously

### 3. **Separated Integration Concerns**
- VCS integration (GitHub, public/self-hosted GitLab, or other host) stored in .git_local/
- AI providers (GWDG) configured via auth.json in .opencode_data/
- Each project supports one VCS option plus optional AI provider
- Non-none VCS choices configure only project-local `user.name` and `user.email`.

### 4. **Enhanced Scripts Over Replacement**
- Existing scripts used as subprocesses
- Additional parameters like --start_opencode for start.sh
- Maintains backward compatibility and code consistency

### 5. **Runtime Discovery Approach**
- Available editions/modes parsed from script help text
- No hardcoded lists, automatically adapts to script changes
- Future-proof integration

### 6. **Atomic Configuration with Backups**
- Temporary file + rename operations prevent corruption
- Automatic backup rotation (last 5 backups)
- Explicit backup/restore in TUI settings

## ADRs Created

1. **0002-tui-user-interface.md** - Gum-based TUI with bash fallback
2. **0003-per-project-config-storage.md** - Three-tier configuration architecture
3. **0004-concurrent-container-execution.md** - Multi-project container support
4. **0005-first-run-setup-automation.md** - Automated setup with state tracking
5. **0006-vcs-vs-ai-provider-separation.md** - Separated VCS and AI configuration
6. **0007-native-script-enhancement.md** - Script enhancement strategy
7. **0008-runtime-detection-editions-modes.md** - Dynamic discovery approach
8. **0009-atomic-config-with-backups.md** - Configuration safety strategy

## Domain Model Updates

CONTEXT.md expanded with 8 new domain-specific terms:
- TUI (Text-based User Interface)
- sandbox_config.json, projects.json, global_config.json
- concurrent containers, first-run setup
- VCS integration, AI provider
- setup-complete marker, atomic config write
- runtime detection, native script enhancement

## Implementation Specification

Complete technical specification covering:

### Architecture
- File structure and component organization
- Configuration schemas with JSON examples
- State machine diagram showing TUI flow

### Core Functions
- TUI initialization and mode detection
- First run setup wizard
- Main menu and project selection
- New project wizard with multi-step flow
- Container operations (start/access/build)
- Failed-start recovery with Retry, Go Back, and explicit Exit choices
- Retry settings are prefilled and persisted only after credential setup succeeds
- First-run setup automation
- Configuration management utilities

### Advanced Features
- Signal handling and cleanup
- Terminal resize adaptation
- Concurrent operations warning
- Comprehensive error handling

### Integration
- Installation via existing install.sh
- Symlink support
- Testing strategy outline
- Future enhancement roadmap

## Design Principles

1. **User Experience First**: Modern, easy-to-use interface
2. **Backward Compatibility**: Existing scripts remain functional
3. **Resilience**: Atomic operations, error recovery, backup strategies
4. **Flexibility**: Runtime discovery, configuration migrations
5. **Simplicity**: Clear mental models, straightforward workflows

## Next Steps

1. Review ADRs and approve architectural decisions
2. Begin implementation based on specification
3. Test core wizard flows and configuration management
4. Integrate with existing scripts
5. User acceptance testing and feedback collection

## Files Created/Modified

### New ADRs
- `/home/dev/project/docs/adr/0002-tui-user-interface.md`
- `/home/dev/project/docs/adr/0003-per-project-config-storage.md`
- `/home/dev/project/docs/adr/0004-concurrent-container-execution.md`
- `/home/dev/project/docs/adr/0005-first-run-setup-automation.md`
- `/home/dev/project/docs/adr/0006-vcs-vs-ai-provider-separation.md`
- `/home/dev/project/docs/adr/0007-native-script-enhancement.md`
- `/home/dev/project/docs/adr/0008-runtime-detection-editions-modes.md`
- `/home/dev/project/docs/adr/0009-atomic-config-with-backups.md`

### Documentation
- `/home/dev/project/docs/tui-implementation.md` - Complete technical specification
- `/home/dev/project/CONTEXT.md` - Updated with new domain terminology
- `/home/dev/project/docs/tui-summary.md` - This summary document

### Scripts (to be implemented)
- `dist/scripts/start-tui.sh` - Main TUI entry point
- Enhanced `dist/scripts/start.sh` with --start_opencode flag

## Design Questions Resolved

All 60 design questions from the grilling session were addressed, covering:
- Configuration architecture and persistence
- Container lifecycle management
- User interface flows and wizard design
- Error handling and recovery
- Security and validation
- Performance considerations
- User experience details

The specification provides a solid foundation for implementing a production-ready TUI that enhances OpenCode Sandbox usability while maintaining compatibility with existing functionality.
