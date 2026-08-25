package main

import (
	"sync"
	"sync/atomic"
)

// connectionGate closes admission atomically before Wait begins. A plain
// WaitGroup is not enough here: Accept may otherwise call Add concurrently
// with shutdown's Wait.
type connectionGate struct {
	mu       sync.Mutex
	draining bool
	wg       sync.WaitGroup
	active   atomic.Int64
}

func (g *connectionGate) admit() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.draining {
		return false
	}
	g.wg.Add(1)
	g.active.Add(1)
	return true
}

func (g *connectionGate) done() {
	g.active.Add(-1)
	g.wg.Done()
}

func (g *connectionGate) beginDrain() int64 {
	g.mu.Lock()
	g.draining = true
	active := g.active.Load()
	g.mu.Unlock()
	return active
}

func (g *connectionGate) wait() {
	g.wg.Wait()
}

func (g *connectionGate) activeCount() int64 {
	return g.active.Load()
}
