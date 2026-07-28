package main

import (
	"fmt"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	level := os.Getenv("LOG_LEVEL")

	// JWT_SECRET is read here but never defined anywhere in the project, which is exactly the kind of gap `envgraph check` reports.
	secret := os.Getenv("JWT_SECRET")

	fmt.Println(port, level, len(secret))
}
