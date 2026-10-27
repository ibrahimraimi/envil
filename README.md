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
- **Safe Restores**: Automatically creates `.env.bak` safety copies before overwriting modified files, and applies strict `0600` permissions.

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
2. **Cryptographic Binding (AAD)**: Ciphertext blobs are bound to your account, project ID, and version number. The server cannot swap or rollback versions without detection.
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
