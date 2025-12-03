package main

import (
	"filestation/internal/server"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := flag.Int("port", 8080, "Port to run the server on")
	flag.Parse()

	// Hardcoded configuration
	config := server.Config{
		Port:      *port,
		SiteTitle: "文件中转站",
		UploadDir: "uploads",
	}

	// Ensure upload directory exists
	if err := os.MkdirAll(config.UploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}

	srv := server.New(config)

	fmt.Printf("Starting server on port %d...\n", config.Port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", config.Port), srv); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
