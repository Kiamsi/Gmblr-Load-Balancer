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
The main function uses the rest of the files to start the load balancer and shut it down

1. Pulls the config path and parses it
2. Loads configuration
3. Initiates the pool (the persistent hash ring and round robin algorithm)
4. Creates the proxy (for handling incoming and outgoing requests)
5. Creates the prober (for checking the health status of backend server on an interval)
6. Creates the servers where the load balancer and the admin panel will run
7. Makes a channel that intercepts system calls for shutdown, and the program waits there
8. Logs the shutdown and prepares it
9. Makes a channel listening to for the shutdown of the main and admin server
10. Spawns goroutines for it
11. Catches errors if any
*/
func main() {

	configPath := flag.String("config", "config/lb.yaml", "path to yaml config file") //step 1
	flag.Parse()

	configuration, err := config.Load(*configPath) //step 2

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

	pool := pool.New(addresses, //step 3
		configuration.Health.FailThreshold,
		configuration.Health.PassThreshold)

	prx, err := proxy.NewProxy(pool, configuration.Stickiness.RoomIDRegex) //step 4

	if err != nil {

		fatalLog("Error when creating proxy", err)

	}

	prober := health.NewProber(pool, //step 5
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

	//creates the admin interface with the appropriate token and handler functions
	adminPanel := admin.NewAdmin(pool, authenticationToken)

	var mainServer http.Server //step 6 (load balancer's server)
	mainServer.Addr = configuration.Listen
	mainServer.Handler = prx
	mainServer.IdleTimeout = proxy.IdleTimeout

	go func() {

		serverError := mainServer.ListenAndServe()

		if serverError != nil && serverError != http.ErrServerClosed {
			fatalLog("Main server failed to start up", serverError)
		}
	}()

	var adminServer http.Server //step 6 (admin's server)
	adminServer.Addr = configuration.Admin.Listen
	adminServer.Handler = adminPanel.Handler()

	go func() {

		serverError := adminServer.ListenAndServe()

		if serverError != nil && serverError != http.ErrServerClosed {
			fatalLog("Admin server failed to start up", serverError)
		}
	}()

	//buffered by one so a very early signal doesn't get dropped
	quit := make(chan os.Signal, 1) //step 7

	//prepares to catch these specific system calls,
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	//program pauses here. it waits until something comes up on the quit channel (the specified system calls)
	channelSignal := <-quit

	logInfo("Process is shutting down", map[string]any{"Signal": channelSignal.String()}) //step 8
	cancel()

	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(),
		30*time.Second)
	defer shutdownCancel()

	done := make(chan error, 2) //step 9

	go func() { done <- mainServer.Shutdown(shutdownContext) }() //step 10
	go func() { done <- adminServer.Shutdown(shutdownContext) }()

	for i := 1; i <= 2; i++ { //step 11

		err := <-done

		if err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}

	//end of program
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
func logInfo(message string, fields map[string]any) {

	entry := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"level":     "info",
		"message":   message,
	}

	for key, value := range fields {
		entry[key] = value
	}

	data, _ := json.Marshal(entry)
	fmt.Println(string(data))
}
