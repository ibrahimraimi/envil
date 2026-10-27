# Envil: Plan, Design, Build, Test

Envil is a secure, cloud-backed environment variable backup and recovery tool. It backs up `.env` files, restores them on any machine, and recovers everything from a recovery phrase.

This document covers four phases: Plan, Design, Build, Test. It is written so that you can build from it directly, in Go.

---

## 0. Key Decisions Summary

| Decision | Choice | Reason |
|---|---|---|
| Security model | Zero knowledge, client-side encryption | The server never sees plaintext or keys. A server breach leaks only ciphertext. |
| Identity | Recovery phrase (BIP39, 24 words) | No passwords, no email required for the MVP. The phrase is the account. |
| Authentication | Ed25519 signed requests | No sessions, no tokens to steal. The public key is the account identifier. |
| Encryption | XChaCha20-Poly1305 | Random 192-bit nonces are safe, simple API, in `golang.org/x/crypto`. |
| Key derivation | HKDF-SHA256 from phrase entropy | The phrase already has 256 bits of entropy, so a slow KDF is not needed. |
| Backend | Go, `net/http` with `chi`, PostgreSQL | Env files are tiny, so blobs can live in Postgres. No object store needed. |
| CLI | Go, `cobra` | Standard, single static binary, easy cross-platform distribution. |
| Local key storage | OS keychain via `go-keyring`, file fallback | Avoids typing 24 words on every command. |

Important consequence: if the user loses both their machine and their phrase, the data is unrecoverable by design. The product must say this clearly at setup.

---

## 1. Plan

### 1.1 Problem statement

Developers keep secrets in `.env` files that are deliberately excluded from Git. When a laptop dies, is replaced, or a new teammate or machine appears, those files are lost or must be recreated by hand.

### 1.2 Goals

1. Back up one or more `.env` files per project with one command.
2. Restore them on any machine with one command.
3. Recover everything using only a recovery phrase.
4. Guarantee that the server operator cannot read any secret.
5. Keep version history so a bad backup can be rolled back.

### 1.3 Non-goals (MVP)

- Team sharing and access control (see Roadmap, v2).
- Secret injection at runtime (that is what tools like Doppler and Infisical do).
- A web UI. The MVP is CLI plus API only.
- Secret rotation or integrations with cloud providers.

### 1.4 Target users

Individual developers with several projects and more than one machine, and freelancers who need to hand over a configuration safely.

### 1.5 Competitive landscape (research summary)

| Tool | Approach | Gap Envil fills |
|---|---|---|
| Doppler, Infisical | Hosted secret managers, team focused | Heavy for solo backup. Server can usually read secrets. |
| dotenv-vault | Encrypted `.env` synced via vault | Account and key model is more complex than a phrase. |
| 1Password or Bitwarden CLI | General vault | Not `.env` aware, no project mapping or diffing. |
| `git-crypt`, `sops`, `age` | Encrypt files in the repo | Solves repo storage, not cross-machine recovery. |

Envil positioning: the simplest possible "backup and restore my `.env` files" tool with a strong zero-knowledge guarantee. Verify the current state of these products before launch, since the landscape changes quickly.

### 1.6 Milestones

| Milestone | Scope | Estimate |
|---|---|---|
| M1 | Crypto core library plus test vectors | 3 to 4 days |
| M2 | Server: registration, signed auth, store and fetch blobs | 4 to 5 days |
| M3 | CLI: `init`, `backup`, `restore`, `list` | 4 to 5 days |
| M4 | `recover`, `history`, `diff`, `delete`, `doctor` | 3 to 4 days |
| M5 | Hardening, rate limits, observability, packaging | 4 to 5 days |
| M6 | Beta release, docs, security review | 3 to 5 days |

Total: roughly 4 to 5 weeks of part-time effort for one developer.

### 1.7 Risks

| Risk | Impact | Mitigation |
|---|---|---|
| User loses phrase | Permanent data loss | Forced confirmation during setup, clear warnings, optional printable recovery sheet. |
| Crypto implementation error | Secrets exposed | Use only vetted primitives, no custom constructions, test vectors, external review before launch. |
| Replay or request forgery | Unauthorized access | Timestamp window, nonce cache, signature covers method, path, and body hash. |
| Account enumeration or spam registration | Abuse and cost | Rate limits, payload caps, per-account quotas. |
| Phrase typed into a shell history | Phrase leak | Read phrase from a hidden prompt, never from flags or arguments. |

---

## 2. Design

### 2.1 Architecture

```
+---------------------+         HTTPS          +-----------------------+
|  envil CLI (Go)     | <--------------------> |  envil server (Go)    |
|                     |   signed requests      |                       |
|  - key derivation   |                        |  - verify signatures  |
|  - encrypt/decrypt  |                        |  - store ciphertext   |
|  - .env parsing     |                        |  - versioning, quotas |
|  - OS keychain      |                        +-----------+-----------+
+---------------------+                                    |
                                                    +------v------+
                                                    | PostgreSQL  |
                                                    | (ciphertext |
                                                    |  only)      |
                                                    +-------------+
```

All cryptography happens in the CLI. The server is a dumb, authenticated blob store with versioning.

### 2.2 Cryptographic design

**Recovery phrase.** Generate 256 bits of entropy with `crypto/rand` and encode as a 24-word BIP39 mnemonic. The BIP39 checksum catches typos on entry.

**Key derivation.** Let `E` be the 32 bytes of entropy decoded from the phrase.

```
signing_seed = HKDF-SHA256(secret=E, salt="envil-v1", info="envil/v1/signing",    len=32)
enc_root     = HKDF-SHA256(secret=E, salt="envil-v1", info="envil/v1/encryption", len=32)
project_key  = HKDF-SHA256(secret=enc_root, salt="envil-v1", info="envil/v1/project/" + project_id, len=32)
```

- `signing_seed` produces an Ed25519 key pair. The public key is registered with the server and its SHA-256 fingerprint is the account ID.
- `project_key` is derived per project, so compromising one derived key does not expose other projects.
- Because the phrase has full entropy, a password-hardening KDF such as Argon2 is not required for the phrase itself. Use Argon2id only if you later add an optional passphrase (BIP39 "25th word") or encrypt the local key file with a user password.

**Encryption.** XChaCha20-Poly1305 with a fresh random 24-byte nonce per encryption.

Blob format (binary):

```
| version (1 byte) | nonce (24 bytes) | ciphertext + tag |
```

Additional authenticated data (AAD): `account_id || project_id || version_number`. This binds a ciphertext to its location so the server cannot swap blobs between projects or versions without detection.

**What is encrypted.** Everything of value: the file contents, the file name, the project display name, and the optional Git remote. The server sees only opaque IDs, sizes, and timestamps.

**Known tradeoff.** Deterministic key derivation means keys cannot be rotated without a new phrase. A later version may switch to random data keys wrapped by the master key to allow rotation. This is documented as a v2 item.

### 2.3 Authentication

Every request carries these headers:

```
X-Envil-Key:       base64(ed25519 public key)
X-Envil-Timestamp: unix seconds
X-Envil-Nonce:     random 16 bytes, base64
X-Envil-Signature: base64(ed25519 signature)
```

Signature input (canonical string):

```
METHOD \n PATH_WITH_QUERY \n TIMESTAMP \n NONCE \n SHA256_HEX(BODY)
```

Server checks:

1. Public key is registered (except on the register endpoint).
2. Timestamp is within plus or minus 5 minutes.
3. Nonce has not been seen in the last 10 minutes (in-memory cache or Redis, keyed by key fingerprint plus nonce).
4. Signature verifies.

### 2.4 Data model (PostgreSQL)

```sql
CREATE TABLE accounts (
    id            BYTEA PRIMARY KEY,          -- sha256(public_key)
    public_key    BYTEA NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    quota_bytes   BIGINT NOT NULL DEFAULT 5242880
);

CREATE TABLE projects (
    id            UUID PRIMARY KEY,           -- generated client side
    account_id    BYTEA NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    meta_blob     BYTEA NOT NULL,             -- encrypted name, git remote
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE TABLE backups (
    id            BIGSERIAL PRIMARY KEY,
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version       INT NOT NULL,               -- monotonically increasing per project
    blob          BYTEA NOT NULL,             -- encrypted bundle of env files
    size_bytes    INT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, version)
);

CREATE INDEX idx_projects_account ON projects(account_id);
```

The encrypted bundle contains all env files of a project (for example `.env`, `.env.local`) as one JSON document, encrypted as a single blob. Cap blob size at 1 MiB.

### 2.5 API specification

Base path: `/v1`

| Method | Path | Purpose |
|---|---|---|
| POST | `/accounts` | Register a public key. Idempotent. |
| GET | `/projects` | List projects (IDs plus encrypted metadata). |
| PUT | `/projects/{id}` | Create or update project metadata. |
| DELETE | `/projects/{id}` | Soft delete a project and its backups. |
| POST | `/projects/{id}/backups` | Upload a new version. Body includes `expected_version` for optimistic concurrency. |
| GET | `/projects/{id}/backups` | List versions (number, size, timestamp). |
| GET | `/projects/{id}/backups/{version}` | Download a version. `latest` is allowed. |
| DELETE | `/account` | Delete the account and all data. |
| GET | `/healthz` | Liveness probe. |

Concurrency rule: an upload with `expected_version` that does not match the current latest returns `409 Conflict`. The CLI then tells the user to run `envil diff` or `envil restore` first.

Standard error shape:

```json
{ "error": { "code": "version_conflict", "message": "..." } }
```

### 2.6 Backup bundle plaintext format

```json
{
  "schema": 1,
  "files": [
    { "name": ".env",       "content": "KEY=value\n...", "sha256": "..." },
    { "name": ".env.local", "content": "...",            "sha256": "..." }
  ],
  "created_by": "envil/0.1.0",
  "hostname_hash": "..."
}
```

Store file content verbatim (including comments and ordering). Do not parse and re-serialize for backup, so restores are byte-identical. Parse only for `diff` and key-level display.

### 2.7 CLI design

```
envil init                 Create or link a project in the current directory
envil backup [--file .env] Encrypt and upload a new version
envil restore [--version N] [--force]
envil list                 List projects (decrypted names)
envil history              List versions of the current project
envil diff [--version N]   Show key-level diff between local and remote (values masked by default)
envil recover              Enter the phrase on a new machine and store keys locally
envil doctor               Check keychain, connectivity, clock skew, config
envil delete               Delete a project or the whole account
envil version
```

Project linkage: `envil init` writes `.envil.json` containing only the project UUID. It contains no secrets and may be committed. Add a safety check that warns if `.env` is not in `.gitignore`.

**First run flow**

1. `envil init` detects no local keys.
2. Generate the phrase, display it once, never write it to disk.
3. Ask the user to re-enter three randomly chosen words.
4. Derive keys, store the signing seed and encryption root in the OS keychain.
5. Register the public key with the server.

**Recovery flow (new machine)**

1. `envil recover` prompts for the 24 words using a hidden input.
2. Validate the BIP39 checksum.
3. Derive keys, store in keychain.
4. Call `GET /projects`, decrypt names, show a list.
5. User runs `envil restore` inside a project folder or picks a project.

**UX details that matter**

- Mask values in terminal output by default. Provide `--reveal`.
- `restore` never overwrites silently. It shows a diff and asks, or requires `--force`. It writes a `.env.bak` before overwriting.
- Set file mode `0600` on restored files.

### 2.8 Threat model

| Threat | Covered by |
|---|---|
| Server breach or malicious operator | Client-side encryption. Server holds ciphertext only. |
| Network attacker | TLS plus signed requests, and ciphertext is authenticated by AEAD. |
| Blob swapping by server | AAD binds ciphertext to account, project, and version. |
| Replay of a captured request | Timestamp window plus nonce cache. |
| Stolen laptop | Keys in OS keychain. Document full-disk encryption as a requirement. Optional local passphrase in v2. |
| Phrase shoulder surfing or logging | Hidden prompt, never accepted as a flag, never logged. |
| Rollback attack (server serves an older version) | Not fully prevented in MVP. CLI records last seen version locally and warns on a decrease. |
| Metadata leakage | Server sees sizes, timestamps, and counts. Documented limitation. |

Out of scope: a compromised developer machine with malware that reads `.env` directly.

### 2.9 Repository layout

```
envil/
  cmd/
    envil/            main.go for the CLI
    envil-server/     main.go for the server
  internal/
    crypto/           phrase, derivation, seal/open
    auth/             request signing and verification
    client/           HTTP client for the API
    bundle/           bundle encode/decode, env parsing, diff
    keystore/         keychain abstraction plus file fallback
    server/           handlers, middleware, routing
    store/            Postgres repositories, migrations
    config/           config loading
  migrations/         SQL migration files
  docs/               this document, threat model, API spec
  test/               integration and end-to-end tests
  go.mod
  Makefile
```

---

## 3. Build

### 3.1 Dependencies

| Purpose | Package |
|---|---|
| CLI framework | `github.com/spf13/cobra` |
| BIP39 phrase | `github.com/tyler-smith/go-bip39` |
| AEAD and HKDF | `golang.org/x/crypto/chacha20poly1305`, `golang.org/x/crypto/hkdf` |
| Hidden prompt | `golang.org/x/term` |
| Keychain | `github.com/zalando/go-keyring` |
| HTTP router | `github.com/go-chi/chi/v5` |
| Postgres driver | `github.com/jackc/pgx/v5` |
| Migrations | `github.com/pressly/goose/v3` |
| Logging | `log/slog` (standard library) |
| Rate limiting | `golang.org/x/time/rate` |
| Integration tests | `github.com/testcontainers/testcontainers-go` |
| Env parsing | `github.com/joho/godotenv` (for diff only) |

Pin versions with `go.mod` and check each package for current releases and advisories before starting.

### 3.2 Phase-by-phase build order

**Phase A: Crypto core (`internal/crypto`)**

Build and fully test this first. Everything else depends on it.

Reference implementation:

```go
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	formatVersion byte = 1
	hkdfSalt           = "envil-v1"
)

var (
	ErrInvalidPhrase = errors.New("invalid recovery phrase")
	ErrDecrypt       = errors.New("decryption failed")
)

type Keys struct {
	Signing ed25519.PrivateKey
	encRoot [32]byte
}

func NewPhrase() (string, error) {
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return "", err
	}
	return bip39.NewMnemonic(entropy)
}

func KeysFromPhrase(phrase string) (*Keys, error) {
	if !bip39.IsMnemonicValid(phrase) {
		return nil, ErrInvalidPhrase
	}
	entropy, err := bip39.EntropyFromMnemonic(phrase)
	if err != nil {
		return nil, ErrInvalidPhrase
	}
	seed, err := derive(entropy, "envil/v1/signing")
	if err != nil {
		return nil, err
	}
	root, err := derive(entropy, "envil/v1/encryption")
	if err != nil {
		return nil, err
	}
	k := &Keys{Signing: ed25519.NewKeyFromSeed(seed)}
	copy(k.encRoot[:], root)
	return k, nil
}

func (k *Keys) AccountID() [32]byte {
	pub := k.Signing.Public().(ed25519.PublicKey)
	return sha256.Sum256(pub)
}

func (k *Keys) ProjectKey(projectID string) ([]byte, error) {
	return derive(k.encRoot[:], "envil/v1/project/"+projectID)
}

func derive(secret []byte, info string) ([]byte, error) {
	r := hkdf.New(sha256.New, secret, []byte(hkdfSalt), []byte(info))
	out := make([]byte, 32)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Seal output: version(1) | nonce(24) | ciphertext+tag
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 1+aead.NonceSize(), 1+aead.NonceSize()+len(plaintext)+aead.Overhead())
	out[0] = formatVersion
	if _, err := rand.Read(out[1 : 1+aead.NonceSize()]); err != nil {
		return nil, err
	}
	nonce := out[1 : 1+aead.NonceSize()]
	return aead.Seal(out, nonce, plaintext, aad), nil
}

func Open(key, blob, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	hdr := 1 + aead.NonceSize()
	if len(blob) < hdr+aead.Overhead() {
		return nil, ErrDecrypt
	}
	if blob[0] != formatVersion {
		return nil, fmt.Errorf("unsupported blob version %d", blob[0])
	}
	pt, err := aead.Open(nil, blob[1:hdr], blob[hdr:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
```

Notes:

- Treat this as a starting point and review it yourself. Never reuse a nonce with the same key. Random 24-byte nonces make collisions negligible.
- Zero sensitive byte slices after use where practical (`clear(b)`), understanding that Go cannot guarantee memory wiping.

**Phase B: Request signing (`internal/auth`)**

- `Sign(req, privKey)` builds the canonical string and sets the headers.
- `Verify(req, store, nonceCache)` is chi middleware. Read and hash the body, then restore it with `io.NopCloser(bytes.NewReader(...))`.
- Enforce `http.MaxBytesReader` (for example 2 MiB) before hashing.

**Phase C: Server (`internal/server`, `internal/store`)**

1. Migrations and repositories.
2. Middleware order: request ID, logging (never log bodies or signature headers in full), recover, rate limit, auth.
3. Handlers per the API table.
4. Versioning inside a transaction: `SELECT max(version) ... FOR UPDATE`, compare with `expected_version`, insert `version+1`.
5. Quota check before insert.
6. Retention: keep the last 20 versions per project by default, delete older ones in the same transaction or a background job.

**Phase D: CLI (`cmd/envil`, `internal/client`, `internal/keystore`)**

- `keystore`: interface with `Get`, `Set`, `Delete`. Implementation 1 is the OS keychain. Implementation 2 is a `0600` file in the config directory for headless Linux, with a clear warning.
- `client`: typed methods for each endpoint, automatic signing, timeouts, and retries only for idempotent calls.
- Commands in the order: `init`, `backup`, `restore`, `list`, then `recover`, `history`, `diff`, `delete`, `doctor`.

**Phase E: Hardening and delivery**

- Rate limiting per IP and per account.
- Structured logs plus basic metrics (request count, latency, error rate).
- Dockerfile for the server (multi-stage, distroless, non-root).
- `goreleaser` for CLI builds (linux, macOS, windows, amd64 and arm64) and checksums, with signed releases.
- Install options: `curl` script, Homebrew tap, `go install`.
- Server config via environment variables: `DATABASE_URL`, `LISTEN_ADDR`, `MAX_BLOB_BYTES`, `RATE_LIMIT_RPS`.
- Deployment: any container host with managed Postgres, TLS terminated at the platform, automated daily database backups.

### 3.3 Definition of done per milestone

| Milestone | Done when |
|---|---|
| M1 | All crypto unit tests and fixed test vectors pass, including tamper tests. |
| M2 | Integration tests register an account, upload, list, and download through real signed requests. |
| M3 | A user can back up and restore a `.env` byte-identically on the same machine. |
| M4 | A user can wipe the local keychain, run `recover`, and restore all projects. |
| M5 | Rate limits, quotas, logs, and container image are in place. Load test meets targets. |
| M6 | External review of crypto and auth completed. Docs published. |

---

## 4. Test

### 4.1 Test pyramid

| Layer | Scope | Tools |
|---|---|---|
| Unit | crypto, bundle, diff, signing, canonicalization | `testing`, `testify` (optional) |
| Property and fuzz | encrypt/decrypt round trip, parser robustness | Go native fuzzing (`go test -fuzz`) |
| Integration | Server plus real Postgres | `testcontainers-go`, `httptest` |
| End-to-end | CLI binary against a running server | Test harness using `os/exec`, temp directories |
| Security | Tamper, replay, swap, oversize, malformed input | Table-driven tests, custom cases |
| Load | Throughput and latency | `k6` or `vegeta` |

### 4.2 Crypto test cases

1. Same phrase always derives the same keys (determinism).
2. Different phrases derive different keys.
3. Different `project_id` values derive different project keys.
4. Round trip: `Open(Seal(x)) == x` for empty, small, and 1 MiB inputs.
5. Two seals of the same plaintext produce different ciphertexts (nonce randomness).
6. Flipping any single bit in version, nonce, ciphertext, or tag makes `Open` fail.
7. Wrong AAD (different project or version) makes `Open` fail.
8. Wrong key makes `Open` fail.
9. Truncated blobs of every length from 0 to the header size fail cleanly without panics.
10. Invalid phrases fail: wrong word, wrong checksum, wrong word count, extra whitespace handling defined and tested.
11. **Known answer tests:** commit a fixed phrase together with expected public key, project key, and a sample ciphertext produced with a fixed nonce (use a test-only seal function). This detects accidental changes to derivation.

### 4.3 Auth test cases

- Valid signed request passes.
- Altered body, path, method, timestamp, or nonce fails.
- Timestamp 6 minutes old or 6 minutes in the future fails.
- Reused nonce fails.
- Unknown public key fails.
- Signature from a different key fails.
- Missing or malformed headers return `401`, never a panic.

### 4.4 Server integration cases

- Concurrent uploads to the same project: exactly one wins, the others get `409`.
- Version numbers are gapless and increasing.
- Quota exceeded returns a clear error and stores nothing.
- Blob above the size cap is rejected before full read.
- Account A cannot read, list, or delete account B's projects (test with two accounts).
- Soft-deleted projects are not listed or downloadable.
- Retention deletes only older versions.
- Database failure returns `500` without leaking internal details.

### 4.5 End-to-end scenarios

| Scenario | Steps | Expected result |
|---|---|---|
| Happy path | init, backup, modify `.env`, restore | Original file restored byte-identically. |
| New machine | backup, wipe keychain and config, recover, restore | All projects and files restored. |
| Wrong phrase | recover with one wrong word | Clear error, no keys stored. |
| Conflict | Two machines back up from the same base version | Second backup fails with a conflict message. |
| Offline | Run backup with the server down | Clear error, local file untouched, non-zero exit code. |
| Clock skew | Set the client clock 10 minutes off | `doctor` reports skew, requests fail with a helpful hint. |
| Overwrite safety | restore over a modified local `.env` | Diff shown, `.env.bak` created, file mode `0600`. |
| Secrets never in output | Run all commands with `--verbose` | Neither the phrase nor any value appears in logs or stderr. |

### 4.6 Fuzz targets

```go
func FuzzSealOpen(f *testing.F) {
	f.Add([]byte("KEY=value"), []byte("aad"))
	f.Fuzz(func(t *testing.T, pt, aad []byte) {
		key := make([]byte, 32)
		blob, err := Seal(key, pt, aad)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Open(key, blob, aad)
		if err != nil || !bytes.Equal(got, pt) {
			t.Fatalf("round trip failed: %v", err)
		}
	})
}

func FuzzOpenGarbage(f *testing.F) {
	f.Fuzz(func(t *testing.T, blob []byte) {
		_, _ = Open(make([]byte, 32), blob, nil) // must never panic
	})
}
```

Also fuzz the env diff parser and the canonical request string builder.

### 4.7 Security checklist before launch

- [ ] No secret, phrase, key, or plaintext value appears in any log line (grep test over captured logs).
- [ ] Phrase is read only from a hidden prompt, never from flags, environment variables, or arguments.
- [ ] `go vet`, `staticcheck`, and `gosec` pass with no unresolved high findings.
- [ ] `govulncheck ./...` is clean.
- [ ] Dependencies are minimal and pinned. SBOM generated.
- [ ] TLS only. HSTS enabled. Server rejects plaintext HTTP in production.
- [ ] Request size limits and timeouts on every handler.
- [ ] Constant-time comparisons where secrets or MACs are compared (`crypto/subtle`).
- [ ] Release binaries are checksummed and signed.
- [ ] An independent reviewer has read the crypto and auth code.

### 4.8 Performance targets (starting points)

| Metric | Target |
|---|---|
| Backup of a 10 KB env bundle, end to end | under 500 ms on a normal connection |
| Server p95 latency for upload and download | under 100 ms excluding network |
| Sustained load | 200 requests per second on a small instance |
| CLI cold start | under 100 ms |

### 4.9 CI pipeline

1. `go build ./...`
2. `go vet ./...` and `staticcheck ./...`
3. `go test -race -cover ./...`
4. Integration tests with Postgres via `testcontainers` or a service container.
5. Short fuzz run (30 seconds per target) on every pull request, longer scheduled runs nightly.
6. `govulncheck` and `gosec`.
7. On tags: `goreleaser` builds and publishes artifacts.

---

## 5. Roadmap After MVP

| Version | Feature |
|---|---|
| v0.2 | Optional local passphrase to protect keys at rest. Device list and per-device revocation tokens. |
| v0.3 | Random per-project data keys wrapped by the master key, enabling key rotation. |
| v0.4 | Printable recovery sheet (QR plus words). Shell completions. |
| v1.0 | Team sharing using public-key encryption (project key wrapped per member). |
| v1.x | Web viewer with in-browser decryption. Import and export for other tools. |

---

## 6. Open Questions for You

1. Do you want a hosted service, a self-hostable server, or both? The design supports both, but self-hosting affects documentation and packaging.
2. Is a 24-word phrase acceptable, or do you prefer 12 words (128 bits)? 24 words is recommended.
3. Do you want an optional passphrase from day one, or defer to v0.2?
4. Should the MVP support monorepos with several `.env` files in subdirectories, or one directory per project?
5. What is the monetization or quota model, if any? This affects the `quota_bytes` design.

---

## 7. Suggested First Week

| Day | Task |
|---|---|
| 1 | Create the repository, `go.mod`, Makefile, CI skeleton. Implement `internal/crypto` and its unit tests. |
| 2 | Add known answer test vectors and fuzz targets. Implement `internal/auth` signing and verification. |
| 3 | Write migrations and repositories. Implement `POST /accounts` and the auth middleware. |
| 4 | Implement project and backup endpoints with versioning and quotas. |
| 5 | Write server integration tests including the concurrency and cross-account cases. |