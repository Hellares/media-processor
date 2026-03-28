package config

import (
	"os"
	"runtime"
	"strconv"
)

type Config struct {
	Port              string
	MaxWorkers        int
	QueueSize         int
	MaxImageSizeMB    int
	MaxVideoSizeMB    int
	ProcessTimeout    int // seconds
	LogLevel          string
	APIKey            string // Simple auth key
	TempCleanupMins   int    // limpiar temps cada X minutos
}

func Load() *Config {
	cpus := runtime.NumCPU()
	workers := cpus - 1
	if workers < 1 {
		workers = 1
	}

	return &Config{
		Port:            getEnv("PORT", "4000"),
		MaxWorkers:      getEnvInt("MAX_WORKERS", workers),
		QueueSize:       getEnvInt("QUEUE_SIZE", 500),
		MaxImageSizeMB:  getEnvInt("MAX_IMAGE_SIZE_MB", 10),
		MaxVideoSizeMB:  getEnvInt("MAX_VIDEO_SIZE_MB", 200),
		ProcessTimeout:  getEnvInt("PROCESSING_TIMEOUT", 30),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		APIKey:          getEnv("API_KEY", ""),
		TempCleanupMins: getEnvInt("TEMP_CLEANUP_MINS", 5),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
