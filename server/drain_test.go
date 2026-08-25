package main

import (
	"sync"
	"testing"
)

func TestConnectionGateStopsAdmissionBeforeWait(t *testing.T) {
	metrics := newServerMetrics()
	gate := newConnectionGate(admissionConfig{}, nil, metrics)
	leases := make([]*connectionLease, 0, 2)
	for i := 0; i < 2; i++ {
		lease, reason := gate.admit()
		if lease == nil {
			t.Fatalf("gate refused session %d before drain", i+1)
		}
		if reason != "" {
			t.Fatalf("unexpected rejection reason %q", reason)
		}
		leases = append(leases, lease)
	}
	if active := gate.beginDrain(); active != 2 {
		t.Fatalf("active at drain = %d, want 2", active)
	}
	if lease, _ := gate.admit(); lease != nil {
		t.Fatal("gate admitted a new session while draining")
	}

	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		leases[0].done()
		leases[1].done()
		gate.wait()
	}()
	done.Wait()
	if active := gate.activeCount(); active != 0 {
		t.Fatalf("active after drain = %d, want 0", active)
	}
}

func TestConnectionGateLimitsSessionsAndHandshakes(t *testing.T) {
	metrics := newServerMetrics()
	gate := newConnectionGate(admissionConfig{maxSessions: 2, maxHandshakes: 1}, nil, metrics)
	first, reason := gate.admit()
	if first == nil || reason != "" {
		t.Fatalf("first admission = (%v, %q)", first, reason)
	}
	if second, got := gate.admit(); second != nil || got != admissionReasonHandshakes {
		t.Fatalf("second admission = (%v, %q), want handshake rejection", second, got)
	}
	first.markEstablished()
	second, reason := gate.admit()
	if second == nil || reason != "" {
		t.Fatalf("second admission after handshake = (%v, %q)", second, reason)
	}
	if third, got := gate.admit(); third != nil || got != admissionReasonSessions {
		t.Fatalf("third admission = (%v, %q), want session rejection", third, got)
	}
	first.done()
	second.done()
}

func TestCanaryLimitTightensSessionLimit(t *testing.T) {
	config := admissionConfig{maxSessions: 100, canary: true, canaryMaxSessions: 3}
	if got := config.effectiveMaxSessions(); got != 3 {
		t.Fatalf("effective max sessions = %d, want 3", got)
	}
}
