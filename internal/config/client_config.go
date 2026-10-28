package config

import (
	"os"
)

const (
	DEFAULT_SERVER_URL = "http://localhost:8080"
	CLI_VERSION        = "0.1.1"
)

type ClientConfig struct {
	ServerURL string
}

func LoadClientConfig(serverFlag string) ClientConfig {
	serverURL := serverFlag
	if serverURL == "" {
		serverURL = os.Getenv("ENVIL_SERVER_URL")
	}
	if serverURL == "" {
		serverURL = DEFAULT_SERVER_URL
	}

	return ClientConfig{
		ServerURL: serverURL,
	}
}
