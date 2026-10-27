# Envil Setup & Installation Guide

This guide covers building the Envil CLI, deploying the Envil API server, running database migrations, and configuring local keystores.

---

## 1. Prerequisites

- **Go**: Version 1.23 or newer (`go version`).
- **Operating System**: Linux, macOS, or Windows (amd64 / arm64).
- **PostgreSQL** (for production server): Version 14 or newer.
- **Docker** (optional): For containerized server deployments.

---

## 2. Building from Source

Clone the repository and build the binaries:

```bash
git clone https://github.com/ibrahimraimi/envil.git
cd envil

# Build both CLI and server into bin/
make build
```

This compiles two binaries:
- `bin/envil`: The developer CLI tool.
- `bin/envil-server`: The backend blob store and authentication service.

To install the CLI directly into your `$GOPATH/bin`:

```bash
go install ./cmd/envil
```

Ensure `$GOPATH/bin` (typically `~/go/bin`) is in your system `PATH`.

---

## 3. Configuring the CLI Keystore

Envil uses an automated tiered keystore to store derived encryption keys locally without asking for your 24-word recovery phrase on every command:

1. **OS Keychain (Default)**:
   - **macOS**: Apple Keychain Services.
   - **Linux**: Freedesktop Secret Service API via DBus (GNOME Keyring, KWallet).
   - **Windows**: Windows Credential Manager.

2. **File Fallback (Headless / Servers / WSL)**:
   If no desktop keyring daemon is running (such as in headless Linux servers, CI/CD runners, or minimal containers), Envil automatically falls back to an encrypted configuration file:
   - **Path**: `~/.config/envil/keys.json`
   - **File Permissions**: Strictly created with mode `0600` (readable and writable only by the current user).
   - **Directory Permissions**: Created with mode `0700`.

> [!NOTE]
> The 24-word recovery phrase is **never written to disk**. Only the derived Ed25519 signing seed and the 32-byte encryption root are stored in the keystore.

---

## 4. Deploying the Envil Server

### Configuration Environment Variables

The server binary is configured entirely via environment variables:

| Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Network address and port to bind to |
| `DATABASE_URL` | `""` | PostgreSQL connection string. If omitted, the server operates in **in-memory mode** (ideal for local testing). |
| `RATE_LIMIT_RPS` | `200` | Sustained requests per second allowed per client IP |
| `RATE_LIMIT_BURST` | `400` | Maximum burst requests allowed per client IP |
| `MAX_BLOB_BYTES` | `1048576` | Maximum size of an individual encrypted backup bundle (1 MiB default) |

### Option A: Local Development (In-Memory Store)

For local development or quick testing without a database:

```bash
./bin/envil-server
```

The server will log:
```json
{"level":"INFO","msg":"no DATABASE_URL provided, using in-memory store"}
{"level":"INFO","msg":"starting envil server","addr":":8080"}
```

### Option B: Production with PostgreSQL

Set the `DATABASE_URL` environment variable:

```bash
export DATABASE_URL="postgresql://user:password@localhost:5432/envil?sslmode=require"
./bin/envil-server
```

---

## 5. Database Schema & Migrations

Database migration files are located in the `migrations/` directory:
- `migrations/00001_init.sql`

### Running Migrations with Goose

Install Goose if not already present:
```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

Apply migrations:
```bash
export DATABASE_URL="postgresql://user:password@localhost:5432/envil?sslmode=require"
goose -dir migrations postgres "$DATABASE_URL" up
```

### Database Tables Created

1. **`accounts`**:
   - `id`: 32-byte SHA-256 fingerprint of the Ed25519 public key (Primary Key).
   - `public_key`: Ed25519 public key bytes.
   - `quota_bytes`: Storage quota in bytes (default: 5,242,880 bytes / 5 MiB).
   - `created_at`: Timestamp of account registration.

2. **`projects`**:
   - `id`: Client-generated UUID (Primary Key).
   - `account_id`: Foreign key referencing `accounts(id)` on delete cascade.
   - `meta_blob`: Encrypted project metadata (project name, git remote).
   - `created_at`: Timestamp of project creation.
   - `deleted_at`: Soft-deletion timestamp.

3. **`backups`**:
   - `id`: Monotonically increasing BigSerial sequence.
   - `project_id`: Foreign key referencing `projects(id)` on delete cascade.
   - `version`: Monotonically increasing version number per project.
   - `blob`: Encrypted bundle containing all `.env` files for this version.
   - `size_bytes`: Size of the encrypted blob in bytes.
   - `created_at`: Timestamp of backup upload.

---

## 6. Docker Deployment

### Building the Container

Envil includes a multi-stage Dockerfile that compiles a static binary and packages it into a minimal Alpine runtime running under a non-root user (`envil:envil`):

```bash
docker build -t envil-server:latest .
```

### Running with Docker

```bash
docker run -d \
  --name envil-server \
  -p 8080:8080 \
  -e DATABASE_URL="postgresql://user:password@db.example.com:5432/envil?sslmode=require" \
  -e RATE_LIMIT_RPS=300 \
  envil-server:latest
```

### Docker Compose Example

```yaml
version: "3.8"

services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: envil
      POSTGRES_USER: envil
      POSTGRES_PASSWORD: secretpassword
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  server:
    build: .
    restart: unless-stopped
    depends_on:
      - postgres
    environment:
      LISTEN_ADDR: ":8080"
      DATABASE_URL: "postgresql://envil:secretpassword@postgres:5432/envil?sslmode=disable"
      RATE_LIMIT_RPS: "200"
    ports:
      - "8080:8080"

volumes:
  postgres_data:
```

---

## 7. Verifying Your Setup

Once the server is running, point the CLI to your server URL:

```bash
# Via flag:
envil doctor --server http://localhost:8080

# Or via environment variable:
export ENVIL_SERVER_URL="http://localhost:8080"
envil doctor
```

The `doctor` command validates:
1. Local Keystore accessibility.
2. Server HTTP connectivity.
3. Server clock skew (must be within ±300s to avoid signature verification failure).
4. Current directory project linkage.
5. Git safety (`.env` in `.gitignore`).
