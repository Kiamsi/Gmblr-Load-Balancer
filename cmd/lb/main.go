package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"poker-lb/pkg/config"
)

func main() {

	configPath := flag.String("config", "config/lb.yaml", "path to yaml config file")
	flag.Parse()

	config, err := config.Load(*configPath)

	if err != nil {
		fatalLog("Error when loading config", err)
	}

	authenticationToken := os.Getenv(config.Admin.AuthTokenEnv)

	if authenticationToken == "" {
		fatalLog("Admin authentication token",
			fmt.Errorf("env var %q is not set", config.Admin.AuthTokenEnv))
	}

}

// function for logging critical errors
// separate from the other logs, output goes to Stderr
// once an error is detected, the OS kills the process
func fatalLog(message string, err error) {

	entry := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"event":   "fatal",
		"message": message,
		"error":   err.Error(),
	}

	data, _ := json.Marshal(entry)
	fmt.Fprintln(os.Stderr, string(data))
	os.Exit(1)

}
