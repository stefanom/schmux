package dashboard

import (
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestGetRotationLock(t *testing.T) {
	t.Run("returns same mutex for same sessionID", func(t *testing.T) {
		s := &Server{
			rotationLocks: make(map[string]*sync.Mutex),
		}
		lock1 := s.getRotationLock("session-123")
		lock2 := s.getRotationLock("session-123")

		if lock1 != lock2 {
			t.Errorf("getRotationLock should return same mutex for same sessionID")
		}
	})

	t.Run("returns different mutexes for different sessionIDs", func(t *testing.T) {
		s := &Server{
			rotationLocks: make(map[string]*sync.Mutex),
		}
		lock1 := s.getRotationLock("session-123")
		lock2 := s.getRotationLock("session-456")

		if lock1 == lock2 {
			t.Errorf("getRotationLock should return different mutexes for different sessionIDs")
		}
	})

	t.Run("concurrent calls are safe", func(t *testing.T) {
		s := &Server{
			rotationLocks: make(map[string]*sync.Mutex),
		}
		sessionID := "session-concurrent"
		var wg sync.WaitGroup
		calls := 10

		for i := 0; i < calls; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.getRotationLock(sessionID)
			}()
		}
		wg.Wait()

		// Should only have one entry in the map
		if len(s.rotationLocks) != 1 {
			t.Errorf("expected 1 entry, got %d", len(s.rotationLocks))
		}
	})
}

func TestRegisterUnregisterWebSocket(t *testing.T) {
	t.Run("register adds connection to list", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*websocket.Conn),
		}
		conn := &websocket.Conn{}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn)

		conns := s.wsConns[sessionID]
		if len(conns) != 1 {
			t.Errorf("expected 1 connection, got %d", len(conns))
		}
		if conns[0] != conn {
			t.Errorf("stored connection is not the same as the one registered")
		}
	})

	t.Run("register multiple connections for same session", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*websocket.Conn),
		}
		conn1 := &websocket.Conn{}
		conn2 := &websocket.Conn{}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn1)
		s.RegisterWebSocket(sessionID, conn2)

		conns := s.wsConns[sessionID]
		if len(conns) != 2 {
			t.Errorf("expected 2 connections, got %d", len(conns))
		}
	})

	t.Run("unregister removes specific connection", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*websocket.Conn),
		}
		conn1 := &websocket.Conn{}
		conn2 := &websocket.Conn{}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn1)
		s.RegisterWebSocket(sessionID, conn2)
		s.UnregisterWebSocket(sessionID, conn1)

		conns := s.wsConns[sessionID]
		if len(conns) != 1 {
			t.Errorf("expected 1 connection after unregister, got %d", len(conns))
		}
		if conns[0] != conn2 {
			t.Errorf("remaining connection is not the expected one")
		}
	})

	t.Run("unregister last connection deletes entry", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*websocket.Conn),
		}
		conn := &websocket.Conn{}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn)
		s.UnregisterWebSocket(sessionID, conn)

		if _, exists := s.wsConns[sessionID]; exists {
			t.Errorf("entry should be deleted when last connection is unregistered")
		}
	})
}

func TestBroadcastToSession(t *testing.T) {
	// Note: BroadcastToSession tries to write to WebSocket connections,
	// which requires complex mocking. These tests verify registry behavior only.

	t.Run("clears entry even when connections exist", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*websocket.Conn),
		}
		// Can't use a real websocket.Conn as it has internal state
		// Just verify the registry is cleared
		s.wsConns["session-123"] = []*websocket.Conn{{}}

		// This will panic on WriteMessage, but entry should be cleared first
		func() {
			defer func() {
				// Expected to panic due to nil conn internals
				_ = recover()
			}()
			s.BroadcastToSession("session-123", "test", "message")
		}()

		// Entry should be cleared after broadcast attempt
		if _, exists := s.wsConns["session-123"]; exists {
			t.Errorf("entry should be cleared after broadcast attempt")
		}
	})

	t.Run("returns 0 for session with no connections", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*websocket.Conn),
		}

		count := s.BroadcastToSession("nonexistent", "test", "message")
		if count != 0 {
			t.Errorf("expected 0 for nonexistent session, got %d", count)
		}
	})
}
