# Envil

Envil is a secure, cloud-backed environment variable backup and recovery tool. It encrypts `.env` files client-side, backs them up to an authenticated blob store with version history, and enables instant recovery across machines using a single 24-word recovery phrase.

---

## Key Features

- **Zero Knowledge Architecture**: All encryption and key derivation happens on your local machine. The server operator only ever sees opaque ciphertext.
- **24-Word Recovery Phrase**: Identity is anchored by standard BIP39 mnemonic entropy. No email, passwords, or persistent sessions to manage.
- **Signed Request Authentication**: Every request is authenticated using Ed25519 signatures over canonical request strings (method, path, timestamp, nonce, body hash).
- **Authenticated Encryption**: Uses `XChaCha20-Poly1305` with 24-byte random nonces and AAD binding (`account_id || project_id || version_number`).
- **Granular Diffing**: Compare local files with remote backup versions key-by-key, with secrets masked by default.
- **Monotonic Versioning & Optimistic Concurrency**: Concurrent uploads targeting the same version are detected and rejected with `409 Conflict`.
- **Safe Restores**: Preserves local modifications by creating `.env.bak` safety copies and sets file permissions to `0600`.

---

## Cryptographic Design

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

1. **Blob Layout**: `version(1 byte) | nonce(24 bytes) | ciphertext + Poly1305 tag`
2. **AAD Binding**: `account_id (32 bytes) || project_id (UTF-8 bytes) || version_number (4 bytes big-endian)`. Prevents the server from swapping blobs across projects or versions.
3. **Local Storage**: Keys are saved to the OS keychain (`go-keyring`) with an automatic `0600` file fallback at `~/.config/envil/keys.json`.

---

## Installation & Build

### Prerequisites
- Go 1.23+ (or 1.26+)
- PostgreSQL (for running the production server; in-memory store supported for dev/tests)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/ibrahimraimi/envil.git
cd envil

# Build both CLI and server binaries into bin/
make build

# Run unit, integration, and property tests
make test
```

Binaries will be placed in `./bin/envil` and `./bin/envil-server`.

---

## CLI Usage

### 1. Initialize Account & Link Project
```bash
envil init
```
- First run generates a 24-word recovery phrase, asks you to confirm 3 random words, and registers your public key with the server.
- Links the current directory by creating a `.envil.json` project configuration file.

### 2. Backup Environment Files
```bash
# Backs up all standard .env files (.env, .env.local, .env.production, etc.)
envil backup

# Back up specific file(s)
envil backup --file .env.custom
```

### 3. Compare Local Files Against Backup
```bash
# Show key-level diff (values masked)
envil diff

# Show diff against specific version with values revealed
envil diff --version 1 --reveal
```

### 4. Restore Environment Files
```bash
# Restore latest version
envil restore

# Restore specific version with force overwrite (creates .env.bak copies)
envil restore --version 2 --force
```

### 5. Inspect Projects and Versions
```bash
# List all your cloud projects (names decrypted client-side)
envil list

# View version history for the current project
envil history
```

### 6. Machine Recovery
On a new laptop or after wiping local keys:
```bash
envil recover
```
Prompts for your 24-word phrase via a hidden prompt, restores your keys, lists your projects, and lets you restore anywhere.

### 7. Diagnostics
```bash
envil doctor
```
Checks keystore accessibility, server connectivity, server clock skew, project linkage, and git safety (`.gitignore`).

### 8. Project & Account Deletion
```bash
# Delete current project
envil delete

# Delete entire account and all backups permanently
envil delete --account
```

---

## Server Deployment

### Configuration

The server is configured via environment variables:

| Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Network address and port to bind to |
| `DATABASE_URL` | `""` | PostgreSQL connection string (uses in-memory store if unset) |
| `RATE_LIMIT_RPS` | `200` | Sustained requests per second allowed per IP |
| `RATE_LIMIT_BURST` | `400` | Maximum burst capacity per IP |
| `MAX_BLOB_BYTES` | `1048576` | Maximum size per encrypted backup bundle (1 MiB default) |

### Running the Server

```bash
# Local development mode (in-memory store)
./bin/envil-server

# Production mode with PostgreSQL
export DATABASE_URL="postgres://user:password@localhost:5432/envil?sslmode=require"
./bin/envil-server
```

### Docker Deployment

```bash
docker build -t envil-server:latest .
docker run -p 8080:8080 -e DATABASE_URL="postgres://..." envil-server:latest
```

---

## Running Database Migrations

Database schema migrations are located in `migrations/`:
```bash
# Apply with Goose:
goose -dir migrations postgres "$DATABASE_URL" up
```

---

## Security Model & Guidelines

- **Phrase Storage**: The 24-word recovery phrase is never written to disk. Only the derived signing seed and encryption root are stored in the OS keychain (or mode `0600` config file).
- **Log Hygiene**: The server and CLI never log request bodies, recovery phrases, or authorization headers.
- **Git Protection**: `envil init` warns immediately if `.env` is omitted from `.gitignore`.
