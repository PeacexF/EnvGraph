package config

import "os"

// Connect returns the database connection string.
func Connect() string {
	return os.Getenv("DATABASE_URL")
}

// Cache returns the Redis host.
func Cache() string {
	host, ok := os.LookupEnv("REDIS_HOST")
	if !ok {
		return "localhost"
	}
	return host
}
