package pool

import (
	"sync"
	"sync/atomic"
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
// all fields except Addr are protected by Pool.mu. (the mutex)
type backend struct {
	Addr             string
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
func New(addresses []string, failThreshold, passThreshold int) *Pool {
	var pool Pool
	pool.backends = make(map[string]*backend, len(addresses))
	pool.ring = newHashRing()
	pool.failThreshold = failThreshold
	pool.passThreshold = passThreshold

	for _, addr := range addresses {
		var b backend
		b.Addr = addr
		b.status = StatusHealthy
		b.updatedAt = time.Now()
		pool.backends[addr] = &b
		pool.ring.add(addr)
	}

	return &pool
}

/*
the proxy will call this when a request with a room id comes in the url

1. gets a permission to read from the pool
2. calls the get function to see which backend owns this room id
3. looks up the backend in the map by its address and returns it

the proxy will use said address to know which server to forward a request to
*/
func (pool *Pool) PickByRoomID(roomID string) *backend {

	pool.mutex.RLock()         //gets a read lock (step 1)
	defer pool.mutex.RUnlock() //will give it back after the function returns

	address := pool.ring.Get(roomID) // step 2

	if address == "" {
		return nil
	}
	return pool.backends[address] //step 3
}

/*
healthySlice returns current backends with a healthy status. pool.mutex is necessary

1. creates an empty slice/list and allocates enough space incase all backends are healthy
2. loops through every server and if it's healthy adds it to the list
3. returns the slice of healthy servers
*/
func (pool *Pool) healthySlice() []*backend {
	result := make([]*backend, 0, len(pool.backends)) //step 1
	for _, b := range pool.backends {
		if b.status == StatusHealthy {
			result = append(result, b) //step 2
		}
	}
	return result // step 3
}

/*
this function is called by the proxy whenever a request without a room id comes in

1. it gets permission to read
2. it secures a list of all currently healthy backends
3. it atomically increments a counter
4. deals request one by one to each server
*/
func (pool *Pool) PickRoundRobin() *backend {
	pool.mutex.RLock() // step 1
	defer pool.mutex.RUnlock()

	healthy := pool.healthySlice() // step 2
	if len(healthy) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&pool.roundRobinIndex, 1) - 1 // (step 3) atomic addition so only one request writes at a time
	return healthy[int(idx)%len(healthy)]                 // (step 4) thanks to whoever wrote this on stack overflow
}
