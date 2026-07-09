package config

import (
	"log"

	"github.com/joho/godotenv"
)

func LoadConfig() {
	// In production (Render), there's no .env file — env vars are injected
	// by the host. So a missing .env is fine; only log it, don't crash.
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using environment variables from the system")
	}
}
