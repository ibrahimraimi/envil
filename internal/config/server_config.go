package config

import (
	"os"
	"strconv"
)

type ServerConfig struct {
	ListenAddr     string
	DatabaseURL    string
	RateLimitRPS   float64
	RateLimitBurst int
	MaxBlobBytes   int
}

func LoadServerConfig() ServerConfig {
	listenAddr := getEnvOrDefault("LISTEN_ADDR", ":8080")
	databaseURL := os.Getenv("DATABASE_URL")
	rps := getEnvFloatOrDefault("RATE_LIMIT_RPS", 200.0)
	burst := getEnvIntOrDefault("RATE_LIMIT_BURST", 400)
	maxBytes := getEnvIntOrDefault("MAX_BLOB_BYTES", 1024*1024)

	return ServerConfig{
		ListenAddr:     listenAddr,
		DatabaseURL:    databaseURL,
		RateLimitRPS:   rps,
		RateLimitBurst: burst,
		MaxBlobBytes:   maxBytes,
	}
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvIntOrDefault(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvFloatOrDefault(key string, fallback float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
