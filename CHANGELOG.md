# Changelog

All notable changes to Envil will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.1.2] - 2026-10-28

### Added
- **Rollback & Fork Detection**: Local monotonic version and ciphertext SHA-256 tracking (`~/.config/envil/state.json`, mode `0600`) protected by HMAC-SHA256 (`HKDF-SHA256` with info `"envil/v1/state"`), plus `--accept-server-state` override and `envil state reset [--project ID]`.
- **Error Catalog & Exit Codes**: Centralized `internal/errs` package mapping network, clock skew, authentication, conflict, quota, bundle size, rollback/fork, state tamper, keychain, and initialization errors to plain-language messages, actionable hints, and exit codes (`0`–`4`).
- **Expanded Doctor (CLI & TUI)**: 11 diagnostic checks (Version, Config directory, Keychain, Keys present, Server reachable, Account registered, Clock skew, State file, Rollback status, Git, Backup copies), `--json` output, and an interactive Doctor screen (`d` from TUI dashboard).
- **`.gitignore` Safety Check**: Uses `git check-ignore` and `git ls-files` during `init`, `backup`, and `restore` to warn when environment files or backup copies are not ignored or are already tracked by Git, with `--no-git-check` and `git_check` configuration support.
- **Shell Completions**: `envil completion [bash|zsh|fish|powershell]` command and dynamic `--version` completion for `restore` and `diff`.

### Changed
- **Safer Local Backup Copies**: Restore backup copies are now timestamped (`.env.bak.YYYYMMDDTHHMMSS` with collision counters), restricted to mode `0600`, checked against `.gitignore`, and automatically pruned to the newest `backup_copies` (default 3). Note: backup copies contain plaintext secrets and should be deleted when no longer needed.

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
