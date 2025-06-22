package peer

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestHandshakeSerialize(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	peerID := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	
	h := NewHandshake(infoHash, peerID)
	data := h.Serialize()
	
	if len(data) != HandshakeLength {
		t.Errorf("Serialized length = %d, want %d", len(data), HandshakeLength)
	}
	
	// Check protocol string length
	if data[0] != 19 {
		t.Errorf("Protocol string length = %d, want 19", data[0])
	}
	
	// Check protocol string
	if string(data[1:20]) != ProtocolIdentifier {
		t.Errorf("Protocol string = %s, want %s", string(data[1:20]), ProtocolIdentifier)
	}
	
	// Check reserved bytes (should be zeros by default)
	for i := 20; i < 28; i++ {
		if data[i] != 0 {
			t.Errorf("Reserved byte %d = %d, want 0", i-20, data[i])
		}
	}
	
	// Check info hash
	if !bytes.Equal(data[28:48], infoHash[:]) {
		t.Errorf("Info hash mismatch")
	}
	
	// Check peer ID
	if !bytes.Equal(data[48:68], peerID[:]) {
		t.Errorf("Peer ID mismatch")
	}
}

func TestHandshakeReadWrite(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	peerID := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	
	h1 := NewHandshake(infoHash, peerID)
	
	// Write to buffer
	var buf bytes.Buffer
	err := h1.Write(&buf)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	
	// Read from buffer
	h2, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	
	// Compare
	if !h1.Equal(h2) {
		t.Errorf("Handshakes not equal after round trip")
	}
}

func TestHandshakeExtensions(t *testing.T) {
	h := &Handshake{}
	
	// Test setting extensions
	ext := Extensions{
		DHT:         true,
		FastPeers:   true,
		ExtProtocol: true,
	}
	
	h.SetExtensions(ext)
	
	// Test parsing extensions
	parsed := h.ParseExtensions()
	
	if parsed.DHT != ext.DHT {
		t.Errorf("DHT = %v, want %v", parsed.DHT, ext.DHT)
	}
	
	if parsed.FastPeers != ext.FastPeers {
		t.Errorf("FastPeers = %v, want %v", parsed.FastPeers, ext.FastPeers)
	}
	
	if parsed.ExtProtocol != ext.ExtProtocol {
		t.Errorf("ExtProtocol = %v, want %v", parsed.ExtProtocol, ext.ExtProtocol)
	}
}

func TestHandshakeInvalidProtocol(t *testing.T) {
	// Create invalid handshake data with wrong protocol string
	data := make([]byte, 68)
	data[0] = 19 // length
	copy(data[1:20], "Invalid protocol   ")
	
	buf := bytes.NewBuffer(data)
	_, err := Read(buf)
	
	if err == nil {
		t.Error("Expected error for invalid protocol string")
	}
}

func TestHandshakeInfoHashMismatch(t *testing.T) {
	// Create mock connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	
	infoHash1 := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	infoHash2 := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	peerID := [20]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	
	// Server sends handshake with different info hash
	go func() {
		h := NewHandshake(infoHash2, peerID)
		h.Write(server)
	}()
	
	// Client expects different info hash
	_, err := DoHandshake(client, infoHash1, peerID)
	
	if err == nil {
		t.Error("Expected error for info hash mismatch")
	}
}

func TestValidatePeerID(t *testing.T) {
	tests := []struct {
		name     string
		peerID   [20]byte
		expected bool
	}{
		{
			name:     "valid peer ID",
			peerID:   [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
			expected: true,
		},
		{
			name:     "all zeros (invalid)",
			peerID:   [20]byte{},
			expected: false,
		},
		{
			name:     "single non-zero byte",
			peerID:   [20]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			expected: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePeerID(tt.peerID)
			if result != tt.expected {
				t.Errorf("ValidatePeerID() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHandshakeBinaryMarshaling(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	peerID := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	
	h1 := NewHandshake(infoHash, peerID)
	
	// Marshal
	data, err := h1.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}
	
	// Unmarshal
	h2 := &Handshake{}
	err = h2.UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}
	
	// Compare
	if !h1.Equal(h2) {
		t.Errorf("Handshakes not equal after binary marshaling")
	}
}

func TestReadHandshakeTimeout(t *testing.T) {
	// Test successful read
	data := NewHandshake([20]byte{1}, [20]byte{2}).Serialize()
	buf := bytes.NewBuffer(data)
	
	h, err := ReadHandshakeTimeout(buf, 1*time.Second)
	if err != nil {
		t.Errorf("ReadHandshakeTimeout failed: %v", err)
	}
	if h == nil {
		t.Error("Expected handshake, got nil")
	}
	
	// Test timeout (with a reader that blocks)
	blockingReader := &blockingReader{}
	_, err = ReadHandshakeTimeout(blockingReader, 100*time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

// blockingReader is a reader that blocks forever
type blockingReader struct{}

func (r *blockingReader) Read(p []byte) (n int, err error) {
	// Block forever
	select {}
}