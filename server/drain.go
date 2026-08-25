package main

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type admissionConfig struct {
	maxSessions       int64
	maxHandshakes     int64
	maxGoroutines     int
	minFreeFDs        int64
	canary            bool
	canaryMaxSessions int64
}

func (c admissionConfig) effectiveMaxSessions() int64 {
	limit := c.maxSessions
	if c.canary && c.canaryMaxSessions > 0 && (limit <= 0 || c.canaryMaxSessions < limit) {
		limit = c.canaryMaxSessions
	}
	return limit
}

// connectionGate closes admission atomically before Wait begins. It also
// bounds new work while allowing established sessions to finish normally.
type connectionGate struct {
	mu          sync.Mutex
	draining    bool
	wg          sync.WaitGroup
	active      atomic.Int64
	established atomic.Int64
	handshakes  atomic.Int64
	config      admissionConfig
	resources   *resourceMonitor
	metrics     *serverMetrics
}

type connectionLease struct {
	gate        *connectionGate
	established atomic.Bool
	doneOnce    sync.Once
}

func newConnectionGate(config admissionConfig, resources *resourceMonitor, metrics *serverMetrics) *connectionGate {
	return &connectionGate{config: config, resources: resources, metrics: metrics}
}

func (g *connectionGate) rejectionReasonLocked() string {
	if g.draining {
		return admissionReasonDrain
	}
	if limit := g.config.effectiveMaxSessions(); limit > 0 && g.active.Load() >= limit {
		return admissionReasonSessions
	}
	if limit := g.config.maxHandshakes; limit > 0 && g.handshakes.Load() >= limit {
		return admissionReasonHandshakes
	}
	if limit := g.config.maxGoroutines; limit > 0 && runtime.NumGoroutine() >= limit {
		return admissionReasonGoroutines
	}
	if g.config.minFreeFDs > 0 && g.resources != nil {
		openFDs, maxFDs := g.resources.fdCounts()
		if openFDs >= 0 && maxFDs > 0 && maxFDs-openFDs < g.config.minFreeFDs {
			return admissionReasonFDs
		}
	}
	return ""
}

func (g *connectionGate) admit() (*connectionLease, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if reason := g.rejectionReasonLocked(); reason != "" {
		g.metrics.recordAdmissionRejected(reason)
		return nil, reason
	}
	g.wg.Add(1)
	g.active.Add(1)
	g.handshakes.Add(1)
	g.metrics.sessionsAdmitted.Add(1)
	return &connectionLease{gate: g}, ""
}

func (l *connectionLease) markEstablished() {
	if !l.established.CompareAndSwap(false, true) {
		return
	}
	l.gate.handshakes.Add(-1)
	l.gate.established.Add(1)
}

func (l *connectionLease) done() {
	l.doneOnce.Do(func() {
		if l.established.Load() {
			l.gate.established.Add(-1)
		} else {
			l.gate.handshakes.Add(-1)
		}
		l.gate.active.Add(-1)
		l.gate.wg.Done()
	})
}

func (g *connectionGate) beginDrain() int64 {
	g.mu.Lock()
	g.draining = true
	active := g.active.Load()
	g.mu.Unlock()
	g.metrics.draining.Store(true)
	g.metrics.drainStarted.Add(1)
	return active
}

func (g *connectionGate) finishDrain(forced bool) {
	g.metrics.draining.Store(false)
	if forced {
		g.metrics.drainForced.Add(1)
	} else {
		g.metrics.drainCompleted.Add(1)
	}
}

func (g *connectionGate) wait() {
	g.wg.Wait()
}

func (g *connectionGate) activeCount() int64 {
	return g.active.Load()
}

func (g *connectionGate) establishedCount() int64 {
	return g.established.Load()
}

func (g *connectionGate) handshakeCount() int64 {
	return g.handshakes.Load()
}

func (g *connectionGate) readiness() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if reason := g.rejectionReasonLocked(); reason != "" {
		return false, reason
	}
	return true, "ready"
}

const (
	admissionReasonDrain      = "drain"
	admissionReasonSessions   = "sessions"
	admissionReasonHandshakes = "handshakes"
	admissionReasonGoroutines = "goroutines"
	admissionReasonFDs        = "file_descriptors"
)
