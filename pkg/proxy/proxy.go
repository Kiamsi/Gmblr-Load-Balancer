package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"poker-lb/pkg/pool"
	"regexp"
	"strings"
	"time"
)

const connectionTimeout = time.Second * 2
const readHeaderTimeout = 5 * time.Second
const idleTimeout = 30 * time.Second
const maxRequestLifetime = 60 * time.Second
const idleConnectionTimeout = 90 * time.Second
const maxIdleConnections = 100

var hopByHopHeaders = []string{ //are removed from forwarded requests
	"Connection",
	"Keep-Alive",
	"Proxy-Connection",
	"TE",
	"Transfer-Encoding",
	"Upgrade",
}

/*
the proxy is responsible for extracting the room id from the url path,
for forwarding requests to the correct backend,
for retrying once on a failed connection,
for allowing the json logs for requests to work properly,
it should only stream the bodies and never buffer
*/
type Proxy struct {
	pool        *pool.Pool     //the pool to pick backends from
	roomIDRegex *regexp.Regexp //the compiled regex for the room ids
	httpClient  *http.Client   //http client so it can send requests
}

/*
   creates a proxy. the http client uses http 1.1 and keep alive to backends

   -1. compiles the regex so it's able to extract room ids
   -2. makes a dialer for establishing the TCP handshake
   -3. makes an http transport using the dialer to open connections
   -4. makes an http client to manage connections and send requests
   -5. initializes the proxy giving it the client

*/

func NewProxy(pool *pool.Pool, roomIDRegex string) (*Proxy, error) {

	regex, err := regexp.Compile(roomIDRegex) // step 1
	if err != nil {
		return nil, fmt.Errorf("compiling room_id regex %q: %w", roomIDRegex, err)
	}

	var dialer net.Dialer // step 2
	dialer.Timeout = connectionTimeout

	var transport http.Transport // step 3
	transport.DialContext = dialer.DialContext
	transport.ForceAttemptHTTP2 = false
	transport.MaxIdleConnsPerHost = maxIdleConnections
	transport.IdleConnTimeout = idleConnectionTimeout
	transport.ResponseHeaderTimeout = readHeaderTimeout
	transport.DisableCompression = true

	var client http.Client //step 4
	client.Transport = &transport
	client.Timeout = maxRequestLifetime
	client.CheckRedirect = func(request *http.Request, list []*http.Request) error {
		return http.ErrUseLastResponse
	}

	var proxy Proxy // step 5
	proxy.pool = pool
	proxy.roomIDRegex = regex
	proxy.httpClient = &client
	return &proxy, nil
}

/*
this function governs how urls will be built for sending requests forward

  - 1. builds the url for the new request to be forwarded to
  - 2. adds it to the provided request, making a new one
  - 3. copies the headers from the provided request into the new one
  - 4. extracts the host ip
  - 5. sets additional headers
  - 6. preserves the original domain so requests are forwarded correctly
*/
func (proxy *Proxy) buildRequest(request *http.Request, backendAddress string) (*http.Request, error) {

	url := fmt.Sprintf("http://%s%s", backendAddress, request.RequestURI) //step 1

	requestToSend, err := http.NewRequestWithContext( //step 2
		request.Context(),
		request.Method,
		url,
		request.Body)

	if err != nil {
		return nil, err
	}

	for key, values := range request.Header { //step 3
		for _, value := range values {
			requestToSend.Header.Add(key, value)
		}
	}

	for _, header := range hopByHopHeaders {
		requestToSend.Header.Del(header)
	}

	clientIp, _, _ := net.SplitHostPort(request.RemoteAddr) //step 4

	// Get prior ip's on the request chain
	prior := requestToSend.Header.Get("X-Forwarded-For") //step 5

	//if there are, append to the end of the chain, if not begin a new chain
	if prior != "" {
		requestToSend.Header.Set("X-Forwarded-For", prior+", "+clientIp)
	} else {
		requestToSend.Header.Set("X-Forwarded-For", clientIp)
	}

	//additional headers for the backend to react accordingly
	requestToSend.Header.Set("X-Forwarded-Proto", "https")
	requestToSend.Header.Set("X-Forwarded-Host", request.Host)
	requestToSend.Header.Set("X-Real-IP", clientIp)

	requestToSend.Host = request.Host //step 6
	return requestToSend, nil
}

// applies the regex to extract a room id
// will be used to determine whether round robin should be used or not
func (proxy *Proxy) extractRoomID(path string) string {

	result := proxy.roomIDRegex.FindStringSubmatch(path)

	if len(result) < 2 {
		return ""
	}

	return result[1]
}

func (proxy *Proxy) logWarn(message, backendAddress string, err error) {
	entry := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"event":   "proxy_warn",
		"backend": backendAddress,
		"msg":     message,
		"error":   err.Error(),
	}
	data, jsonerror := json.Marshal(entry)
	if jsonerror != nil {
		fmt.Printf(
			"some error occured inside proxy with the log warning - "+
				"just know you're being warned by it: %v\n",
			jsonerror)
		return
	}
	fmt.Println(string(data))
}

// reports if an error is a context deadline or a network timeout
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
		return true
	}
	return strings.Contains(err.Error(), "context deadline exceeded")
}

// copies headers from an incoming request into an outgoing request
func copyResponseHeaders(destination, source http.Header) {
	hop := make(map[string]bool, len(hopByHopHeaders))
	for _, header := range hopByHopHeaders {
		hop[strings.ToLower(header)] = true
	}
	for key, values := range source {
		if hop[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}
