package proxy

import (
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
