package tracker

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"github.com/mt/bittorrent-impl/internal/bencode"
)

func TestParseCompactPeers(t *testing.T) {
	// Create test data: 2 peers
	data := make([]byte, 12)
	
	// First peer: 192.168.1.1:6881
	copy(data[0:4], net.IPv4(192, 168, 1, 1).To4())
	binary.BigEndian.PutUint16(data[4:6], 6881)
	
	// Second peer: 10.0.0.2:6882
	copy(data[6:10], net.IPv4(10, 0, 0, 2).To4())
	binary.BigEndian.PutUint16(data[10:12], 6882)
	
	peers := parseCompactPeers(data)
	
	if len(peers) != 2 {
		t.Fatalf("Expected 2 peers, got %d", len(peers))
	}
	
	// Check first peer
	if !peers[0].IP.Equal(net.IPv4(192, 168, 1, 1)) {
		t.Errorf("First peer IP = %v, want 192.168.1.1", peers[0].IP)
	}
	if peers[0].Port != 6881 {
		t.Errorf("First peer port = %d, want 6881", peers[0].Port)
	}
	
	// Check second peer
	if !peers[1].IP.Equal(net.IPv4(10, 0, 0, 2)) {
		t.Errorf("Second peer IP = %v, want 10.0.0.2", peers[1].IP)
	}
	if peers[1].Port != 6882 {
		t.Errorf("Second peer port = %d, want 6882", peers[1].Port)
	}
}

func TestParseDictPeers(t *testing.T) {
	peersData := []interface{}{
		map[string]interface{}{
			"peer id": "12345678901234567890",
			"ip":      "192.168.1.1",
			"port":    int64(6881),
		},
		map[string]interface{}{
			"ip":   "10.0.0.2",
			"port": int64(6882),
		},
		map[string]interface{}{
			"ip": "invalid-ip", // Should be skipped
			"port": int64(6883),
		},
	}
	
	peers := parseDictPeers(peersData)
	
	if len(peers) != 2 {
		t.Fatalf("Expected 2 valid peers, got %d", len(peers))
	}
	
	// Check first peer
	if !peers[0].IP.Equal(net.IPv4(192, 168, 1, 1)) {
		t.Errorf("First peer IP = %v, want 192.168.1.1", peers[0].IP)
	}
	if peers[0].Port != 6881 {
		t.Errorf("First peer port = %d, want 6881", peers[0].Port)
	}
	if string(peers[0].ID) != "12345678901234567890" {
		t.Errorf("First peer ID = %s, want 12345678901234567890", peers[0].ID)
	}
	
	// Check second peer
	if !peers[1].IP.Equal(net.IPv4(10, 0, 0, 2)) {
		t.Errorf("Second peer IP = %v, want 10.0.0.2", peers[1].IP)
	}
	if peers[1].Port != 6882 {
		t.Errorf("Second peer port = %d, want 6882", peers[1].Port)
	}
}

func TestParseResponse(t *testing.T) {
	client := NewClient()
	
	// Test successful response with compact peers
	respData := map[string]interface{}{
		"interval":   int64(1800),
		"complete":   int64(10),
		"incomplete": int64(5),
		"peers":      string([]byte{192, 168, 1, 1, 0x1A, 0xE1}), // 192.168.1.1:6881
	}
	
	encoded, err := bencode.Encode(respData)
	if err != nil {
		t.Fatalf("Failed to encode test response: %v", err)
	}
	
	resp, err := client.parseResponse(encoded)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	
	if resp.Interval != 1800 {
		t.Errorf("Interval = %d, want 1800", resp.Interval)
	}
	if resp.Complete != 10 {
		t.Errorf("Complete = %d, want 10", resp.Complete)
	}
	if resp.Incomplete != 5 {
		t.Errorf("Incomplete = %d, want 5", resp.Incomplete)
	}
	if len(resp.Peers) != 1 {
		t.Fatalf("Expected 1 peer, got %d", len(resp.Peers))
	}
	if !resp.Peers[0].IP.Equal(net.IPv4(192, 168, 1, 1)) {
		t.Errorf("Peer IP = %v, want 192.168.1.1", resp.Peers[0].IP)
	}
}

func TestParseResponseWithError(t *testing.T) {
	client := NewClient()
	
	// Test error response
	respData := map[string]interface{}{
		"failure reason": "Invalid info_hash",
	}
	
	encoded, err := bencode.Encode(respData)
	if err != nil {
		t.Fatalf("Failed to encode test response: %v", err)
	}
	
	_, err = client.parseResponse(encoded)
	if err == nil {
		t.Error("Expected error for failure response")
	}
	if err.Error() != "tracker error: Invalid info_hash" {
		t.Errorf("Error = %v, want 'tracker error: Invalid info_hash'", err)
	}
}

func TestGeneratePeerID(t *testing.T) {
	id1 := GeneratePeerID()
	id2 := GeneratePeerID()
	
	// Check length
	if len(id1) != 20 {
		t.Errorf("Peer ID length = %d, want 20", len(id1))
	}
	
	// Check prefix
	prefix := string(id1[:8])
	if prefix != "-SB0100-" {
		t.Errorf("Peer ID prefix = %s, want -SB0100-", prefix)
	}
	
	// Check that IDs are different
	if bytes.Equal(id1[:], id2[:]) {
		t.Error("Generated peer IDs should be different")
	}
}

func TestCompactPeersToBytes(t *testing.T) {
	peers := []Peer{
		{IP: net.IPv4(192, 168, 1, 1), Port: 6881},
		{IP: net.IPv4(10, 0, 0, 2), Port: 6882},
	}
	
	data := CompactPeersToBytes(peers)
	
	if len(data) != 12 {
		t.Fatalf("Expected 12 bytes, got %d", len(data))
	}
	
	// Check first peer
	if !bytes.Equal(data[0:4], []byte{192, 168, 1, 1}) {
		t.Errorf("First peer IP bytes = %v, want [192 168 1 1]", data[0:4])
	}
	port1 := binary.BigEndian.Uint16(data[4:6])
	if port1 != 6881 {
		t.Errorf("First peer port = %d, want 6881", port1)
	}
	
	// Check second peer
	if !bytes.Equal(data[6:10], []byte{10, 0, 0, 2}) {
		t.Errorf("Second peer IP bytes = %v, want [10 0 0 2]", data[6:10])
	}
	port2 := binary.BigEndian.Uint16(data[10:12])
	if port2 != 6882 {
		t.Errorf("Second peer port = %d, want 6882", port2)
	}
}