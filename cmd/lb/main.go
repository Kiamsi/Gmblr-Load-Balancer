package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"poker-lb/pkg/admin"
	"poker-lb/pkg/config"
	"poker-lb/pkg/health"
	"poker-lb/pkg/pool"
	"poker-lb/pkg/proxy"
)

func main() {

	configPath := flag.String("config", "config/lb.yaml", "path to yaml config file")
	flag.Parse()

	configuration, err := config.Load(*configPath)

	if err != nil {
		fatalLog("Error when loading config", err)
	}

	authenticationToken := os.Getenv(configuration.Admin.AuthTokenEnv)

	if authenticationToken == "" {
		fatalLog("Admin authentication token",
			fmt.Errorf("env var %q is not set", configuration.Admin.AuthTokenEnv))
	}

	addresses := make([]string, len(configuration.Backends))

	for i, backend := range configuration.Backends {
		addresses[i] = backend.Address
	}

	pool := pool.New(addresses,
		configuration.Health.FailThreshold,
		configuration.Health.PassThreshold)

	prx, err := proxy.NewProxy(pool, configuration.Stickiness.RoomIDRegex)

	if err != nil {

		fatalLog("Error when creating proxy", err)

	}

	prober := health.NewProber(pool,
		configuration.Health.Path,
		configuration.Health.IntervalS,
		configuration.Health.FailThreshold,
		configuration.Health.PassThreshold)

	adminPanel := admin.NewAdmin(pool, authenticationToken)

	var mainServer http.Server
	mainServer.Addr = configuration.Listen
	mainServer.Handler = prx
	mainServer.IdleTimeout = proxy.IdleTimeout

	var adminServer http.Server
	adminServer.Addr = configuration.Admin.Listen
	adminServer.Handler = adminPanel.Handler()

	contex, cancel := context.WithCancel(context.Background())

	go prober.Run(contex)

	defer cancel()

	go func() {

		serverError := mainServer.ListenAndServe()

		if serverError != nil && serverError != http.ErrServerClosed {
			fatalLog("Main server failed to start up", serverError)
		}
	}()

	go func() {

		serverError := adminServer.ListenAndServe()

		if serverError != nil && serverError != http.ErrServerClosed {
			fatalLog("Admin server failed to start up", serverError)
		}
	}()

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

// for logging when the process doesn't need to be killed
func logInfo(msg string, fields map[string]any) {

	entry := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"level": "info",
		"msg":   msg,
	}

	for k, v := range fields {
		entry[k] = v
	}

	data, _ := json.Marshal(entry)
	fmt.Println(string(data))
}
