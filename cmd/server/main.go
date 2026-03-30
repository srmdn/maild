package main

import (
	"log"

	"github.com/srmdn/maild/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatalf("maild exited with error: %v", err)
	}
}
