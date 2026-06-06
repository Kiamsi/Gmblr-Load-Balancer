package proxy

import (
	"fmt"
	"net"
	"net/http"
	"poker-lb/pkg/pool"
	"regexp"
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

	url := fmt.Sprintf("http://%s%s", backendAddress, request.RequestURI)

	requestToSend, err := http.NewRequestWithContext(
		request.Context(),
		request.Method,
		url,
		request.Body)

	if err != nil {
		return nil, err
	}

	for key, values := range request.Header { //
		for _, value := range values {
			requestToSend.Header.Add(key, value)
		}
	}

	for _, header := range hopByHopHeaders {
		requestToSend.Header.Del(header)
	}

	clientIp, _, _ := net.SplitHostPort(request.RemoteAddr)

	// Get prior ip's on the request chain
	prior := requestToSend.Header.Get("X-Forwarded-For")

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

	requestToSend.Host = request.Host
	return requestToSend, nil
}
