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
	return &hashRing{clones: serverClones}
}

// places addr onto the ring
func (r *hashRing) add(addr string) {
	for i := 0; i < r.clones; i++ {
		key := fmt.Sprintf("%s#%d", addr, i)
		r.vnodes = append(r.vnodes, vnode{
			hash: hashKey(key),
			addr: addr,
		})
	}
	sort.Slice(r.vnodes, func(i, j int) bool {
		return r.vnodes[i].hash < r.vnodes[j].hash
	})
}

// takes addr off of the ring
func (r *hashRing) Remove(addr string) {
	filtered := r.vnodes[:0]
	for _, v := range r.vnodes {
		if v.addr != addr {
			filtered = append(filtered, v)
		}
	}
	r.vnodes = filtered
}

// returns the backend address for key by finding the nearest virtual clone/node
func (r *hashRing) Get(key string) string {
	if len(r.vnodes) == 0 {
		return ""
	}
	h := hashKey(key)
	idx := sort.Search(len(r.vnodes), func(i int) bool {
		return r.vnodes[i].hash >= h
	})
	if idx == len(r.vnodes) {
		idx = 0
	}
	return r.vnodes[idx].addr
}

func hashKey(key string) uint32 {
	sum := md5.Sum([]byte(key))
	return binary.BigEndian.Uint32(sum[:4])
}
