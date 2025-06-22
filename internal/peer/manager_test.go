package peer

import (
	"net"
	"testing"
	"time"

	"github.com/mt/bittorrent-impl/internal/tracker"
)

// mockConn is a mock net.Conn for testing
type mockConn struct {
	addr string
}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error) { return len(b), nil }
func (m *mockConn) Close() error                      { return nil }
func (m *mockConn) LocalAddr() net.Addr               { return &mockAddr{m.addr} }
func (m *mockConn) RemoteAddr() net.Addr              { return &mockAddr{m.addr} }
func (m *mockConn) SetDeadline(t time.Time) error     { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// mockAddr is a mock net.Addr for testing
type mockAddr struct {
	addr string
}

func (m *mockAddr) Network() string { return "tcp" }
func (m *mockAddr) String() string  { return m.addr }

func TestNewManager(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	peerID := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	numPieces := 100
	
	manager := NewManager(infoHash, peerID, numPieces)
	
	if manager.infoHash != infoHash {
		t.Error("Info hash not set correctly")
	}
	if manager.peerID != peerID {
		t.Error("Peer ID not set correctly")
	}
	if manager.maxPeers != DefaultMaxPeers {
		t.Error("Max peers not set to default")
	}
	if manager.maxDownloadPeers != DefaultMaxDownloadPeers {
		t.Error("Max download peers not set to default")
	}
	
	// Check bitfield size
	expectedSize := (numPieces + 7) / 8
	if len(manager.bitfield) != expectedSize {
		t.Errorf("Bitfield size = %d, want %d", len(manager.bitfield), expectedSize)
	}
	
	// Check all pieces are initially missing
	for i := 0; i < numPieces; i++ {
		if manager.hasPieceIndex(i) {
			t.Errorf("Piece %d should not be available initially", i)
		}
	}
}

func TestManagerPeerCounts(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	if manager.GetActivePeerCount() != 0 {
		t.Error("Should have 0 active peers initially")
	}
	
	// Test limits
	manager.SetMaxPeers(5)
	manager.SetMaxDownloadPeers(3)
	
	if manager.maxPeers != 5 {
		t.Error("Max peers not updated")
	}
	if manager.maxDownloadPeers != 3 {
		t.Error("Max download peers not updated")
	}
}

func TestManagerBitfieldOperations(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 16)
	
	// Initially should have no pieces
	if manager.hasPieces() {
		t.Error("Should have no pieces initially")
	}
	
	// Set a piece
	manager.setPiece(0)
	if !manager.hasPieceIndex(0) {
		t.Error("Should have piece 0 after setting it")
	}
	if !manager.hasPieces() {
		t.Error("Should have pieces after setting one")
	}
	
	// Test out of bounds
	manager.setPiece(100) // Should not crash
	
	// Test getBitfield returns copy
	bf1 := manager.getBitfield()
	bf2 := manager.getBitfield()
	
	bf1[0] = 0xFF
	if bf2[0] == 0xFF {
		t.Error("Bitfield should be copied, not shared")
	}
}

func TestManagerStats(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	stats := manager.GetStats()
	if stats.TotalConnected != 0 {
		t.Error("Should have 0 total connected initially")
	}
	if stats.ActivePeers != 0 {
		t.Error("Should have 0 active peers initially")
	}
	
	// Update stats
	manager.stats.mu.Lock()
	manager.stats.TotalConnected = 5
	manager.stats.BytesDownloaded = 1024
	manager.stats.mu.Unlock()
	
	stats = manager.GetStats()
	if stats.TotalConnected != 5 {
		t.Error("Stats not updated correctly")
	}
	if stats.BytesDownloaded != 1024 {
		t.Error("Bytes downloaded not updated correctly")
	}
}

func TestManagerAddRemovePeer(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Create mock connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	// Test adding peer
	added := manager.addPeer(peer)
	if !added {
		t.Error("Should be able to add peer")
	}
	
	if manager.GetActivePeerCount() != 1 {
		t.Error("Should have 1 active peer")
	}
	
	// Test adding same peer again (should fail)
	added = manager.addPeer(peer)
	if added {
		t.Error("Should not be able to add same peer twice")
	}
	
	// Test removing peer
	manager.removePeer(peer)
	if manager.GetActivePeerCount() != 0 {
		t.Error("Should have 0 active peers after removal")
	}
}

func TestManagerPeerLimits(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	manager.SetMaxPeers(2)
	
	// Create peers with mock connections that have different addresses
	peer1 := NewPeer(&mockConn{addr: "127.0.0.1:6881"}, [20]byte{}, [20]byte{})
	peer2 := NewPeer(&mockConn{addr: "127.0.0.1:6882"}, [20]byte{}, [20]byte{})
	peer3 := NewPeer(&mockConn{addr: "127.0.0.1:6883"}, [20]byte{}, [20]byte{})
	
	// Should be able to add first two
	if !manager.addPeer(peer1) {
		t.Error("Should be able to add first peer")
	}
	if !manager.addPeer(peer2) {
		t.Error("Should be able to add second peer")
	}
	
	// Should not be able to add third (exceeds limit)
	if manager.addPeer(peer3) {
		t.Error("Should not be able to add third peer (exceeds limit)")
	}
	
	if manager.GetActivePeerCount() != 2 {
		t.Errorf("Should have 2 active peers, got %d", manager.GetActivePeerCount())
	}
}

func TestManagerHasPeer(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	addr := "127.0.0.1:6881"
	if manager.hasPeer(addr) {
		t.Error("Should not have peer initially")
	}
	
	// Create mock connection with specific address
	// Note: This is tricky to test with real addresses, so we'll test the logic
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	manager.addPeer(peer)
	
	// Test with actual peer address
	actualAddr := peer.Address().String()
	if !manager.hasPeer(actualAddr) {
		t.Error("Should have peer with its actual address")
	}
}

func TestManagerGetPeers(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Initially empty
	peers := manager.GetPeers()
	if len(peers) != 0 {
		t.Error("Should have no peers initially")
	}
	
	// Add a peer
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	manager.addPeer(peer)
	
	peers = manager.GetPeers()
	if len(peers) != 1 {
		t.Error("Should have 1 peer")
	}
	if peers[0] != peer {
		t.Error("Should return the same peer")
	}
}

func TestManagerDownloadUploadPeers(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Create peers with different addresses and states
	peer1 := NewPeer(&mockConn{addr: "127.0.0.1:6881"}, [20]byte{}, [20]byte{})
	peer2 := NewPeer(&mockConn{addr: "127.0.0.1:6882"}, [20]byte{}, [20]byte{})
	
	// Set up peer1 for downloading (not choked, we're interested)
	peer1.mu.Lock()
	peer1.state.PeerChoking = false
	peer1.state.AmInterested = true
	peer1.mu.Unlock()
	
	// Set up peer2 for uploading (we're not choking, peer is interested)
	peer2.mu.Lock()
	peer2.state.AmChoking = false
	peer2.state.PeerInterested = true
	peer2.mu.Unlock()
	
	manager.addPeer(peer1)
	manager.addPeer(peer2)
	
	// Test downloading peers
	downloadingPeers := manager.GetDownloadingPeers()
	if len(downloadingPeers) != 1 {
		t.Errorf("Should have 1 downloading peer, got %d", len(downloadingPeers))
	}
	if len(downloadingPeers) > 0 && downloadingPeers[0] != peer1 {
		t.Error("Wrong downloading peer")
	}
	
	// Test uploading peers
	uploadingPeers := manager.GetUploadingPeers()
	if len(uploadingPeers) != 1 {
		t.Errorf("Should have 1 uploading peer, got %d", len(uploadingPeers))
	}
	if len(uploadingPeers) > 0 && uploadingPeers[0] != peer2 {
		t.Error("Wrong uploading peer")
	}
}

func TestManagerFindPeersWithPiece(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Create peer with pieces
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	// Set up bitfield
	peer.mu.Lock()
	peer.bitfield = make([]byte, 2)
	peer.bitfield[0] = 0x80 // Has piece 0
	peer.mu.Unlock()
	
	manager.addPeer(peer)
	
	// Test finding peers with piece
	peers := manager.FindPeersWithPiece(0)
	if len(peers) != 1 {
		t.Errorf("Should find 1 peer with piece 0, got %d", len(peers))
	}
	if peers[0] != peer {
		t.Error("Wrong peer found")
	}
	
	// Test piece peer doesn't have
	peers = manager.FindPeersWithPiece(1)
	if len(peers) != 0 {
		t.Errorf("Should find 0 peers with piece 1, got %d", len(peers))
	}
}

func TestManagerBroadcastHave(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Create peer
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	manager.addPeer(peer)
	
	// Broadcast have
	manager.BroadcastHave(0)
	
	// Check that we marked the piece as ours
	if !manager.hasPieceIndex(0) {
		t.Error("Should have piece 0 after broadcasting have")
	}
	
	// Note: Testing actual message sending would require more complex mocking
}

func TestManagerRequestPieceFromPeers(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Test with no peers
	err := manager.RequestPieceFromPeers(0, 0, 16384)
	if err == nil {
		t.Error("Should fail when no peers available")
	}
	
	// Add peer without the piece
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	peer.mu.Lock()
	peer.bitfield = make([]byte, 2)
	peer.state.PeerChoking = false
	peer.state.AmInterested = true
	peer.mu.Unlock()
	
	manager.addPeer(peer)
	
	// Should still fail (peer doesn't have piece 0)
	err = manager.RequestPieceFromPeers(0, 0, 16384)
	if err == nil {
		t.Error("Should fail when no peer has the piece")
	}
	
	// Give peer the piece
	peer.SetPiece(0)
	
	// Should succeed now
	err = manager.RequestPieceFromPeers(0, 0, 16384)
	if err != nil {
		t.Errorf("Should succeed when peer has piece: %v", err)
	}
}

func TestManagerConnectToPeers(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Create tracker peers (these won't actually connect in tests)
	trackerPeers := []tracker.Peer{
		{IP: net.IPv4(127, 0, 0, 1), Port: 6881},
		{IP: net.IPv4(127, 0, 0, 1), Port: 6882},
	}
	
	// This will attempt connections but they'll fail in tests
	// We're mainly testing that it doesn't crash
	manager.ConnectToPeers(trackerPeers)
	
	// Give a brief moment for goroutines to start
	time.Sleep(10 * time.Millisecond)
	
	// Should still have 0 peers (connections fail in test environment)
	if manager.GetActivePeerCount() != 0 {
		t.Error("Connections should fail in test environment")
	}
}

func TestManagerGetPeerInfo(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Initially empty
	info := manager.GetPeerInfo()
	if len(info) != 0 {
		t.Error("Should have no peer info initially")
	}
	
	// Add a peer
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{1, 2, 3}, [20]byte{})
	peer.mu.Lock()
	peer.remotePeerID = [20]byte{4, 5, 6}
	peer.mu.Unlock()
	
	manager.addPeer(peer)
	
	info = manager.GetPeerInfo()
	if len(info) != 1 {
		t.Error("Should have 1 peer info")
	}
	
	peerInfo := info[0]
	if peerInfo.Address == "" {
		t.Error("Peer address should not be empty")
	}
	if !peerInfo.IsConnected {
		t.Error("Peer should be connected")
	}
}

func TestManagerStartStop(t *testing.T) {
	manager := NewManager([20]byte{}, [20]byte{}, 10)
	
	// Test start
	manager.Start()
	
	// Add a peer to test cleanup
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	manager.addPeer(peer)
	
	// Test stop
	manager.Stop()
	
	// Give a moment for cleanup
	time.Sleep(10 * time.Millisecond)
	
	// Manager should be stopped (context cancelled)
	select {
	case <-manager.ctx.Done():
		// Good, context is cancelled
	default:
		t.Error("Manager context should be cancelled after stop")
	}
}