package http

import (
	"container/list"
	"fmt"
	"hash/fnv"
	"net/netip"
	"sync"
)

type Cache struct {
	capacity  int
	mu        sync.Mutex
	entries   map[uint64]*list.Element
	values    *list.List
	evictions uint64
}

type CacheStats struct {
	Capacity  int
	Size      int
	Evictions uint64
}

func NewCache(capacity int) *Cache {
	if capacity < 0 {
		capacity = 0
	}
	return &Cache{
		capacity: capacity,
		entries:  make(map[uint64]*list.Element),
		values:   list.New(),
	}
}

func key(ip netip.Addr) uint64 {
	h := fnv.New64a()
	h.Write(ip.AsSlice())
	return h.Sum64()
}

func (c *Cache) Set(ip netip.Addr, resp Response) {
	if c.capacity == 0 {
		return
	}
	k := key(ip)
	c.mu.Lock()
	defer c.mu.Unlock()
	minEvictions := len(c.entries) - c.capacity + 1
	if minEvictions > 0 { // At or above capacity. Shrink the cache
		evicted := 0
		for evicted < minEvictions {
			el := c.values.Front()
			if el == nil {
				break
			}
			delete(c.entries, key(el.Value.(Response).IP))
			c.values.Remove(el)
			evicted++
		}
		c.evictions += uint64(evicted)
	}
	current, ok := c.entries[k]
	if ok {
		c.values.Remove(current)
	}
	c.entries[k] = c.values.PushBack(resp)
}

func (c *Cache) Get(ip netip.Addr) (Response, bool) {
	k := key(ip)
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[k]
	if !ok {
		return Response{}, false
	}
	c.values.MoveToBack(el)
	return el.Value.(Response), true
}

func (c *Cache) Resize(capacity int) error {
	if capacity < 0 {
		return fmt.Errorf("invalid capacity: %d", capacity)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capacity = capacity
	for len(c.entries) > c.capacity {
		el := c.values.Front()
		if el == nil {
			break
		}
		delete(c.entries, key(el.Value.(Response).IP))
		c.values.Remove(el)
		c.evictions++
	}
	return nil
}

func (c *Cache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheStats{
		Size:      len(c.entries),
		Capacity:  c.capacity,
		Evictions: c.evictions,
	}
}
