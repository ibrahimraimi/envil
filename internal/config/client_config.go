package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DEFAULT_SERVER_URL    = "http://localhost:8080"
	DEFAULT_BACKUP_COPIES = 3
	CLI_VERSION           = "0.1.2"
	CONFIG_DIR_NAME       = "envil"
	CONFIG_FILE_NAME      = "config.json"
)

type ClientConfig struct {
	ServerURL    string
	BackupCopies int
	GitCheck     bool
}

type rawClientConfig struct {
	ServerURL    *string `json:"server_url,omitempty"`
	BackupCopies *int    `json:"backup_copies,omitempty"`
	GitCheck     *bool   `json:"git_check,omitempty"`
}

func GetDefaultConfigDir() (string, error) {
	if customDir := os.Getenv("ENVIL_CONFIG_DIR"); customDir != "" {
		return customDir, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(configDir, CONFIG_DIR_NAME), nil
}

func GetDefaultConfigFilePath() (string, error) {
	dir, err := GetDefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, CONFIG_FILE_NAME), nil
}

func LoadClientConfig(serverFlag string) ClientConfig {
	configPath, _ := GetDefaultConfigFilePath()
	return LoadClientConfigFromPath(configPath, serverFlag)
}

func LoadClientConfigFromPath(configPath, serverFlag string) ClientConfig {
	cfg := ClientConfig{
		ServerURL:    DEFAULT_SERVER_URL,
		BackupCopies: DEFAULT_BACKUP_COPIES,
		GitCheck:     true,
	}

	if configPath != "" {
		if rawBytes, err := os.ReadFile(configPath); err == nil {
			var parsed rawClientConfig
			if json.Unmarshal(rawBytes, &parsed) == nil {
				if parsed.ServerURL != nil && *parsed.ServerURL != "" {
					cfg.ServerURL = *parsed.ServerURL
				}
				if parsed.BackupCopies != nil && *parsed.BackupCopies >= 0 {
					cfg.BackupCopies = *parsed.BackupCopies
				}
				if parsed.GitCheck != nil {
					cfg.GitCheck = *parsed.GitCheck
				}
			}
		}
	}

	if envURL := os.Getenv("ENVIL_SERVER_URL"); envURL != "" {
		cfg.ServerURL = envURL
	} else if envServer := os.Getenv("ENVIL_SERVER"); envServer != "" {
		cfg.ServerURL = envServer
	}

	if serverFlag != "" {
		cfg.ServerURL = serverFlag
	}

	return cfg
}
