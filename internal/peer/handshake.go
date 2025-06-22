package peer

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	// ProtocolIdentifier is the BitTorrent protocol string
	ProtocolIdentifier = "BitTorrent protocol"
	
	// HandshakeLength is the total length of a handshake message
	HandshakeLength = 68
	
	// HandshakeTimeout is the timeout for handshake operations
	HandshakeTimeout = 30 * time.Second
)

// Handshake represents a BitTorrent handshake message
type Handshake struct {
	Pstr     string
	Reserved [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

// NewHandshake creates a new handshake message
func NewHandshake(infoHash, peerID [20]byte) *Handshake {
	return &Handshake{
		Pstr:     ProtocolIdentifier,
		Reserved: [8]byte{0, 0, 0, 0, 0, 0, 0, 0},
		InfoHash: infoHash,
		PeerID:   peerID,
	}
}

// Serialize converts the handshake to bytes
func (h *Handshake) Serialize() []byte {
	buf := make([]byte, HandshakeLength)
	
	// Protocol string length (1 byte)
	buf[0] = byte(len(h.Pstr))
	
	// Protocol string (19 bytes)
	copy(buf[1:20], h.Pstr)
	
	// Reserved bytes (8 bytes)
	copy(buf[20:28], h.Reserved[:])
	
	// Info hash (20 bytes)
	copy(buf[28:48], h.InfoHash[:])
	
	// Peer ID (20 bytes)
	copy(buf[48:68], h.PeerID[:])
	
	return buf
}

// Read reads a handshake from an io.Reader
func Read(r io.Reader) (*Handshake, error) {
	// Set read timeout if it's a net.Conn
	if conn, ok := r.(net.Conn); ok {
		conn.SetReadDeadline(time.Now().Add(HandshakeTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}
	
	// Read protocol string length
	lengthBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, fmt.Errorf("failed to read protocol string length: %w", err)
	}
	
	pstrlen := int(lengthBuf[0])
	if pstrlen == 0 {
		return nil, fmt.Errorf("invalid protocol string length: 0")
	}
	
	// Read the rest of the handshake
	handshakeBuf := make([]byte, pstrlen+48) // pstr + reserved + info_hash + peer_id
	if _, err := io.ReadFull(r, handshakeBuf); err != nil {
		return nil, fmt.Errorf("failed to read handshake: %w", err)
	}
	
	h := &Handshake{}
	
	// Extract protocol string
	h.Pstr = string(handshakeBuf[:pstrlen])
	if h.Pstr != ProtocolIdentifier {
		return nil, fmt.Errorf("invalid protocol identifier: %s", h.Pstr)
	}
	
	// Extract reserved bytes
	copy(h.Reserved[:], handshakeBuf[pstrlen:pstrlen+8])
	
	// Extract info hash
	copy(h.InfoHash[:], handshakeBuf[pstrlen+8:pstrlen+28])
	
	// Extract peer ID
	copy(h.PeerID[:], handshakeBuf[pstrlen+28:pstrlen+48])
	
	return h, nil
}

// Write writes a handshake to an io.Writer
func (h *Handshake) Write(w io.Writer) error {
	// Set write timeout if it's a net.Conn
	if conn, ok := w.(net.Conn); ok {
		conn.SetWriteDeadline(time.Now().Add(HandshakeTimeout))
		defer conn.SetWriteDeadline(time.Time{})
	}
	
	data := h.Serialize()
	n, err := w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write handshake: %w", err)
	}
	
	if n != len(data) {
		return fmt.Errorf("incomplete handshake write: wrote %d bytes, expected %d", n, len(data))
	}
	
	return nil
}

// DoHandshake performs a complete handshake with a peer
func DoHandshake(conn net.Conn, infoHash, peerID [20]byte) (*Handshake, error) {
	// Create our handshake
	ourHandshake := NewHandshake(infoHash, peerID)
	
	// Send our handshake
	if err := ourHandshake.Write(conn); err != nil {
		return nil, fmt.Errorf("failed to send handshake: %w", err)
	}
	
	// Read peer's handshake
	peerHandshake, err := Read(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read peer handshake: %w", err)
	}
	
	// Verify info hash matches
	if !bytes.Equal(peerHandshake.InfoHash[:], infoHash[:]) {
		return nil, fmt.Errorf("info hash mismatch: expected %x, got %x", 
			infoHash, peerHandshake.InfoHash)
	}
	
	return peerHandshake, nil
}

// ParseReserved parses the reserved bytes to check for extensions
type Extensions struct {
	DHT         bool // BEP 5
	FastPeers   bool // BEP 6
	ExtProtocol bool // BEP 10
}

// ParseExtensions parses the reserved bytes for supported extensions
func (h *Handshake) ParseExtensions() Extensions {
	ext := Extensions{}
	
	// Check for DHT support (reserved[7] & 0x01)
	if h.Reserved[7]&0x01 != 0 {
		ext.DHT = true
	}
	
	// Check for Fast Peers (reserved[7] & 0x04)
	if h.Reserved[7]&0x04 != 0 {
		ext.FastPeers = true
	}
	
	// Check for Extension Protocol (reserved[5] & 0x10)
	if h.Reserved[5]&0x10 != 0 {
		ext.ExtProtocol = true
	}
	
	return ext
}

// SetExtensions sets the reserved bytes based on supported extensions
func (h *Handshake) SetExtensions(ext Extensions) {
	// Clear reserved bytes
	h.Reserved = [8]byte{}
	
	// Set extension bits
	if ext.DHT {
		h.Reserved[7] |= 0x01
	}
	
	if ext.FastPeers {
		h.Reserved[7] |= 0x04
	}
	
	if ext.ExtProtocol {
		h.Reserved[5] |= 0x10
	}
}

// String returns a string representation of the handshake
func (h *Handshake) String() string {
	return fmt.Sprintf("Handshake{Protocol: %s, InfoHash: %x, PeerID: %s}",
		h.Pstr, h.InfoHash, h.PeerID)
}

// Equal checks if two handshakes are equal
func (h *Handshake) Equal(other *Handshake) bool {
	if h == nil || other == nil {
		return h == other
	}
	
	return h.Pstr == other.Pstr &&
		bytes.Equal(h.Reserved[:], other.Reserved[:]) &&
		bytes.Equal(h.InfoHash[:], other.InfoHash[:]) &&
		bytes.Equal(h.PeerID[:], other.PeerID[:])
}

// ValidatePeerID checks if a peer ID follows common conventions
func ValidatePeerID(peerID [20]byte) bool {
	// Check for all zeros (invalid)
	allZeros := true
	for _, b := range peerID {
		if b != 0 {
			allZeros = false
			break
		}
	}
	
	return !allZeros
}

// ReadHandshakeTimeout reads a handshake with a specific timeout
func ReadHandshakeTimeout(r io.Reader, timeout time.Duration) (*Handshake, error) {
	// Create a channel for the result
	type result struct {
		handshake *Handshake
		err       error
	}
	
	ch := make(chan result, 1)
	
	// Read in a goroutine
	go func() {
		h, err := Read(r)
		ch <- result{h, err}
	}()
	
	// Wait for result or timeout
	select {
	case res := <-ch:
		return res.handshake, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("handshake timeout after %v", timeout)
	}
}

// MarshalBinary implements encoding.BinaryMarshaler
func (h *Handshake) MarshalBinary() ([]byte, error) {
	return h.Serialize(), nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler
func (h *Handshake) UnmarshalBinary(data []byte) error {
	if len(data) != HandshakeLength {
		return fmt.Errorf("invalid handshake length: %d", len(data))
	}
	
	pstrlen := int(data[0])
	if pstrlen+49 != HandshakeLength {
		return fmt.Errorf("invalid protocol string length: %d", pstrlen)
	}
	
	h.Pstr = string(data[1 : 1+pstrlen])
	copy(h.Reserved[:], data[1+pstrlen:1+pstrlen+8])
	copy(h.InfoHash[:], data[1+pstrlen+8:1+pstrlen+28])
	copy(h.PeerID[:], data[1+pstrlen+28:1+pstrlen+48])
	
	return nil
}