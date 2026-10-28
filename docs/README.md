# Envil Documentation

Envil is a secure, cloud-backed environment variable backup and recovery tool designed for developers. It encrypts `.env` files locally before upload, maintains monotonic version history, and enables instant recovery across machines using a single 24-word recovery phrase.

---

## 1. Core Principles

- **Zero Knowledge Architecture**: All encryption and key derivation occurs on your local machine. The server operator only stores opaque ciphertext blobs and has no mechanism to decrypt your environment files or metadata.
- **Phrase-Based Identity**: Identity is anchored by standard BIP39 mnemonic entropy (256 bits). There are no passwords, user accounts, or persistent web sessions.
- **Cryptographic Request Signing**: Requests are authenticated with Ed25519 digital signatures computed over canonical request strings, preventing replay attacks and payload tampering in transit.
- **Deterministic Key Derivation**: Project keys are derived per project from an encryption root, ensuring that compromising one project key never compromises another.

---

## 2. Architecture Overview

```
+------------------------------------+         HTTPS          +--------------------------------------+
|             Envil CLI              | <--------------------> |             Envil Server             |
|                                    |   Signed Requests      |                                      |
|  - BIP39 Mnemonic (24 words)       |                        |  - Request Signature Verification    |
|  - HKDF-SHA256 Derivation          |                        |  - Optimistic Concurrency Control    |
|  - XChaCha20-Poly1305 Encryption   |                        |  - Rate Limiting (IP/Token Bucket)   |
|  - Key-Level Env Parser & Diff     |                        |  - Quota Management (5 MiB default)  |
|  - Local Keystore (Keychain/File)  |                        +------------------+-------------------+
+------------------------------------+                                           |
                                                                          +------v------+
                                                                          | PostgreSQL  |
                                                                          | (Ciphertext |
                                                                          |  Storage)   |
                                                                          +-------------+
```

### Components

1. **CLI (`envil`)**: Static, cross-platform binary built with Cobra. Handles key generation, keystore interactions, file discovery, bundling, encryption/decryption, and git safety verification.
2. **Server (`envil-server`)**: Lightweight Go HTTP service built with Chi. Authenticates incoming requests, enforces monotonic version increments and storage quotas, and manages backup retention.
3. **Database (PostgreSQL)**: Stores accounts, projects, and encrypted versioned backups. Can run against managed cloud Postgres (e.g., Neon, Supabase, AWS RDS) or an embedded in-memory store for local testing.

---

## 3. Cryptographic Design

```
24-Word BIP39 Mnemonic (256-bit entropy)
   │
   ├─► HKDF-SHA256(salt="envil-v1", info="envil/v1/signing")    ──► Ed25519 Keypair (Request Auth)
   │
   └─► HKDF-SHA256(salt="envil-v1", info="envil/v1/encryption") ──► Encryption Root (32 bytes)
                                                                       │
                                                                       └─► HKDF-SHA256(info="envil/v1/project/{id}")
                                                                              │
                                                                              ▼
                                                                     Project Data Key (32 bytes)
```

### Cipher Details

- **Algorithm**: `XChaCha20-Poly1305` (`golang.org/x/crypto/chacha20poly1305`).
- **Nonce**: 24-byte cryptographically secure random nonce generated per encryption (`crypto/rand`).
- **Binary Blob Format**:
  ```
  | format_version (1 byte = 0x01) | nonce (24 bytes) | ciphertext + poly1305 tag |
  ```
- **Additional Authenticated Data (AAD)**:
  ```
  AAD = account_id (32 bytes) || project_id (UTF-8 bytes) || version_number (4 bytes big-endian)
  ```
  Binding account, project, and version into the AEAD tag prevents server operators or network attackers from swapping blobs between projects or rolling back versions without detection.

### Request Authentication Protocol

Every API request carries four authentication headers:
- `X-Envil-Key`: Base64-encoded Ed25519 public key.
- `X-Envil-Timestamp`: Unix epoch seconds.
- `X-Envil-Nonce`: 16-byte random nonce (Base64-encoded).
- `X-Envil-Signature`: Base64-encoded Ed25519 signature.

The signature is computed over the canonical string:
```
METHOD \n PATH_WITH_QUERY \n TIMESTAMP \n NONCE \n SHA256_HEX(BODY)
```

Verification rules:
1. Request timestamp must be within ±5 minutes of the server clock.
2. Nonce must not have been seen within the 10-minute cache window.
3. Signature must match the public key.
4. Public key must belong to an active registered account (except for `POST /v1/accounts`).

---

## 4. Threat Model & Guarantees

| Threat | Mitigation |
|---|---|
| **Server Compromise / Malicious Host** | Client-side encryption ensures the server operator holds only ciphertext blobs and project UUIDs. Project names and git remotes are encrypted. |
| **Network Interception / Replay** | TLS in transit, plus Ed25519 signed canonical requests, 5-minute timestamp windows, and 10-minute nonce caches. |
| **Blob Substitution Across Projects** | AEAD Additional Authenticated Data (AAD) cryptographically binds each blob to its specific project ID and version number. |
| **Accidental Git Exposure** | `envil init` actively checks `.gitignore` and warns if `.env` is omitted. |
| **Silent File Overwrite** | `envil restore` shows file diffs, requires `--force` for non-identical local files, and writes a `.env.bak` safety copy before writing. Restored files are set to mode `0600`. |
| **Shoulder Surfing / Shell Logs** | Recovery phrases are entered through hidden terminal prompts and are never accepted via CLI flags or environment variables. |

---

## 5. Documentation Directory

- [Setup & Installation Guide](./SETUP.md): Build requirements, installation methods, server deployment, database migrations, and keystore configuration.
- [CLI Usage Guide](./USEAGE.md): Step-by-step workflow commands, first-run initialization, backup, diff, restore, machine recovery, and diagnostics.

---

## 6. Changelog

### v0.1.1
- **Added**: Interactive Terminal User Interface (TUI) built on Bubble Tea v2 (`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`).
- **Added**: `envil tui` command and automatic interactive dashboard on bare `envil` invocation.
- **Added**: `--no-tui` persistent flag and `ENVIL_NO_TUI=1` environment variable for automation and CI/CD pipelines.
- **Added**: Guided setup and recovery wizard interfaces with alt-screen memory isolation and phrase wiping.
- **Added**: Variable-level diffing in restore flow with secret masking and on-demand reveal toggle (`v`).
- **Changed**: Reusable service layer (`internal/service`) decoupled from Cobra commands.

