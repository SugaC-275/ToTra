package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           string
	PostgresDSN    string
	JWTSecret      string
	JWTExpiry      time.Duration
	EncryptionKey  string
	InternalSecret string
}

func Load() *Config {
	hours, _ := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", "24"))
	pgHost := mustGetEnv("POSTGRES_HOST")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgDB := mustGetEnv("POSTGRES_DB")
	pgUser := mustGetEnv("POSTGRES_USER")
	pgPass := mustGetEnv("POSTGRES_PASSWORD")
	pgSSLMode := getEnv("POSTGRES_SSLMODE", "disable")
	internalSecret := mustGetEnv("INTERNAL_SECRET")

	return &Config{
		Port:           getEnv("ADMIN_PORT", "8081"),
		PostgresDSN:    fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=%s", pgHost, pgPort, pgDB, pgUser, pgPass, pgSSLMode),
		JWTSecret:      mustGetEnv("JWT_SECRET"),
		JWTExpiry:      time.Duration(hours) * time.Hour,
		EncryptionKey:  mustGetEnv("ENCRYPTION_KEY"),
		InternalSecret: internalSecret,
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
