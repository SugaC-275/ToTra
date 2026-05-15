package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port           string
	PostgresDSN    string
	RedisAddr      string
	RedisPassword  string
	EncryptionKey  string
	JWTSecret      string
	AgentLoopLimit int64
	ParserURL      string
}

func Load() (*Config, error) {
	port := getEnv("GATEWAY_PORT", "8080")
	pgHost := mustGetEnv("POSTGRES_HOST")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgDB := mustGetEnv("POSTGRES_DB")
	pgUser := mustGetEnv("POSTGRES_USER")
	pgPass := mustGetEnv("POSTGRES_PASSWORD")

	loopLimit := int64(20)
	if v := os.Getenv("AGENT_LOOP_LIMIT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			loopLimit = n
		}
	}

	return &Config{
		Port:           port,
		PostgresDSN:    fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable", pgHost, pgPort, pgDB, pgUser, pgPass),
		RedisAddr:      fmt.Sprintf("%s:%s", getEnv("REDIS_HOST", "localhost"), getEnv("REDIS_PORT", "6379")),
		RedisPassword:  os.Getenv("REDIS_PASSWORD"),
		EncryptionKey:  mustGetEnv("GATEWAY_ENCRYPTION_KEY"),
		JWTSecret:      mustGetEnv("JWT_SECRET"),
		AgentLoopLimit: loopLimit,
		ParserURL:      getEnv("PARSER_URL", "http://localhost:8090"),
	}, nil
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
		panic(fmt.Sprintf("required env var %s is not set", key))
	}
	return v
}
