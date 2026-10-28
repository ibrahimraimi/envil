# Envil

Envil is a secure, cloud-backed environment variable backup and recovery tool. It encrypts `.env` files client-side using `XChaCha20-Poly1305`, backs them up to an authenticated blob store with version history, and enables instant recovery across machines using a single 24-word recovery phrase.

---

## Key Features

- **Zero Knowledge**: Encryption and key derivation happen entirely on your machine. The server only ever sees opaque ciphertext blobs.
- **24-Word Recovery Phrase**: Account identity is derived from standard BIP39 mnemonic entropy. No email, passwords, or sessions.
- **Signed Request Authentication**: Requests are signed with Ed25519 over canonical request strings, preventing replay attacks and tampering in transit.
- **Authenticated Encryption**: Uses `XChaCha20-Poly1305` with 24-byte random nonces and AAD binding (`account_id || project_id || version`).
- **Granular Diffing**: Compare local files with remote backup versions key-by-key, with secrets masked by default.
- **Optimistic Concurrency**: Prevents race conditions and accidental overwrites with version conflict detection (`409 Conflict`).
- **Rollback & Fork Protection**: Tracks the highest seen version and ciphertext hash locally (`state.json`, HMAC-verified) to detect stale or substituted server responses.
- **Safe Restores**: Automatically creates timestamped `.env.bak.<timestamp>` safety copies (`0600`), prunes older copies, and checks `.gitignore` rules before writing.

---

## How It Works

```
24-Word BIP39 Phrase (256 bits entropy)
   │
   ├─► HKDF-SHA256(info="envil/v1/signing")    ──► Ed25519 Keypair (Authentication)
   └─► HKDF-SHA256(info="envil/v1/encryption") ──► Encryption Root
                                                      │
                                                      └─► HKDF-SHA256(info="envil/v1/project/{id}")
                                                             │
                                                             ▼
                                                    Project Data Key (XChaCha20-Poly1305)
```

1. **Deterministic Isolation**: Each project derives an independent 32-byte data key from your master encryption root. Compromising one project key does not expose other projects.
2. **Cryptographic Binding (AAD) & Rollback Detection**: Ciphertext blobs are bound to your account, project ID, and version number via AEAD AAD. In addition, the client records the highest seen version and ciphertext SHA-256 per project in an HMAC-protected local state file (`~/.config/envil/state.json`, mode `0600`) to detect rollback or fork attacks after first sight of a project (the first run on a new machine is trust on first use).
3. **Local Keystore**: Derived keys are stored in your OS keychain (`go-keyring`) with an automatic `0600` file fallback (`~/.config/envil/keys.json`).

---

## Quick Start

### 1. Installation

```bash
go install github.com/ibrahimraimi/envil/cmd/envil@latest
```

*(See the [Installation Guide](docs/INSTALLATION.md) for pre-built binaries and package managers.)*

### 2. Daily Workflow

```bash
# Initialize account and link the current project
envil init

# Back up all local environment files (.env, .env.local, etc.)
envil backup

# Inspect key-level differences against cloud backup (secrets masked)
envil diff

# Restore environment files onto any machine
envil restore

# Run system, keystore, server, clock, and rollback diagnostics
envil doctor
```

---

## Exit Codes & Error Catalog

Envil uses consistent exit codes for scripting and CI/CD automation:

| Exit Code | Meaning |
|---|---|
| `0` | Success |
| `1` | General error |
| `2` | Usage or flag error |
| `3` | Integrity warning (rollback, fork, or tampered local state) |
| `4` | Network, authentication, clock skew, or quota error |

| Code | Condition | Message | Hint |
|---|---|---|---|
| `E_CLOCK_SKEW` | Server rejects timestamp or skew > 5m | Your system clock differs from the server by more than 5 minutes. | Enable automatic time sync, then retry. Run `envil doctor`. |
| `E_OFFLINE` | Network failure or timeout | Could not reach the Envil server. | Check your connection and `ENVIL_SERVER` setting. Your local files were not changed. |
| `E_AUTH` | 401/403 or signature rejected | The server did not accept your credentials. | Run `envil doctor`. If you are on a new machine, run `envil recover`. |
| `E_CONFLICT` | 409 on upload | A newer version exists than the one you started from. | Run `envil diff`, then restore or merge, then back up again. |
| `E_QUOTA` | Quota exceeded | Your storage limit was reached. | Delete old projects or versions, then retry. |
| `E_TOO_LARGE` | Blob above 1 MiB cap | The environment bundle is larger than the 1 MiB limit. | Remove large values or split projects. |
| `E_ROLLBACK` | Rollback or fork detected | The server reported an older or divergent backup version for this project. | Nothing was changed. If you know this is expected, run again with `--accept-server-state`. |
| `E_STATE_TAMPERED` | State MAC mismatch | The local state file could not be verified. | Run `envil state reset` to rebuild it. |
| `E_KEYCHAIN` | Keychain unavailable | Could not access the system keychain. | See `envil doctor` for fallback options. |
| `E_NOT_INITIALIZED` | No keys or no project link | This directory is not set up. | Run `envil init`. |

---

## Shell Completions

Generate autocompletion scripts for your shell:

```bash
# Bash
source <(envil completion bash)

# Zsh
envil completion zsh > "${fpath[1]}/_envil"

# Fish
envil completion fish > ~/.config/fish/completions/envil.fish

# PowerShell
envil completion powershell | Out-String | Invoke-Expression
```

---

## Documentation

Comprehensive guides are available in the [`docs/`](docs/) directory:

- **[Architecture & Cryptography](docs/README.md)**: Zero-knowledge threat model, protocol specification, and cipher details.
- **[Setup & Server Deployment](docs/SETUP.md)**: Database configuration, migrations, PostgreSQL deployment, and Docker setup.
- **[CLI Usage Guide](docs/USEAGE.md)**: Complete command reference, recovery flows, diffing, and diagnostics.
- **[Installation Guide](docs/INSTALLATION.md)**: Go install, GitHub releases, source builds, and shell autocompletion.

---

## Development & Testing

```bash
# Build both CLI and server into bin/
make build

# Run unit, integration, and property tests
make test

# Run tests with data race detection
make test-race

# Run static analysis
make vet
```

---

## Security

Envil is designed so that a total breach of the server infrastructure yields only encrypted ciphertext blobs.

- The 24-word recovery phrase is **never written to disk** or sent over the network.
- Requests, logs, and error traces **never include secret values**.
- If you discover a security issue, please review our [Security Policy](.github/ISSUE_TEMPLATE/config.yml) or open a private advisory.

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
