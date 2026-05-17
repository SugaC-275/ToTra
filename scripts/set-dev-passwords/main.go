// Dev-only tool: sets bcrypt password_hash for all dev users.
// Usage: POSTGRES_HOST=localhost POSTGRES_DB=totra POSTGRES_USER=totra POSTGRES_PASSWORD=totra_secret go run ./scripts/set-dev-passwords/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	password := os.Getenv("DEV_PASSWORD")
	if password == "" {
		password = "totra123"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("bcrypt: %v", err)
	}

	dsn := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		mustEnv("POSTGRES_HOST"),
		getEnv("POSTGRES_PORT", "5432"),
		mustEnv("POSTGRES_DB"),
		mustEnv("POSTGRES_USER"),
		mustEnv("POSTGRES_PASSWORD"),
	)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	tag, err := pool.Exec(context.Background(),
		`UPDATE users SET password_hash = $1 WHERE password_hash IS NULL`,
		string(hash),
	)
	if err != nil {
		log.Fatalf("update: %v", err)
	}
	fmt.Printf("Updated %d user(s) with password: %s\n", tag.RowsAffected(), password)
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("required env var %s not set", k)
	}
	return v
}

func getEnv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
