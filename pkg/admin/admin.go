package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"poker-lb/pkg/pool"
	"strings"
	"time"
)

type Server struct {
	pool                *pool.Pool
	authenticationToken string
	mux                 http.ServeMux
}

// standard event logging for whenever necessary
func (server *Server) logEvent(event, backend string) {

	entry := map[string]any{

		"ts":      time.Now().UTC().Format(time.RFC3339),
		"event":   event,
		"backend": backend,
	}

	data, _ := json.Marshal(entry)
	fmt.Println(string(data))
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

/*
extracts and validates the authentication token.
it has to return a function, if it doesn't, the authentication will run when the server
is booting up without any purpose, so we return a function that it will automatically run when it needs to
(when someone actually tries to authenticate)
*/
func (server *Server) authCheck(next http.HandlerFunc) http.HandlerFunc {

	return func(writer http.ResponseWriter, request *http.Request) {

		authHeader := request.Header.Get("Authorization")
		token, hasBearer := strings.CutPrefix(authHeader, "Bearer ")
		tokenIsValid := hasBearer && token == server.authenticationToken

		if tokenIsValid == false {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="poker-lb admin"`)
			http.Error(writer, "No authorization.", http.StatusUnauthorized)
			return
		}
		next(writer, request)
	}
}

// checks if the method used on the endpoint is GET
// returns information about all backend servers if it is
// their address, status, the last error and when they were last updated
func (server *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {

	if request.Method != http.MethodGet {
		http.Error(writer, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	} else {

		serversInformation := map[string]any{
			"ts":       time.Now().UTC().Format(time.RFC3339),
			"backends": server.pool.ServersInfo(),
		}

		writeJSON(writer, http.StatusOK, serversInformation)

	}
}

/*
before a backend can be taken down for a deploy,
this function is used to tell the load balancer
to stop sending new requests to it,
it's the first step of the drain protocol.

after that /api/poker/drain should be called on the backend
to tell it to flip it's health endpoint to unhealthy so it can
be safely updated

the two endpoints are separate and yes, it can be made into a single one,
though that introduces a lot of issues on it's own

call like POST /drain?backend=10.0.0.10:3001
*/
func (server *Server) handleDrain(writer http.ResponseWriter, request *http.Request) {

	if request.Method != http.MethodPost {
		http.Error(writer, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}

	address := request.URL.Query().Get("backend")
	if address == "" {
		http.Error(writer, "Missing required query param: backend", http.StatusBadRequest)
		return
	}

	if err := server.pool.MarkDraining(address); err != nil {
		http.Error(writer, fmt.Sprintf("Cannot drain: %v", err), http.StatusBadRequest)
		return
	}

	server.logEvent("drain_requested", address)
	writeJSON(writer, http.StatusOK, map[string]any{
		"ok":      true,
		"backend": address,
		"message": "Backend marked draining, now call POST /api/poker/drain on the backend to complete",
	})
}

func NewAdmin(pool *pool.Pool, authenticationToken string) *Server {

	var server Server

	server.pool = pool
	server.authenticationToken = authenticationToken
	server.mux.HandleFunc("/status", server.authCheck(server.handleStatus))
	server.mux.HandleFunc("/drain", server.authCheck(server.handleDrain))

	return &server

}
