package peer

import (
	"net"
	"testing"
	"time"
)

func TestNewPeerState(t *testing.T) {
	state := NewPeerState()
	
	// Check default values
	if !state.AmChoking {
		t.Error("Expected AmChoking to be true by default")
	}
	if state.AmInterested {
		t.Error("Expected AmInterested to be false by default")
	}
	if !state.PeerChoking {
		t.Error("Expected PeerChoking to be true by default")
	}
	if state.PeerInterested {
		t.Error("Expected PeerInterested to be false by default")
	}
}

func TestNewPeer(t *testing.T) {
	// Create mock connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	peerID := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	
	peer := NewPeer(client, infoHash, peerID)
	
	if peer.conn != client {
		t.Error("Connection not set correctly")
	}
	if peer.infoHash != infoHash {
		t.Error("Info hash not set correctly")
	}
	if peer.peerID != peerID {
		t.Error("Peer ID not set correctly")
	}
	if peer.state == nil {
		t.Error("State not initialized")
	}
	if peer.sendCh == nil {
		t.Error("Send channel not initialized")
	}
	if peer.receiveCh == nil {
		t.Error("Receive channel not initialized")
	}
}

func TestPeerGetState(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	state := peer.GetState()
	
	// Should get a copy, not the original
	state.AmChoking = false
	
	originalState := peer.GetState()
	if !originalState.AmChoking {
		t.Error("State was not copied, original was modified")
	}
}

func TestPeerBitfieldOperations(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	// Initially no bitfield
	if peer.HasPiece(0) {
		t.Error("Should not have any pieces initially")
	}
	
	// Set bitfield
	bitfield := make([]byte, 2) // 16 pieces
	bitfield[0] = 0x80          // First piece available
	
	peer.mu.Lock()
	peer.bitfield = bitfield
	peer.mu.Unlock()
	
	// Test HasPiece
	if !peer.HasPiece(0) {
		t.Error("Should have piece 0")
	}
	if peer.HasPiece(1) {
		t.Error("Should not have piece 1")
	}
	
	// Test SetPiece
	peer.SetPiece(1)
	if !peer.HasPiece(1) {
		t.Error("Should have piece 1 after setting it")
	}
	
	// Test GetBitfield returns copy
	bf := peer.GetBitfield()
	if bf == nil {
		t.Error("GetBitfield returned nil")
	}
	
	bf[0] = 0x00 // Modify copy
	if !peer.HasPiece(0) {
		t.Error("Original bitfield was modified")
	}
}

func TestPeerStateTransitions(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	// Test choke/unchoke
	err := peer.Unchoke()
	if err != nil {
		t.Errorf("Unchoke failed: %v", err)
	}
	
	state := peer.GetState()
	if state.AmChoking {
		t.Error("Should not be choking after unchoke")
	}
	
	err = peer.Choke()
	if err != nil {
		t.Errorf("Choke failed: %v", err)
	}
	
	state = peer.GetState()
	if !state.AmChoking {
		t.Error("Should be choking after choke")
	}
	
	// Test interested/not interested
	err = peer.Interested()
	if err != nil {
		t.Errorf("Interested failed: %v", err)
	}
	
	state = peer.GetState()
	if !state.AmInterested {
		t.Error("Should be interested after interested")
	}
	
	err = peer.NotInterested()
	if err != nil {
		t.Errorf("NotInterested failed: %v", err)
	}
	
	state = peer.GetState()
	if state.AmInterested {
		t.Error("Should not be interested after not interested")
	}
}

func TestPeerCanUploadDownload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	// Initially can't download (peer choking, we're not interested)
	if peer.CanDownload() {
		t.Error("Should not be able to download initially")
	}
	
	// Initially can't upload (we're choking, peer not interested)
	if peer.CanUpload() {
		t.Error("Should not be able to upload initially")
	}
	
	// Set up for download
	peer.mu.Lock()
	peer.state.PeerChoking = false
	peer.state.AmInterested = true
	peer.mu.Unlock()
	
	if !peer.CanDownload() {
		t.Error("Should be able to download now")
	}
	
	// Set up for upload
	peer.mu.Lock()
	peer.state.AmChoking = false
	peer.state.PeerInterested = true
	peer.mu.Unlock()
	
	if !peer.CanUpload() {
		t.Error("Should be able to upload now")
	}
}

func TestPeerMessageHandling(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	// Initialize bitfield for testing
	peer.mu.Lock()
	peer.bitfield = make([]byte, 2)
	peer.mu.Unlock()
	
	// Test handling choke message
	chokeMsg := NewChokeMessage()
	err := peer.handleMessage(chokeMsg)
	if err != nil {
		t.Errorf("Failed to handle choke message: %v", err)
	}
	
	state := peer.GetState()
	if !state.PeerChoking {
		t.Error("Peer should be choking after choke message")
	}
	
	// Test handling unchoke message
	unchokeMsg := NewUnchokeMessage()
	err = peer.handleMessage(unchokeMsg)
	if err != nil {
		t.Errorf("Failed to handle unchoke message: %v", err)
	}
	
	state = peer.GetState()
	if state.PeerChoking {
		t.Error("Peer should not be choking after unchoke message")
	}
	
	// Test handling have message
	haveMsg := NewHaveMessage(0)
	err = peer.handleMessage(haveMsg)
	if err != nil {
		t.Errorf("Failed to handle have message: %v", err)
	}
	
	if !peer.HasPiece(0) {
		t.Error("Should have piece 0 after have message")
	}
	
	// Test handling bitfield message
	bitfield := []byte{0xFF, 0x00} // First 8 pieces available
	bitfieldMsg := NewBitfieldMessage(bitfield)
	err = peer.handleMessage(bitfieldMsg)
	if err != nil {
		t.Errorf("Failed to handle bitfield message: %v", err)
	}
	
	for i := 0; i < 8; i++ {
		if !peer.HasPiece(i) {
			t.Errorf("Should have piece %d after bitfield message", i)
		}
	}
	
	for i := 8; i < 16; i++ {
		if peer.HasPiece(i) {
			t.Errorf("Should not have piece %d after bitfield message", i)
		}
	}
	
	// Test handling keep-alive (nil message)
	err = peer.handleMessage(nil)
	if err != nil {
		t.Errorf("Failed to handle keep-alive message: %v", err)
	}
}

func TestPeerRequestOperations(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	// Should fail when peer is choking
	err := peer.RequestPiece(0, 0, 16384)
	if err == nil {
		t.Error("Expected error when peer is choking")
	}
	
	// Unchoke peer
	peer.mu.Lock()
	peer.state.PeerChoking = false
	peer.mu.Unlock()
	
	// Should succeed now (message will be queued)
	err = peer.RequestPiece(0, 0, 16384)
	if err != nil {
		t.Errorf("Request should succeed when peer is not choking: %v", err)
	}
	
	// Test sending piece (should fail when we're choking)
	err = peer.SendPiece(0, 0, []byte("test"))
	if err == nil {
		t.Error("Expected error when we're choking")
	}
	
	// Unchoke ourselves
	peer.mu.Lock()
	peer.state.AmChoking = false
	peer.mu.Unlock()
	
	// Should succeed now
	err = peer.SendPiece(0, 0, []byte("test"))
	if err != nil {
		t.Errorf("SendPiece should succeed when not choking: %v", err)
	}
}

func TestPeerIsConnected(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	if !peer.IsConnected() {
		t.Error("Peer should be connected initially")
	}
	
	// Stop the peer
	peer.Stop()
	
	// Give it a moment to process
	time.Sleep(10 * time.Millisecond)
	
	if peer.IsConnected() {
		t.Error("Peer should not be connected after stop")
	}
}

func TestPeerString(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	str := peer.String()
	if str == "" {
		t.Error("String representation should not be empty")
	}
	
	// Should contain address information
	if peer.Address() == nil {
		t.Error("Address should not be nil")
	}
}

func TestPeerLastSeen(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	lastSeen := peer.LastSeen()
	if lastSeen.IsZero() {
		t.Error("LastSeen should be initialized")
	}
	
	// Update last seen
	time.Sleep(1 * time.Millisecond)
	peer.mu.Lock()
	peer.lastSeen = time.Now()
	peer.mu.Unlock()
	
	newLastSeen := peer.LastSeen()
	if !newLastSeen.After(lastSeen) {
		t.Error("LastSeen should be updated")
	}
}

func TestPeerControlMessages(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	peer := NewPeer(client, [20]byte{}, [20]byte{})
	
	tests := []struct {
		msg      *Message
		isControl bool
	}{
		{NewChokeMessage(), true},
		{NewUnchokeMessage(), true},
		{NewInterestedMessage(), true},
		{NewNotInterestedMessage(), true},
		{NewHaveMessage(0), true},
		{NewBitfieldMessage([]byte{0xFF}), true},
		{NewRequestMessage(0, 0, 16384), false},
		{NewPieceMessage(0, 0, []byte("data")), false},
		{NewCancelMessage(0, 0, 16384), false},
	}
	
	for _, tt := range tests {
		result := peer.isControlMessage(tt.msg)
		if result != tt.isControl {
			t.Errorf("isControlMessage(%s) = %v, want %v", tt.msg.String(), result, tt.isControl)
		}
	}
}