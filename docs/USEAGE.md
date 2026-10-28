# Envil CLI Usage Guide

This guide details all Envil commands, options, and operational workflows for backing up, diffing, and recovering environment variables.

---

## 1. Quick Start: The 3-Step Workflow

```bash
# Step 1: Initialize your account and link your project
envil init

# Step 2: Back up all local environment files
envil backup

# Step 3: Restore them anytime on any machine
envil restore
```

---

## 2. Command Reference

### `envil init`

Initializes Envil on your machine and links the current directory to a project.

```bash
envil init [flags]
```

**Flags**:
- `-y, --yes`: Bypass interactive 3-word phrase verification (useful for automated setups and scripts).

**What happens on first run**:
1. Checks the local keystore. If no account exists, generates a 24-word BIP39 recovery phrase.
2. Displays the phrase clearly once.
3. Prompts you to confirm 3 randomly selected words from the phrase to ensure you recorded it.
4. Derives the Ed25519 signing keypair and encryption root, saving them securely to your OS keychain or `~/.config/envil/keys.json` (`0600`).
5. Registers your public key with the Envil server.

**What happens for project linking**:
1. Checks for `.envil.json` in the current working directory.
2. Generates a new project UUID.
3. Discovers the project name from the directory and the Git remote from `git config --get remote.origin.url`.
4. Encrypts project metadata client-side using the project's derived key.
5. Saves `.envil.json` locally containing `{ "project_id": "<uuid>" }`.
6. **Safety Check**: Checks `.gitignore`. If `.env` is missing, displays a warning alert:
   ```
   ⚠️  WARNING: '.env' is not in your .gitignore file. Ensure secrets are not committed!
   ```

---

### `envil backup`

Encrypts and uploads environment files for the current project.

```bash
# Automatically back up all standard env files in current directory
envil backup

# Back up specific file(s) only
envil backup -f .env -f .env.production
```

**Flags**:
- `-f, --file`: Specific file(s) to back up. If omitted, automatically discovers `.env`, `.env.local`, `.env.development`, `.env.staging`, and `.env.production`.

**Behavior & Safety**:
- Stores file contents verbatim (including comments, formatting, and key order) so restores are byte-identical.
- Computes SHA-256 hashes for each file to ensure tamper resistance.
- Uses **optimistic concurrency control**: sends the expected current version number to the server. If another machine uploaded a newer version in the interim, the upload is rejected with a version conflict notice.

---

### `envil diff`

Compares local environment files against the cloud backup at the key/variable level.

```bash
# Diff against the latest cloud version (secrets masked)
envil diff

# Diff against a specific version
envil diff --version 1

# Reveal secret values in diff output
envil diff --reveal
```

**Flags**:
- `-v, --version`: Specific backup version to compare against (defaults to latest).
- `--reveal`: Reveal actual secret values instead of masking them as `********`.

**Diff Output Legend**:
- `+ KEY=val`: Variable exists only in remote backup (missing locally).
- `- KEY=val`: Variable exists only in local file (missing in backup).
- `~ KEY: local=... -> remote=...`: Variable exists in both but values differ.
- Unchanged keys are omitted for clean, focused output.

---

### `envil restore`

Downloads, decrypts, and restores environment files into the current directory.

```bash
# Restore latest version
envil restore

# Restore a specific version
envil restore --version 2

# Overwrite modified local files without prompt
envil restore --force
```

**Flags**:
- `-v, --version`: Version number to restore (defaults to latest).
- `-f, --force`: Overwrite existing modified local files.

**Safety Protections**:
1. **Identical Files**: If local files are identical to the backup, they are safely skipped.
2. **Conflict Prevention**: If a local file has modified content, `envil restore` refuses to overwrite it unless `--force` is provided.
3. **Safety Backup**: Whenever an existing file is overwritten with `--force`, Envil writes a timestamped `.env.bak.<timestamp>` copy (mode `0600`) of the pre-existing local file and prunes older copies to the newest `backup_copies` (default 3). Backup copies are plaintext and should be deleted when no longer needed.
4. **Rollback & Fork Detection**: Before restoring, Envil checks the server's latest version and ciphertext SHA-256 against `~/.config/envil/state.json`. If the server reports an older version or a forked blob, Envil aborts with exit code `3` unless `--accept-server-state` is passed.
5. **Strict Permissions**: Restored files are written with file mode `0600` (readable/writable only by the file owner).

---

### `envil history`

Displays the backup version history for the currently linked project.

```bash
envil history
```

**Output Example**:
```
VERSION    SIZE     CREATED AT
v3         412 B    2026-10-27 14:15:30
v2         398 B    2026-10-27 12:04:12
v1         245 B    2026-10-26 18:22:05
```

---

### `envil list`

Lists all cloud-stored projects linked to your account.

```bash
envil list
```

**Behavior**:
- Fetches all projects associated with your account public key.
- Decrypts each project's name and git remote client-side using derived project keys.
- Displays a formatted table with Project ID, Name, Git Remote, and Creation Date.

---

### `envil recover`

Recovers your account and encryption keys on a new machine using your 24-word phrase.

```bash
envil recover
```

**Flow**:
1. Prompts for your 24-word phrase using a hidden terminal input (characters are not echoed to screen or shell history).
2. Verifies the BIP39 checksum to prevent typos.
3. Re-derives your signing and encryption root keys.
4. Saves keys into the local keystore.
5. Connects to the server and lists all your existing cloud projects.
6. You can then navigate to any project folder and run `envil restore`.

---

### `envil doctor`

Runs comprehensive health and diagnostic checks.

```bash
envil doctor
envil doctor --json
```

**Checks Performed**:
1. **Version**: Reports the CLI version and OS/architecture.
2. **Config Directory**: Verifies `~/.config/envil` permissions (`0700` directory, `0600` files on Unix).
3. **Keychain**: Tests reading and writing to the OS keychain or notes `0600` file fallback.
4. **Keys Present**: Verifies local signing and encryption keys exist.
5. **Server Reachable**: Pings `/v1/healthz` and measures latency.
6. **Account Registered**: Verifies authenticated request acceptance.
7. **Clock Skew**: Checks time drift between local machine and server (warns > 2m, fails > 5m).
8. **State File**: Verifies `state.json` HMAC integrity.
9. **Rollback Status**: Compares local seen version with server latest for the linked project.
10. **Git Safety**: Confirms `.env` files are ignored and not tracked by Git.
11. **Backup Copies**: Reports the count and age of local `.env.bak.*` copies.

---

### `envil state reset`

Resets local version tracking in `state.json` for a specific project or rebuilds the state file.

```bash
# Reset all tracked projects / rebuild state.json
envil state reset

# Reset a single project
envil state reset --project <uuid>
```

---

### `envil delete`

Deletes the current project or the entire account.

```bash
# Delete current project
envil delete

# Delete entire account and all backups permanently
envil delete --account

# Skip interactive confirmation prompt
envil delete --yes
```

**Safeguards**:
- Project deletion unlinks the local folder and soft-deletes the project on the server.
- Account deletion requires typing the exact confirmation phrase `"delete my account"` to prevent accidental loss of all projects and versions.

---

### `envil tui`

Opens the interactive Terminal User Interface (TUI) dashboard.

```bash
envil tui
```

In interactive terminals, simply running `envil` with no arguments automatically opens the dashboard.

---

## Interactive Mode (Terminal UI)

Envil includes a full-featured keyboard-driven Terminal User Interface built with Bubble Tea:

| Invocation | Behavior |
|---|---|
| `envil` (no arguments, in a terminal) | Opens the interactive dashboard. |
| `envil tui` | Opens the dashboard explicitly. |
| `envil init`, `envil recover` (in a terminal) | Runs guided wizards for phrase generation and recovery. |
| Any command with `--no-tui` (or when piping / non-TTY) | Standard plain text output for automation and scripts. |
| Environment variable `ENVIL_NO_TUI=1` | Disables TUI globally. |

### Keyboard Controls

- **Dashboard**:
  - `↑` / `↓` (or `k` / `j`): Navigate projects list
  - `enter`: Open selected project details and version history
  - `b`: Trigger backup for the selected or current project
  - `r`: Refresh project list from server
  - `?`: Toggle help shortcuts
  - `q` / `ctrl+c`: Quit
- **Project Detail**:
  - `↑` / `↓` (or `k` / `j`): Select backup version
  - `enter` / `d`: Open diff viewer for selected version
  - `s`: Restore selected version
  - `esc`: Return to dashboard
- **Restore & Diff**:
  - `↑` / `↓`: Move cursor across diff entries
  - `v`: Toggle reveal of masked secret value (`********`) for selected key
  - `y`: Confirm restore (automatically writes `.env.bak` safety copies)
  - `n` / `esc`: Cancel restore and return to dashboard
- **Setup & Recovery Wizards**:
  - `enter`: Advance to verification / submit phrase
  - `tab`: Switch between verification word fields
  - `r`: Review phrase again
  - `esc`: Cancel wizard

---

### Global Flags

Available across all commands:

- `--server <url>`: Override the Envil server endpoint (default: `http://localhost:8080` or via `ENVIL_SERVER_URL`).
- `--no-tui`: Disable the interactive terminal UI and force non-interactive output (or via `ENVIL_NO_TUI=1`).

---

## 3. Common Scenarios & FAQ

### Handling a Version Conflict (`409 Conflict`)
If another machine uploaded a newer backup while you were working:
1. Run `envil diff` to inspect differences between your local files and the new remote version.
2. Run `envil restore --force` if you want to accept remote changes (a `.env.bak` copy of your local file will be preserved).
3. Alternatively, update your local variables and run `envil backup` again.

### Moving to a New Machine
1. Install Envil: `go install github.com/ibrahimraimi/envil/cmd/envil@latest`.
2. Run `envil recover` and enter your 24-word recovery phrase.
3. Clone your repository.
4. Run `envil restore` inside the repository.
