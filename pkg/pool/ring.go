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

// uses md5 hash algorithm to turn a string into a number from 0 to the max unsigned 32 int length
// this will be used to place vnodes on the ring and to find existing ones
func hashKey(key string) uint32 {
	stringToBytes := []byte(key)
	sum := md5.Sum(stringToBytes)
	first4bytes := sum[:4]
	return binary.BigEndian.Uint32(first4bytes)
}

// places a backend onto the ring and
// puts markers on the ring for said backend at however many clones there should be
// 1. builds a string 2. hashes the string into a random number 3. sorts the slice (list)
func (ring *hashRing) add(addr string) {
	for i := 0; i < ring.clones; i++ {

		//"builds a string in the format like 10.0.0.10:3001#2"
		key := fmt.Sprintf("%s#%d", addr, i)

		//hashes, turning the string into a random number to be placed onto the ring
		hash := hashKey(key)
		node := vnode{hash: hash, addr: addr}
		ring.vnodes = append(ring.vnodes, node)
	}

	//a function that sort.Slice happened to require and annoyed me
	byHash := func(i, j int) bool {
		return ring.vnodes[i].hash < ring.vnodes[j].hash
	}
	sort.Slice(ring.vnodes, byHash)
}

// takes a server off of the ring.
// completely removes all of a backend's markers. gets called whenever a server is deemed unhealthy.
func (ring *hashRing) Remove(address string) {
	filtered := ring.vnodes[:0]
	for _, v := range ring.vnodes {
		if v.addr != address {
			filtered = append(filtered, v)
		}
	}
	ring.vnodes = filtered
}

/*
1. takes a string and hashes it to a position number
2. finds the first vnode sitting at or past that position on the ring
3. then it returns that vnode's backend address.
*/
func (ring *hashRing) Get(key string) string {
	if len(ring.vnodes) == 0 {
		return ""
	}

	//step 1
	hash := hashKey(key)

	//step2
	firstNodeAtOrPast := func(i int) bool {
		return ring.vnodes[i].hash >= hash
	}

	index := sort.Search(len(ring.vnodes), firstNodeAtOrPast)

	if index == len(ring.vnodes) {
		index = 0
	}

	//step 3
	return ring.vnodes[index].addr
}
