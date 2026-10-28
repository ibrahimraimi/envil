# Changelog

All notable changes to Envil will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.1.1] - 2026-10-28

### Added
- **Interactive Terminal UI**: Full keyboard-driven TUI built with Bubble Tea v2 (`charm.land/bubbletea/v2`), Bubbles v2 (`charm.land/bubbles/v2`), and Lip Gloss v2 (`charm.land/lipgloss/v2`).
- **Dashboard & Project Detail**: Live project overview with account fingerprint status bar, async loading spinner, version history browsing, and project selection.
- **Diff & Safe Restore**: Interactive variable-level diffing between local files and remote backups with secret masking by default (`********`), per-key reveal toggle (`v`), and safety backups (`.env.bak`).
- **Guided Setup & Recovery Wizards**:
  - Setup wizard with 24-word phrase grid display, mandatory 3-word verification, and immediate phrase zeroing from memory upon completion or exit.
  - Recovery wizard with masked password input (`EchoPassword`), live word counter, and BIP39 checksum validation.
- **Entry Points & Fallback**:
  - `envil` with no arguments in an interactive terminal opens the TUI dashboard.
  - `envil tui` command to explicitly launch the TUI dashboard.
  - `--no-tui` persistent flag and `ENVIL_NO_TUI=1` environment variable to disable TUI for scripting and CI/CD pipelines.
  - Automatic fallback to plain text CLI output when stdin/stdout are non-interactive pipes.
- **Service Layer**: Reusable `internal/service` package decoupling application logic from Cobra commands.

### Changed
- Refactored CLI commands (`init`, `backup`, `restore`, `list`, `history`, `diff`, `recover`, `doctor`, `delete`) to delegate business logic to `service.Service`.

---

## [0.1.0] - 2026-10-27

### Added
- Initial release of Envil: zero-knowledge environment variable backup and recovery tool.
- Client-side authenticated encryption using XChaCha20-Poly1305 with AAD binding (`account_id || project_id || version`).
- Deterministic key derivation from 24-word BIP39 mnemonic using HKDF-SHA256.
- Ed25519 digital signature authentication for all API requests.
- Local keystore integration with OS keychain (`go-keyring`) and 0600 file fallback (`~/.config/envil/keys.json`).
- Optimistic concurrency control and version conflict detection (`409 Conflict`).
- PostgreSQL persistence store and embedded in-memory test store.
- Comprehensive CLI commands and doctor diagnostics.
