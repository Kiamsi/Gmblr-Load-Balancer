package pool

import (
	"sort"
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
	Address          string
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
	Address   string    `json:"addr"`
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

	for _, address := range addresses {
		var b backend
		b.Address = address
		b.status = StatusHealthy
		b.updatedAt = time.Now()
		pool.backends[address] = &b
		pool.ring.add(address)
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
func (p *Pool) healthySlice() []*backend {
	result := make([]*backend, 0, len(p.backends))
	for _, b := range p.backends {
		if b.status == StatusHealthy {
			result = append(result, b)
		}
	}
	byAddress := func(i, j int) bool {
		return result[i].Address < result[j].Address
	}
	sort.Slice(result, byAddress)
	return result
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

// used to return the address of a backend pointer
func BackendAddress(backend *backend) string {
	if backend == nil {
		return ""
	}
	return backend.Address
}

/*
this function is called by the health checker each time a health check fails.
in this case, it updates the given backend's fails and passes, saves the reason for the fail
and if the fails reach the limit it takes the server off the ring
*/
func (pool *Pool) LogFail(address, reason string) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	backend, addressExists := pool.backends[address]

	if addressExists == false {
		return
	}

	backend.consecutiveFails++
	backend.consecutivePass = 0
	backend.lastError = reason
	backend.updatedAt = time.Now()

	if backend.consecutiveFails >= pool.failThreshold && backend.status != StatusUnhealthy {
		backend.status = StatusUnhealthy
		pool.ring.Remove(address)
	}
}

// same function, but skips the fail counting
// used to take a server off instantly, for whenever it's draining or unhealthy
func (pool *Pool) LogFailInstant(address, reason string) {
	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	backend, addressExists := pool.backends[address]
	if addressExists == false {
		return
	}
	backend.consecutiveFails = pool.failThreshold
	backend.consecutivePass = 0
	backend.lastError = reason
	backend.updatedAt = time.Now()

	if backend.status != StatusUnhealthy {
		backend.status = StatusUnhealthy
		pool.ring.Remove(address)
	}
}

/*
	this function is called each time a server pings back as healthy

	1. gets reading and writing permission
	2. saves the current status of the server before changing
	3. updates the pass and fail counters, eventually marks server as healthy
	4. if the server was unhealthy but not draining it gets readded
*/

func (pool *Pool) LogSuccess(address string) {
	pool.mutex.Lock() //step 1
	defer pool.mutex.Unlock()

	backend, addressExists := pool.backends[address]
	if addressExists == false {
		return
	}
	wasUnhealthy := backend.status == StatusUnhealthy //step 2
	backend.consecutivePass++                         //step 3
	backend.consecutiveFails = 0
	backend.updatedAt = time.Now()

	if backend.consecutivePass >= pool.passThreshold && backend.status != StatusHealthy {
		backend.status = StatusHealthy
		backend.lastError = ""
		if wasUnhealthy { //step 4

			pool.ring.add(address)
		}
	}
}
