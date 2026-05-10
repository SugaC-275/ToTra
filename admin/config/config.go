package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port          string
	PostgresDSN   string
	JWTSecret     string
	JWTExpiry     time.Duration
	EncryptionKey string
}

func Load() *Config {
	hours, _ := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", "24"))
	pgHost := mustGetEnv("POSTGRES_HOST")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgDB := mustGetEnv("POSTGRES_DB")
	pgUser := mustGetEnv("POSTGRES_USER")
	pgPass := mustGetEnv("POSTGRES_PASSWORD")

	return &Config{
		Port:          getEnv("ADMIN_PORT", "8081"),
		PostgresDSN:   fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable", pgHost, pgPort, pgDB, pgUser, pgPass),
		JWTSecret:     mustGetEnv("JWT_SECRET"),
		JWTExpiry:     time.Duration(hours) * time.Hour,
		EncryptionKey: mustGetEnv("ENCRYPTION_KEY"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env var %s not set", key))
	}
	return v
}
