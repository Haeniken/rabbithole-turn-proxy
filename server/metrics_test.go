package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObservabilityReadinessAndMetrics(t *testing.T) {
	metrics := newServerMetrics()
	resources := newResourceMonitor()
	gate := newConnectionGate(admissionConfig{maxSessions: 1}, resources, metrics)
	mux := newObservabilityMux("test", "canary", metrics, gate, resources)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "ready" {
		t.Fatalf("initial readiness = %d %q", response.Code, response.Body.String())
	}

	lease, reason := gate.admit()
	if lease == nil || reason != "" {
		t.Fatalf("admission = (%v, %q)", lease, reason)
	}
	request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.TrimSpace(response.Body.String()) != admissionReasonSessions {
		t.Fatalf("limited readiness = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	body := response.Body.String()
	for _, expected := range []string{
		`rabbithole_turn_info{instance="test",role="canary"} 1`,
		"rabbithole_turn_sessions_active 1",
		"rabbithole_turn_process_goroutines",
		"rabbithole_turn_process_open_fds",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics missing %q", expected)
		}
	}
	lease.done()
}
