package service

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
)

const (
	ProjectConfigFile = ".envil.json"
)

var (
	ErrNotLinked = errors.New("directory is not linked to an envil project, run 'envil init' first")
)

type ProjectConfig struct {
	ProjectID uuid.UUID `json:"project_id"`
}

func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	configPath := filepath.Join(dir, ProjectConfigFile)
	contentBytes, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotLinked
		}
		return nil, fmt.Errorf("read %s: %w", ProjectConfigFile, err)
	}

	var config ProjectConfig
	if err := json.Unmarshal(contentBytes, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ProjectConfigFile, err)
	}

	return &config, nil
}

func SaveProjectConfig(dir string, config *ProjectConfig) error {
	configPath := filepath.Join(dir, ProjectConfigFile)
	contentBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project config: %w", err)
	}

	return os.WriteFile(configPath, contentBytes, 0644)
}

func CheckGitIgnoreProtectsEnv(dir string) bool {
	gitignorePath := filepath.Join(dir, ".gitignore")
	contentBytes, err := os.ReadFile(gitignorePath)
	if err != nil {
		return false
	}

	scanner := bufio.NewScanner(strings.NewReader(string(contentBytes)))
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
		".env.test",
		".env.staging",
		".env.production",
	}

	var discoveredFiles []string
	for _, candidate := range standardCandidates {
		targetPath := filepath.Join(dir, candidate)
		fileInfo, err := os.Stat(targetPath)
		if err == nil && !fileInfo.IsDir() {
			discoveredFiles = append(discoveredFiles, candidate)
		}
	}

	return discoveredFiles
}
