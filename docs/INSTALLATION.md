# Installing Envil

This guide covers installing the Envil CLI on Linux, macOS, and Windows.

---

## 1. Quick Install via Go

If you already have Go 1.23+ installed:

```bash
go install github.com/ibrahimraimi/envil/cmd/envil@latest
```

Ensure `$GOPATH/bin` (typically `~/go/bin`) is in your shell `PATH`:

```bash
export PATH="$HOME/go/bin:$PATH"
```

Verify installation:
```bash
envil version
```

---

## 2. Pre-Built Binary Releases

Download the latest pre-compiled static binary for your architecture from [GitHub Releases](https://github.com/ibrahimraimi/envil/releases):

### Linux (amd64 / arm64)

```bash
# Example for Linux amd64
curl -sSL -o envil.tar.gz https://github.com/ibrahimraimi/envil/releases/latest/download/envil_Linux_x86_64.tar.gz
tar -xzf envil.tar.gz
sudo mv envil /usr/local/bin/
rm envil.tar.gz
```

### macOS (Apple Silicon / Intel)

```bash
# Example for macOS Apple Silicon (arm64)
curl -sSL -o envil.tar.gz https://github.com/ibrahimraimi/envil/releases/latest/download/envil_Darwin_arm64.tar.gz
tar -xzf envil.tar.gz
sudo mv envil /usr/local/bin/
rm envil.tar.gz
```

### Windows (amd64 / arm64)

Download the `.zip` archive from the Releases page, extract `envil.exe`, and add its folder to your system `PATH`.

---

## 3. Building from Source

```bash
git clone https://github.com/ibrahimraimi/envil.git
cd envil
make build
```

The compiled binaries will be available in `./bin/envil` and `./bin/envil-server`.

---

## 4. Shell Autocompletion

Envil supports autocompletion for `bash`, `zsh`, `fish`, and `powershell`:

### Zsh
```bash
envil completion zsh > "${fpath[1]}/_envil"
```

### Bash
```bash
envil completion bash | sudo tee /etc/bash_completion.d/envil > /dev/null
```

### Fish
```bash
envil completion fish > ~/.config/fish/completions/envil.fish
```

### PowerShell
```powershell
envil completion powershell | Out-String | Invoke-Expression
```
