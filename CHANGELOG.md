# Changelog

All notable release-facing changes are documented here.

## [Unreleased]

### Added

- Version, commit, and build-date metadata in `swipenode --version`.
- Tag-driven cross-platform release archives, SHA-256 checksums, and verified Unix installer.
- Atomic Claude Desktop MCP configuration updates with backups.
- Entire external-command binary `entire-swipenode` with local context, doctor, explicit-source research, deterministic Git-diff verification, and safe AGENTS.md initialization.
- Entire environment handling for `ENTIRE_CLI_VERSION`, `ENTIRE_REPO_ROOT`, and lazily created `ENTIRE_PLUGIN_DATA_DIR`.

### Changed

- Documentation now describes supported extraction behavior and limitations conservatively.
- Release archives now include separate checksum-verified assets for the standalone CLI and the Entire plugin across the existing platform matrix.

## [2.0.1] - pending

Planned first CLI/MCP release after the existing `v2.0.0` tag. No tag is created by this change.
