package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"poker-lb/pkg/admin"
	"poker-lb/pkg/config"
	"poker-lb/pkg/health"
	"poker-lb/pkg/pool"
	"poker-lb/pkg/proxy"
)

/*
1. Pulls the config path and parses it
2. Loads configuration
3. Initiates the pool (the persistent hash ring and round robin algorithm)
4. Creates the proxy (for handling incoming and outgoing requests)
5. Creates the prober (for checking the health status of backend server on an interval)
6. Creates the servers where the load balancer and the admin panel will run
*/
func main() {

	configPath := flag.String("config", "config/lb.yaml", "path to yaml config file")
	flag.Parse()

	configuration, err := config.Load(*configPath)

	if err != nil {
		fatalLog("Error when loading config", err)
	}

	logInfo("config loaded", map[string]any{
		"listen":        configuration.Listen,
		"admin_listen":  configuration.Admin.Listen,
		"backend_count": len(configuration.Backends),
	})

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

	contex, cancel := context.WithCancel(context.Background())
	defer cancel()

	go prober.Run(contex)

	logInfo("health prober started", map[string]any{
		"path":           configuration.Health.Path,
		"interval_s":     configuration.Health.IntervalS,
		"fail_threshold": configuration.Health.FailThreshold,
		"pass_threshold": configuration.Health.PassThreshold,
	})

	adminPanel := admin.NewAdmin(pool, authenticationToken)

	var mainServer http.Server
	mainServer.Addr = configuration.Listen
	mainServer.Handler = prx
	mainServer.IdleTimeout = proxy.IdleTimeout

	go func() {

		serverError := mainServer.ListenAndServe()

		if serverError != nil && serverError != http.ErrServerClosed {
			fatalLog("Main server failed to start up", serverError)
		}
	}()

	var adminServer http.Server
	adminServer.Addr = configuration.Admin.Listen
	adminServer.Handler = adminPanel.Handler()

	go func() {

		serverError := adminServer.ListenAndServe()

		if serverError != nil && serverError != http.ErrServerClosed {
			fatalLog("Admin server failed to start up", serverError)
		}
	}()

	quit := make(chan os.Signal, 1) //buffered by one so a very early signal doesn't get dropped

	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	channelSignal := <-quit

	logInfo("Process is shutting down", map[string]any{"Signal": channelSignal.String()})
	cancel()

	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(),
		30*time.Second)
	defer shutdownCancel()

	done := make(chan error, 2)

	go func() { done <- mainServer.Shutdown(shutdownContext) }()
	go func() { done <- adminServer.Shutdown(shutdownContext) }()

	for i := 1; i <= 2; i++ {

		err := <-done

		if err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}

	logInfo("Shutdown completed", nil)
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
