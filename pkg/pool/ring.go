package pool

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"sort"
)

//we make a consistent hash ring so the servers are evenly distributed.
//each server has by default 200 clones as markers on the ring
//by increasing the size they get more evenly distributed but more memory is required

const serverClones = 200

type hashRing struct {
	clones int
	vnodes []vnode
}

type vnode struct {
	hash uint32
	addr string
}

func newHashRing() *hashRing {
	var ring hashRing
	ring.clones = serverClones
	return &ring
}

func hashKey(key string) uint32 {
	sum := md5.Sum([]byte(key))
	return binary.BigEndian.Uint32(sum[:4])
}

// places a backend onto the ring
// puts markers on the ring for said backend at however many clones there should be
func (ring *hashRing) add(addr string) {
	for i := 0; i < ring.clones; i++ {
		key := fmt.Sprintf("%s#%d", addr, i)
		hash := hashKey(key)
		node := vnode{hash: hash, addr: addr}
		ring.vnodes = append(ring.vnodes, node)
	}

	byHash := func(i, j int) bool {
		return ring.vnodes[i].hash < ring.vnodes[j].hash
	}
	sort.Slice(ring.vnodes, byHash)
}

// takes addr off of the ring
func (ring *hashRing) Remove(addr string) {
	filtered := ring.vnodes[:0]
	for _, v := range ring.vnodes {
		if v.addr != addr {
			filtered = append(filtered, v)
		}
	}
	ring.vnodes = filtered
}

// returns the backend address for key by finding the nearest virtual clone/node
func (ring *hashRing) Get(key string) string {
	if len(ring.vnodes) == 0 {
		return ""
	}

	hash := hashKey(key)

	firstNodeAtOrPast := func(i int) bool {
		return ring.vnodes[i].hash >= hash
	}

	index := sort.Search(len(ring.vnodes), firstNodeAtOrPast)

	if index == len(ring.vnodes) {
		index = 0
	}

	return ring.vnodes[index].addr
}
