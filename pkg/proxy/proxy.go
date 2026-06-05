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
	transport.MaxIdleConns = maxIdleConnections
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
