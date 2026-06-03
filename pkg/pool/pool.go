package pool

import (
	"sync"
	"time"
)

type Status int

const StatusHealthy Status = 0   //a healthy server
const StatusDraining Status = 1  //excluded from round robin
const StatusUnhealthy Status = 2 //removed from the ring

func (condition Status) String() string {
	switch condition {
	case StatusHealthy:
		return "healthy"
	case StatusDraining:
		return "draining"
	case StatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// backend holds all state for one upstream server.
// All fields except Addr are protected by Pool.mu.
type backend struct {
	Addr             string // immutable — safe to read without a lock
	status           Status
	consecutiveFails int
	consecutivePass  int
	lastError        string
	updatedAt        time.Time
}

// this is a read only struct only used by the admin on the /status endpoint
// technically we can just put json tags on the existing backend struct
// but i think this is cleaner and still fine
type BackendInfo struct {
	Addr      string    `json:"addr"`
	Status    string    `json:"status"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// i found that this mutex version is better for use cases with a lot of reads but less writes like this one
type Pool struct {
	mutex           sync.RWMutex
	backends        map[string]*backend
	ring            *hashRing
	roundRobinIndex uint64 //it needs to be unsigned...
	failThreshold   int
	passThreshold   int
}

// this function creates a Pool with the given backend addresses
// all servers start as healthy and are placed on the ring
func New(addrs []string, failThreshold, passThreshold int) *Pool {
	var pool Pool
	pool.backends = make(map[string]*backend, len(addrs))
	pool.ring = newHashRing()
	pool.failThreshold = failThreshold
	pool.passThreshold = passThreshold

	for _, addr := range addrs {
		var b backend
		b.Addr = addr
		b.status = StatusHealthy
		b.updatedAt = time.Now()
		pool.backends[addr] = &b
		pool.ring.add(addr)
	}

	return &pool
}
