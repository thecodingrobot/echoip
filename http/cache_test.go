package http

import (
	"fmt"
	"math/rand"
	"net/netip"
	"sync"
	"testing"
)

func TestCacheCapacity(t *testing.T) {
	var tests = []struct {
		addCount, capacity, size int
		evictions                uint64
	}{
		{1, 0, 0, 0},
		{1, 2, 1, 0},
		{2, 2, 2, 0},
		{3, 2, 2, 1},
		{10, 5, 5, 5},
	}
	for i, tt := range tests {
		c := NewCache(tt.capacity)
		var responses []Response
		for i := 0; i < tt.addCount; i++ {
			ip, _ := netip.ParseAddr(fmt.Sprintf("192.0.2.%d", i))
			r := Response{IP: ip}
			responses = append(responses, r)
			c.Set(ip, r)
		}
		if got := len(c.entries); got != tt.size {
			t.Errorf("#%d: len(entries) = %d, want %d", i, got, tt.size)
		}
		if got := c.evictions; got != tt.evictions {
			t.Errorf("#%d: evictions = %d, want %d", i, got, tt.evictions)
		}
		if tt.capacity > 0 && tt.addCount > tt.capacity && tt.capacity == tt.size {
			lastAdded := responses[tt.addCount-1]
			if _, ok := c.Get(lastAdded.IP); !ok {
				t.Errorf("#%d: Get(%s) = (_, %t), want (_, %t)", i, lastAdded.IP.String(), ok, !ok)
			}
			firstAdded := responses[0]
			if _, ok := c.Get(firstAdded.IP); ok {
				t.Errorf("#%d: Get(%s) = (_, %t), want (_, %t)", i, firstAdded.IP.String(), ok, !ok)
			}
		}
	}
}

func TestCacheDuplicate(t *testing.T) {
	c := NewCache(10)
	ip, _ := netip.ParseAddr("192.0.2.1")
	response := Response{IP: ip}
	c.Set(ip, response)
	c.Set(ip, response)
	want := 1
	if got := len(c.entries); got != want {
		t.Errorf("want %d entries, got %d", want, got)
	}
	if got := c.values.Len(); got != want {
		t.Errorf("want %d values, got %d", want, got)
	}
}

func TestCacheResize(t *testing.T) {
	c := NewCache(10)
	for i := 1; i <= 20; i++ {
		ip, _ := netip.ParseAddr(fmt.Sprintf("192.0.2.%d", i))
		r := Response{IP: ip}
		c.Set(ip, r)
	}
	if got, want := len(c.entries), 10; got != want {
		t.Errorf("want %d entries, got %d", want, got)
	}
	if got, want := c.evictions, uint64(10); got != want {
		t.Errorf("want %d evictions, got %d", want, got)
	}
	if err := c.Resize(5); err != nil {
		t.Fatal(err)
	}
	// Resize should evict to fit and accumulate (not reset) the evictions counter.
	if got, want := c.evictions, uint64(15); got != want {
		t.Errorf("want %d evictions, got %d", want, got)
	}
	ip, _ := netip.ParseAddr("192.0.2.42")
	r := Response{IP: ip}
	c.Set(r.IP, r)
	if got, want := len(c.entries), 5; got != want {
		t.Errorf("want %d entries, got %d", want, got)
	}
}

func TestCacheLRUPromotion(t *testing.T) {
	c := NewCache(2)
	ip1, _ := netip.ParseAddr("192.0.2.1")
	ip2, _ := netip.ParseAddr("192.0.2.2")
	ip3, _ := netip.ParseAddr("192.0.2.3")
	c.Set(ip1, Response{IP: ip1})
	c.Set(ip2, Response{IP: ip2})
	// Access ip1 — should promote it to most-recently-used.
	if _, ok := c.Get(ip1); !ok {
		t.Fatalf("expected Get(ip1) to hit")
	}
	// Inserting ip3 should evict the least-recently-used: ip2.
	c.Set(ip3, Response{IP: ip3})
	if _, ok := c.Get(ip1); !ok {
		t.Errorf("ip1 was evicted; expected ip1 to remain after promotion")
	}
	if _, ok := c.Get(ip2); ok {
		t.Errorf("ip2 still present; expected ip2 to be evicted as LRU")
	}
	if _, ok := c.Get(ip3); !ok {
		t.Errorf("ip3 missing; expected ip3 to be present")
	}
}

func TestCacheResizeShrink(t *testing.T) {
	c := NewCache(10)
	for i := 0; i < 10; i++ {
		ip, _ := netip.ParseAddr(fmt.Sprintf("192.0.2.%d", i))
		c.Set(ip, Response{IP: ip})
	}
	if got, want := len(c.entries), 10; got != want {
		t.Fatalf("pre-resize len(entries) = %d, want %d", got, want)
	}
	preEvictions := c.evictions
	if err := c.Resize(5); err != nil {
		t.Fatal(err)
	}
	// Shrink must take effect immediately, not on next Set.
	if got, want := len(c.entries), 5; got != want {
		t.Errorf("post-resize len(entries) = %d, want %d", got, want)
	}
	if got, want := c.values.Len(), 5; got != want {
		t.Errorf("post-resize values.Len = %d, want %d", got, want)
	}
	// Evictions counter must increment by 5, not reset.
	if got, want := c.evictions, preEvictions+5; got != want {
		t.Errorf("post-resize evictions = %d, want %d", got, want)
	}
}

func TestCacheConcurrent(t *testing.T) {
	c := NewCache(100)
	var wg sync.WaitGroup
	const goroutines = 1000
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for j := 0; j < 50; j++ {
				ip, _ := netip.ParseAddr(fmt.Sprintf("192.0.2.%d", r.Intn(256)))
				if r.Intn(2) == 0 {
					c.Set(ip, Response{IP: ip})
				} else {
					c.Get(ip)
				}
			}
		}(int64(i))
	}
	wg.Wait()
	if got := len(c.entries); got > 100 {
		t.Errorf("len(entries) = %d, exceeds capacity 100", got)
	}
}
