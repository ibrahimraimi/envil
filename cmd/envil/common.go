package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
)

const (
	PROJECT_CONFIG_FILE = ".envil.json"
)

var (
	ErrNotLinked = errors.New("directory is not linked to an envil project, run 'envil init' first")
)

type ProjectConfig struct {
	ProjectID uuid.UUID `json:"project_id"`
}

func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	configPath := filepath.Join(dir, PROJECT_CONFIG_FILE)
	fileBytes, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotLinked
		}
		return nil, fmt.Errorf("read %s: %w", PROJECT_CONFIG_FILE, err)
	}

	var config ProjectConfig
	if err := json.Unmarshal(fileBytes, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", PROJECT_CONFIG_FILE, err)
	}

	return &config, nil
}

func SaveProjectConfig(dir string, config *ProjectConfig) error {
	configPath := filepath.Join(dir, PROJECT_CONFIG_FILE)
	fileBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}

	return os.WriteFile(configPath, fileBytes, 0644)
}

func ResolveClientAndKeys(ks keystore.Keystore, serverURL string) (*client.Client, *crypto.Keys, error) {
	keys, err := ks.GetKeys()
	if err != nil {
		return nil, nil, err
	}

	apiClient := client.NewClient(serverURL, keys.Signing)
	return apiClient, keys, nil
}

func CheckGitIgnoreProtectsEnv(dir string) bool {
	gitignorePath := filepath.Join(dir, ".gitignore")
	fileBytes, err := os.ReadFile(gitignorePath)
	if err != nil {
		return false
	}

	scanner := bufio.NewScanner(strings.NewReader(string(fileBytes)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == ".env" || line == "*.env" || line == ".env*" {
			return true
		}
	}
	return false
}

func DiscoverGitRemote(dir string) string {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = dir
	outputBytes, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(outputBytes))
}

func FindDefaultEnvFiles(dir string) []string {
	standardCandidates := []string{
		".env",
		".env.local",
		".env.development",
		".env.staging",
		".env.production",
	}

	existingFiles := make([]string, 0)
	for _, candidate := range standardCandidates {
		path := filepath.Join(dir, candidate)
		if fileInfo, err := os.Stat(path); err == nil && !fileInfo.IsDir() {
			existingFiles = append(existingFiles, candidate)
		}
	}

	return existingFiles
}
