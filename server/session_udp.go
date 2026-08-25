package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	udpSessionPrefaceLen  = 17
	udpSessionMaxStreams  = 64
	udpSessionReorderFlag = 0x80
	udpSessionQueueSize   = 128
	udpSessionStripeSize  = 16
	udpSessionFlowletGap  = 2 * time.Millisecond
	udpSessionPacketSize  = 2048
)

type udpSessionHello struct {
	id              [16]byte
	streamID        byte
	supportsReorder bool
}

// parseUDPSessionPreface recognizes the preface that proxy_v2 Android clients
// have sent since before server-side aggregation existed: UUIDv4 (16 bytes)
// followed by one stream index. WireGuard packets cannot be 17 bytes, and the
// UUID/version checks keep legacy non-WireGuard traffic on its original path.
func parseUDPSessionPreface(packet []byte) (udpSessionHello, bool) {
	var hello udpSessionHello
	if len(packet) != udpSessionPrefaceLen || packet[6]>>4 != 4 || packet[8]&0xc0 != 0x80 {
		return hello, false
	}
	rawStreamID := packet[16]
	// Bit 7 is an optional capability flag. Bits 6..0 retain the original
	// stream identifier, so an old server still treats the whole 17-byte
	// preface as an ignorable non-WireGuard packet.
	hello.supportsReorder = rawStreamID&udpSessionReorderFlag != 0
	hello.streamID = rawStreamID &^ udpSessionReorderFlag
	if int(hello.streamID) >= udpSessionMaxStreams {
		return hello, false
	}
	copy(hello.id[:], packet[:16])
	return hello, true
}

type udpSessionKey struct {
	id          [16]byte
	connectAddr string
}

type udpSessionRegistry struct {
	mu       sync.Mutex
	sessions map[udpSessionKey]*aggregatedUDPSession
}

type udpRegistryStats struct {
	sessions int
	lanes    int
	queued   int
}

func newUDPSessionRegistry() *udpSessionRegistry {
	return &udpSessionRegistry{sessions: make(map[udpSessionKey]*aggregatedUDPSession)}
}

var sharedUDPSessions = newUDPSessionRegistry()

func (r *udpSessionRegistry) stats() udpRegistryStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	stats := udpRegistryStats{sessions: len(r.sessions)}
	for _, session := range r.sessions {
		session.mu.Lock()
		stats.lanes += len(session.lanes)
		for _, lane := range session.lanes {
			stats.queued += len(lane.out)
		}
		session.mu.Unlock()
	}
	return stats
}

func (r *udpSessionRegistry) getOrCreate(ctx context.Context, hello udpSessionHello, connectAddr string) (*aggregatedUDPSession, error) {
	key := udpSessionKey{id: hello.id, connectAddr: connectAddr}
	r.mu.Lock()
	defer r.mu.Unlock()
	if session := r.sessions[key]; session != nil && session.ctx.Err() == nil {
		return session, nil
	}

	backend, err := net.Dial("udp", connectAddr)
	if err != nil {
		serverStats.recordBackendError("dial")
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &aggregatedUDPSession{
		ctx:      sessionCtx,
		cancel:   cancel,
		key:      key,
		registry: r,
		backend:  backend,
		lanes:    make(map[byte]*udpSessionLane),
	}
	r.sessions[key] = session
	go session.readBackend()
	debugf("[udp-session %s] created", session.shortID())
	return session, nil
}

func (r *udpSessionRegistry) remove(key udpSessionKey, wanted *aggregatedUDPSession) {
	r.mu.Lock()
	if r.sessions[key] == wanted {
		delete(r.sessions, key)
	}
	r.mu.Unlock()
}

type udpSessionPacket struct {
	buf []byte
}

var udpSessionPacketPool = sync.Pool{
	New: func() any {
		return &udpSessionPacket{buf: make([]byte, udpSessionPacketSize)}
	},
}

func acquireUDPSessionPacket(payload []byte) *udpSessionPacket {
	packet, ok := udpSessionPacketPool.Get().(*udpSessionPacket)
	if !ok {
		packet = &udpSessionPacket{buf: make([]byte, udpSessionPacketSize)}
	}
	if cap(packet.buf) < len(payload) {
		packet.buf = make([]byte, len(payload))
	}
	packet.buf = packet.buf[:len(payload)]
	copy(packet.buf, payload)
	return packet
}

func releaseUDPSessionPacket(packet *udpSessionPacket) {
	if packet == nil {
		return
	}
	packet.buf = packet.buf[:cap(packet.buf)]
	udpSessionPacketPool.Put(packet)
}

type udpSessionLane struct {
	streamID        byte
	supportsReorder bool
	conn            net.Conn
	out             chan *udpSessionPacket
	ctx             context.Context
	cancel          context.CancelFunc
	writeMu         sync.Mutex
	closed          atomic.Bool
}

func (l *udpSessionLane) write(packet []byte) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if err := l.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	_, err := l.conn.Write(packet)
	return err
}

func (l *udpSessionLane) stop() {
	if !l.closed.CompareAndSwap(false, true) {
		return
	}
	l.cancel()
	if err := l.conn.SetDeadline(time.Now()); err != nil {
		debugf("failed to unblock UDP session lane %d: %v", l.streamID, err)
	}
}

type udpLaneSelector struct {
	current    byte
	hasCurrent bool
	remaining  int
	cursor     byte
	lastPacket time.Time
}

func (s *udpLaneSelector) choose(lanes [udpSessionMaxStreams]*udpSessionLane, now time.Time) *udpSessionLane {
	canStripe := true
	activeLanes := 0
	for _, lane := range lanes {
		if lane == nil || lane.closed.Load() {
			continue
		}
		activeLanes++
		canStripe = canStripe && lane.supportsReorder
	}
	if activeLanes == 0 {
		s.hasCurrent = false
		return nil
	}

	// Existing Android clients already send the session preface, but do not
	// have a reorder buffer. Aggregate their uplink into one backend socket,
	// while pinning downlink to one lane so the server cannot introduce a new
	// source of packet reordering. New clients explicitly advertise support.
	if !canStripe {
		if s.hasCurrent {
			lane := lanes[s.current]
			if lane != nil && !lane.closed.Load() {
				if len(lane.out) < cap(lane.out) {
					return lane
				}
				// Dropping under sustained backpressure is preferable to sending a
				// later packet down another TCP lane ahead of queued packets.
				return nil
			}
		}
		for offset := 0; offset < len(lanes); offset++ {
			index := byte((int(s.cursor) + offset) % len(lanes))
			lane := lanes[index]
			if lane == nil || lane.closed.Load() || len(lane.out) >= cap(lane.out) {
				continue
			}
			s.cursor = byte((int(index) + 1) % len(lanes))
			s.current = index
			s.hasCurrent = true
			s.remaining = 0
			return lane
		}
		s.hasCurrent = false
		return nil
	}

	if !s.lastPacket.IsZero() && now.Sub(s.lastPacket) > udpSessionFlowletGap {
		s.hasCurrent = false
		s.remaining = 0
	}
	s.lastPacket = now

	if s.hasCurrent && s.remaining > 0 {
		lane := lanes[s.current]
		if lane != nil && !lane.closed.Load() && len(lane.out) < udpSessionStripeSize {
			s.remaining--
			return lane
		}
	}

	minimumDepth := udpSessionQueueSize + 1
	for _, lane := range lanes {
		if lane != nil && !lane.closed.Load() && len(lane.out) < minimumDepth {
			minimumDepth = len(lane.out)
		}
	}
	if minimumDepth >= udpSessionQueueSize {
		s.hasCurrent = false
		return nil
	}
	for offset := 0; offset < len(lanes); offset++ {
		index := byte((int(s.cursor) + offset) % len(lanes))
		lane := lanes[index]
		if lane == nil || lane.closed.Load() || len(lane.out) != minimumDepth {
			continue
		}
		s.cursor = byte((int(index) + 1) % len(lanes))
		s.current = index
		s.hasCurrent = true
		s.remaining = udpSessionStripeSize - 1
		return lane
	}
	s.hasCurrent = false
	return nil
}

type aggregatedUDPSession struct {
	ctx      context.Context
	cancel   context.CancelFunc
	key      udpSessionKey
	registry *udpSessionRegistry
	backend  net.Conn

	mu             sync.Mutex
	lanes          map[byte]*udpSessionLane
	selector       udpLaneSelector
	backendWriteMu sync.Mutex
	closeOnce      sync.Once
	downlinkDrops  atomic.Uint64
}

func (s *aggregatedUDPSession) shortID() string {
	return hex.EncodeToString(s.key.id[:4])
}

func (s *aggregatedUDPSession) addLane(hello udpSessionHello, conn net.Conn) (*udpSessionLane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}
	streamID := hello.streamID
	if previous := s.lanes[streamID]; previous != nil {
		previous.stop()
	}
	laneCtx, cancel := context.WithCancel(s.ctx)
	lane := &udpSessionLane{
		streamID:        streamID,
		supportsReorder: hello.supportsReorder,
		conn:            conn,
		out:             make(chan *udpSessionPacket, udpSessionQueueSize),
		ctx:             laneCtx,
		cancel:          cancel,
	}
	s.lanes[streamID] = lane
	go s.writeLane(lane)
	debugf("[udp-session %s] stream %d attached (lanes=%d)", s.shortID(), streamID, len(s.lanes))
	return lane, nil
}

func (s *aggregatedUDPSession) removeLane(lane *udpSessionLane) {
	lane.stop()
	s.mu.Lock()
	if s.lanes[lane.streamID] == lane {
		delete(s.lanes, lane.streamID)
	}
	left := len(s.lanes)
	s.mu.Unlock()
	debugf("[udp-session %s] stream %d detached (lanes=%d)", s.shortID(), lane.streamID, left)
	if left == 0 {
		s.close()
	}
}

func (s *aggregatedUDPSession) close() {
	s.closeOnce.Do(func() {
		s.cancel()
		if err := s.backend.SetDeadline(time.Now()); err != nil {
			debugf("[udp-session %s] failed to unblock backend: %v", s.shortID(), err)
		}
		_ = s.backend.Close()
		s.mu.Lock()
		for _, lane := range s.lanes {
			lane.stop()
		}
		s.lanes = make(map[byte]*udpSessionLane)
		s.mu.Unlock()
		s.registry.remove(s.key, s)
		debugf("[udp-session %s] closed (downlink-drops=%d)", s.shortID(), s.downlinkDrops.Load())
	})
}

func (s *aggregatedUDPSession) writeBackend(packet []byte) error {
	s.backendWriteMu.Lock()
	defer s.backendWriteMu.Unlock()
	if err := s.backend.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	_, err := s.backend.Write(packet)
	return err
}

func (s *aggregatedUDPSession) snapshotLanesLocked() [udpSessionMaxStreams]*udpSessionLane {
	var lanes [udpSessionMaxStreams]*udpSessionLane
	for streamID, lane := range s.lanes {
		if int(streamID) < len(lanes) {
			lanes[streamID] = lane
		}
	}
	return lanes
}

func (s *aggregatedUDPSession) enqueueDownlink(payload []byte, now time.Time) bool {
	s.mu.Lock()
	lanes := s.snapshotLanesLocked()
	lane := s.selector.choose(lanes, now)
	if lane == nil {
		s.mu.Unlock()
		s.downlinkDrops.Add(1)
		return false
	}
	packet := acquireUDPSessionPacket(payload)
	select {
	case lane.out <- packet:
		s.mu.Unlock()
		return true
	default:
		s.mu.Unlock()
		releaseUDPSessionPacket(packet)
		s.downlinkDrops.Add(1)
		return false
	}
}

func (s *aggregatedUDPSession) readBackend() {
	defer s.close()
	buf := make([]byte, udpSessionPacketSize)
	for {
		if err := s.backend.SetReadDeadline(time.Now().Add(time.Minute * 30)); err != nil {
			return
		}
		n, err := s.backend.Read(buf)
		if err != nil {
			if s.ctx.Err() == nil {
				serverStats.recordBackendError("read")
				log.Printf("[udp-session %s] backend read error: %v", s.shortID(), err)
			}
			return
		}
		serverStats.packetsFromBackend.Add(1)
		if !s.enqueueDownlink(buf[:n], time.Now()) {
			serverStats.udpDownlinkDrops.Add(1)
		}
	}
}

func (s *aggregatedUDPSession) writeLane(lane *udpSessionLane) {
	for {
		select {
		case <-lane.ctx.Done():
			for {
				select {
				case packet := <-lane.out:
					releaseUDPSessionPacket(packet)
				default:
					return
				}
			}
		case packet := <-lane.out:
			err := lane.write(packet.buf)
			releaseUDPSessionPacket(packet)
			if err != nil {
				if lane.ctx.Err() == nil {
					debugf("[udp-session %s] stream %d write error: %v", s.shortID(), lane.streamID, err)
				}
				lane.stop()
				return
			}
		}
	}
}

func handleAggregatedUDPConnection(ctx context.Context, conn net.Conn, connectAddr string, hello udpSessionHello) {
	session, err := sharedUDPSessions.getOrCreate(ctx, hello, connectAddr)
	if err != nil {
		log.Printf("session backend dial error: %v", err)
		return
	}
	lane, err := session.addLane(hello, conn)
	if err != nil {
		log.Printf("session stream attach error: %v", err)
		return
	}
	defer session.removeLane(lane)

	buf := make([]byte, udpSessionPacketSize)
	for {
		select {
		case <-lane.ctx.Done():
			return
		default:
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Minute * 30)); err != nil {
			return
		}
		n, readErr := conn.Read(buf)
		if readErr != nil {
			if lane.ctx.Err() == nil && ctx.Err() == nil {
				debugf("[udp-session %s] stream %d read error: %v", session.shortID(), hello.streamID, readErr)
			}
			return
		}
		packet := buf[:n]
		if _, probe := parseProbePacket(packet); probe {
			if writeErr := lane.write(packet); writeErr != nil {
				return
			}
			continue
		}
		if writeErr := session.writeBackend(packet); writeErr != nil {
			if session.ctx.Err() == nil {
				serverStats.recordBackendError("write")
				log.Printf("[udp-session %s] backend write error: %v", session.shortID(), writeErr)
			}
			return
		}
		serverStats.packetsToBackend.Add(1)
	}
}

func (h udpSessionHello) String() string {
	return fmt.Sprintf("%s/%d", hex.EncodeToString(h.id[:4]), h.streamID)
}
