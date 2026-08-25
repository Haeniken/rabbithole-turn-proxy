package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type atomicBool struct{ value atomic.Uint32 }

func (b *atomicBool) Store(value bool) {
	if value {
		b.value.Store(1)
	} else {
		b.value.Store(0)
	}
}

func (b *atomicBool) Load() bool { return b.value.Load() != 0 }

type serverMetrics struct {
	sessionsAdmitted   atomic.Uint64
	rejectedDrain      atomic.Uint64
	rejectedSessions   atomic.Uint64
	rejectedHandshakes atomic.Uint64
	rejectedGoroutines atomic.Uint64
	rejectedFDs        atomic.Uint64
	handshakeFailures  atomic.Uint64
	backendDialErrors  atomic.Uint64
	backendReadErrors  atomic.Uint64
	backendWriteErrors atomic.Uint64
	udpDownlinkDrops   atomic.Uint64
	packetsToBackend   atomic.Uint64
	packetsFromBackend atomic.Uint64
	drainStarted       atomic.Uint64
	drainCompleted     atomic.Uint64
	drainForced        atomic.Uint64
	draining           atomicBool
}

func newServerMetrics() *serverMetrics { return &serverMetrics{} }

var serverStats = newServerMetrics()

func (m *serverMetrics) recordAdmissionRejected(reason string) {
	switch reason {
	case admissionReasonDrain:
		m.rejectedDrain.Add(1)
	case admissionReasonSessions:
		m.rejectedSessions.Add(1)
	case admissionReasonHandshakes:
		m.rejectedHandshakes.Add(1)
	case admissionReasonGoroutines:
		m.rejectedGoroutines.Add(1)
	case admissionReasonFDs:
		m.rejectedFDs.Add(1)
	}
}

func (m *serverMetrics) recordBackendError(operation string) {
	switch operation {
	case "dial":
		m.backendDialErrors.Add(1)
	case "read":
		m.backendReadErrors.Add(1)
	case "write":
		m.backendWriteErrors.Add(1)
	}
}

type resourceMonitor struct {
	openFDs atomic.Int64
	maxFDs  atomic.Int64
}

func newResourceMonitor() *resourceMonitor {
	r := &resourceMonitor{}
	r.openFDs.Store(-1)
	r.maxFDs.Store(-1)
	r.sample()
	return r
}

func (r *resourceMonitor) run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sample()
		}
	}
}

func (r *resourceMonitor) sample() {
	entries, err := os.ReadDir("/proc/self/fd")
	if err == nil {
		r.openFDs.Store(int64(len(entries)))
	}
	if limit := readMaxOpenFiles(); limit > 0 {
		r.maxFDs.Store(limit)
	}
}

func (r *resourceMonitor) fdCounts() (int64, int64) {
	return r.openFDs.Load(), r.maxFDs.Load()
}

func readMaxOpenFiles() int64 {
	contents, err := os.ReadFile("/proc/self/limits")
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.Join(fields[:3], " ") == "Max open files" {
			if fields[3] == "unlimited" {
				return -1
			}
			value, parseErr := strconv.ParseInt(fields[3], 10, 64)
			if parseErr == nil {
				return value
			}
		}
	}
	return -1
}

func startObservabilityServer(
	ctx context.Context,
	listenAddr string,
	instance string,
	role string,
	metrics *serverMetrics,
	gate *connectionGate,
	resources *resourceMonitor,
) error {
	if strings.TrimSpace(listenAddr) == "" {
		return nil
	}
	mux := newObservabilityMux(instance, role, metrics, gate, resources)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			log.Printf("Observability server failed: %v", serveErr)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Printf("Observability server shutdown failed: %v", shutdownErr)
		}
	}()
	return nil
}

func newObservabilityMux(
	instance string,
	role string,
	metrics *serverMetrics,
	gate *connectionGate,
	resources *resourceMonitor,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, writeErr := w.Write([]byte("ok\n")); writeErr != nil {
			debugf("health response write failed: %v", writeErr)
		}
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		ready, reason := gate.readiness()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = fmt.Fprintf(w, "%s\n", reason)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writePrometheusMetrics(w, instance, role, metrics, gate, resources)
	})
	return mux
}

func metricHelp(w http.ResponseWriter, name, kind, help string) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func metricValue(w http.ResponseWriter, name string, value any) {
	_, _ = fmt.Fprintf(w, "%s %v\n", name, value)
}

func metricLabelValue(w http.ResponseWriter, name, label, value string, number uint64) {
	_, _ = fmt.Fprintf(w, "%s{%s=%q} %d\n", name, label, value, number)
}

func writePrometheusMetrics(
	w http.ResponseWriter,
	instance string,
	role string,
	m *serverMetrics,
	gate *connectionGate,
	resources *resourceMonitor,
) {
	_, _ = fmt.Fprintf(w, "# HELP rabbithole_turn_info Static instance information.\n# TYPE rabbithole_turn_info gauge\nrabbithole_turn_info{instance=%q,role=%q} 1\n", instance, role)
	metricHelp(w, "rabbithole_turn_sessions_active", "gauge", "Admitted DTLS connections, including handshakes.")
	metricValue(w, "rabbithole_turn_sessions_active", gate.activeCount())
	metricHelp(w, "rabbithole_turn_sessions_established", "gauge", "Established DTLS sessions.")
	metricValue(w, "rabbithole_turn_sessions_established", gate.establishedCount())
	metricHelp(w, "rabbithole_turn_handshakes_active", "gauge", "DTLS handshakes currently in progress.")
	metricValue(w, "rabbithole_turn_handshakes_active", gate.handshakeCount())
	metricHelp(w, "rabbithole_turn_sessions_admitted_total", "counter", "Sessions admitted since process start.")
	metricValue(w, "rabbithole_turn_sessions_admitted_total", m.sessionsAdmitted.Load())
	metricHelp(w, "rabbithole_turn_sessions_rejected_total", "counter", "Sessions rejected before handler startup.")
	metricLabelValue(w, "rabbithole_turn_sessions_rejected_total", "reason", admissionReasonDrain, m.rejectedDrain.Load())
	metricLabelValue(w, "rabbithole_turn_sessions_rejected_total", "reason", admissionReasonSessions, m.rejectedSessions.Load())
	metricLabelValue(w, "rabbithole_turn_sessions_rejected_total", "reason", admissionReasonHandshakes, m.rejectedHandshakes.Load())
	metricLabelValue(w, "rabbithole_turn_sessions_rejected_total", "reason", admissionReasonGoroutines, m.rejectedGoroutines.Load())
	metricLabelValue(w, "rabbithole_turn_sessions_rejected_total", "reason", admissionReasonFDs, m.rejectedFDs.Load())
	metricHelp(w, "rabbithole_turn_handshake_failures_total", "counter", "Failed DTLS handshakes.")
	metricValue(w, "rabbithole_turn_handshake_failures_total", m.handshakeFailures.Load())
	metricHelp(w, "rabbithole_turn_backend_errors_total", "counter", "Backend I/O errors by operation.")
	metricLabelValue(w, "rabbithole_turn_backend_errors_total", "operation", "dial", m.backendDialErrors.Load())
	metricLabelValue(w, "rabbithole_turn_backend_errors_total", "operation", "read", m.backendReadErrors.Load())
	metricLabelValue(w, "rabbithole_turn_backend_errors_total", "operation", "write", m.backendWriteErrors.Load())
	metricHelp(w, "rabbithole_turn_udp_downlink_queue_drops_total", "counter", "Downlink packets dropped because all compatible client lane queues were full.")
	metricValue(w, "rabbithole_turn_udp_downlink_queue_drops_total", m.udpDownlinkDrops.Load())
	metricHelp(w, "rabbithole_turn_packets_total", "counter", "Packets forwarded to or from the backend.")
	metricLabelValue(w, "rabbithole_turn_packets_total", "direction", "to_backend", m.packetsToBackend.Load())
	metricLabelValue(w, "rabbithole_turn_packets_total", "direction", "from_backend", m.packetsFromBackend.Load())
	udpStats := sharedUDPSessions.stats()
	metricHelp(w, "rabbithole_turn_udp_sessions_active", "gauge", "Aggregated Android UDP sessions.")
	metricValue(w, "rabbithole_turn_udp_sessions_active", udpStats.sessions)
	metricHelp(w, "rabbithole_turn_udp_lanes_active", "gauge", "Active lanes across aggregated UDP sessions.")
	metricValue(w, "rabbithole_turn_udp_lanes_active", udpStats.lanes)
	metricHelp(w, "rabbithole_turn_udp_queue_packets", "gauge", "Packets waiting in aggregated UDP downlink queues.")
	metricValue(w, "rabbithole_turn_udp_queue_packets", udpStats.queued)
	bondStats := globalBondRegistry.stats()
	metricHelp(w, "rabbithole_turn_bond_sessions_active", "gauge", "Active bonded VLESS sessions.")
	metricValue(w, "rabbithole_turn_bond_sessions_active", bondStats.sessions)
	metricHelp(w, "rabbithole_turn_bond_lanes_active", "gauge", "Active bonded VLESS lanes.")
	metricValue(w, "rabbithole_turn_bond_lanes_active", bondStats.lanes)
	metricHelp(w, "rabbithole_turn_bond_queue_frames", "gauge", "Frames waiting in bonded VLESS receive queues.")
	metricValue(w, "rabbithole_turn_bond_queue_frames", bondStats.queued)
	metricHelp(w, "rabbithole_turn_draining", "gauge", "Whether graceful drain is refusing new sessions.")
	if m.draining.Load() {
		metricValue(w, "rabbithole_turn_draining", 1)
	} else {
		metricValue(w, "rabbithole_turn_draining", 0)
	}
	metricHelp(w, "rabbithole_turn_drain_total", "counter", "Drain lifecycle events.")
	metricLabelValue(w, "rabbithole_turn_drain_total", "result", "started", m.drainStarted.Load())
	metricLabelValue(w, "rabbithole_turn_drain_total", "result", "completed", m.drainCompleted.Load())
	metricLabelValue(w, "rabbithole_turn_drain_total", "result", "forced", m.drainForced.Load())
	metricHelp(w, "rabbithole_turn_process_goroutines", "gauge", "Current Go goroutine count.")
	metricValue(w, "rabbithole_turn_process_goroutines", runtime.NumGoroutine())
	openFDs, maxFDs := resources.fdCounts()
	metricHelp(w, "rabbithole_turn_process_open_fds", "gauge", "Open file descriptors, or -1 when unavailable.")
	metricValue(w, "rabbithole_turn_process_open_fds", openFDs)
	metricHelp(w, "rabbithole_turn_process_max_fds", "gauge", "Process file descriptor limit, or -1 when unavailable.")
	metricValue(w, "rabbithole_turn_process_max_fds", maxFDs)
}
