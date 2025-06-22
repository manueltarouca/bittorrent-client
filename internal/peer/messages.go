package peer

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// Message types
const (
	MsgChoke         = 0
	MsgUnchoke       = 1
	MsgInterested    = 2
	MsgNotInterested = 3
	MsgHave          = 4
	MsgBitfield      = 5
	MsgRequest       = 6
	MsgPiece         = 7
	MsgCancel        = 8
	MsgPort          = 9 // DHT extension
)

const (
	// MessageTimeout is the timeout for message operations
	MessageTimeout = 30 * time.Second
	
	// MaxMessageLength is the maximum allowed message length
	MaxMessageLength = 131072 // 128KB
	
	// BlockSize is the standard block size for piece requests
	BlockSize = 16384 // 16KB
)

// Message represents a BitTorrent protocol message
type Message struct {
	ID      uint8
	Payload []byte
}

// NewMessage creates a new message with the given ID and payload
func NewMessage(id uint8, payload []byte) *Message {
	return &Message{
		ID:      id,
		Payload: payload,
	}
}

// Serialize converts a message to bytes
func (m *Message) Serialize() []byte {
	if m == nil {
		// Keep-alive message
		return []byte{0, 0, 0, 0}
	}
	
	length := uint32(1 + len(m.Payload))
	buf := make([]byte, 4+length)
	
	// Write length prefix
	binary.BigEndian.PutUint32(buf[0:4], length)
	
	// Write message ID
	buf[4] = m.ID
	
	// Write payload
	copy(buf[5:], m.Payload)
	
	return buf
}

// ReadMessage reads a message from a connection
func ReadMessage(r io.Reader) (*Message, error) {
	// Set read timeout if it's a net.Conn
	if conn, ok := r.(net.Conn); ok {
		conn.SetReadDeadline(time.Now().Add(MessageTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}
	
	// Read length prefix (4 bytes)
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, fmt.Errorf("failed to read message length: %w", err)
	}
	
	length := binary.BigEndian.Uint32(lengthBuf)
	
	// Keep-alive message
	if length == 0 {
		return nil, nil
	}
	
	// Validate message length
	if length > MaxMessageLength {
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}
	
	// Read message ID and payload
	messageBuf := make([]byte, length)
	if _, err := io.ReadFull(r, messageBuf); err != nil {
		return nil, fmt.Errorf("failed to read message: %w", err)
	}
	
	return &Message{
		ID:      messageBuf[0],
		Payload: messageBuf[1:],
	}, nil
}

// WriteMessage writes a message to a connection
func WriteMessage(w io.Writer, msg *Message) error {
	// Set write timeout if it's a net.Conn
	if conn, ok := w.(net.Conn); ok {
		conn.SetWriteDeadline(time.Now().Add(MessageTimeout))
		defer conn.SetWriteDeadline(time.Time{})
	}
	
	data := msg.Serialize()
	n, err := w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	
	if n != len(data) {
		return fmt.Errorf("incomplete message write: wrote %d bytes, expected %d", n, len(data))
	}
	
	return nil
}

// Specific message constructors

// NewChokeMessage creates a choke message
func NewChokeMessage() *Message {
	return NewMessage(MsgChoke, nil)
}

// NewUnchokeMessage creates an unchoke message
func NewUnchokeMessage() *Message {
	return NewMessage(MsgUnchoke, nil)
}

// NewInterestedMessage creates an interested message
func NewInterestedMessage() *Message {
	return NewMessage(MsgInterested, nil)
}

// NewNotInterestedMessage creates a not interested message
func NewNotInterestedMessage() *Message {
	return NewMessage(MsgNotInterested, nil)
}

// NewHaveMessage creates a have message for a piece index
func NewHaveMessage(index uint32) *Message {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, index)
	return NewMessage(MsgHave, payload)
}

// NewBitfieldMessage creates a bitfield message
func NewBitfieldMessage(bitfield []byte) *Message {
	return NewMessage(MsgBitfield, bitfield)
}

// NewRequestMessage creates a request message for a piece block
func NewRequestMessage(index, begin, length uint32) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], length)
	return NewMessage(MsgRequest, payload)
}

// NewPieceMessage creates a piece message with block data
func NewPieceMessage(index, begin uint32, block []byte) *Message {
	payload := make([]byte, 8+len(block))
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	copy(payload[8:], block)
	return NewMessage(MsgPiece, payload)
}

// NewCancelMessage creates a cancel message for a piece block
func NewCancelMessage(index, begin, length uint32) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], length)
	return NewMessage(MsgCancel, payload)
}

// NewPortMessage creates a port message for DHT
func NewPortMessage(port uint16) *Message {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, port)
	return NewMessage(MsgPort, payload)
}

// Message parsing methods

// ParseHave parses a have message and returns the piece index
func (m *Message) ParseHave() (uint32, error) {
	if m.ID != MsgHave {
		return 0, fmt.Errorf("not a have message: ID %d", m.ID)
	}
	
	if len(m.Payload) != 4 {
		return 0, fmt.Errorf("invalid have payload length: %d", len(m.Payload))
	}
	
	return binary.BigEndian.Uint32(m.Payload), nil
}

// ParseBitfield parses a bitfield message and returns the bitfield
func (m *Message) ParseBitfield() ([]byte, error) {
	if m.ID != MsgBitfield {
		return nil, fmt.Errorf("not a bitfield message: ID %d", m.ID)
	}
	
	// Return a copy to avoid mutation
	bitfield := make([]byte, len(m.Payload))
	copy(bitfield, m.Payload)
	return bitfield, nil
}

// ParseRequest parses a request message and returns index, begin, length
func (m *Message) ParseRequest() (index, begin, length uint32, err error) {
	if m.ID != MsgRequest {
		return 0, 0, 0, fmt.Errorf("not a request message: ID %d", m.ID)
	}
	
	if len(m.Payload) != 12 {
		return 0, 0, 0, fmt.Errorf("invalid request payload length: %d", len(m.Payload))
	}
	
	index = binary.BigEndian.Uint32(m.Payload[0:4])
	begin = binary.BigEndian.Uint32(m.Payload[4:8])
	length = binary.BigEndian.Uint32(m.Payload[8:12])
	
	return index, begin, length, nil
}

// ParsePiece parses a piece message and returns index, begin, and block data
func (m *Message) ParsePiece() (index, begin uint32, block []byte, err error) {
	if m.ID != MsgPiece {
		return 0, 0, nil, fmt.Errorf("not a piece message: ID %d", m.ID)
	}
	
	if len(m.Payload) < 8 {
		return 0, 0, nil, fmt.Errorf("invalid piece payload length: %d", len(m.Payload))
	}
	
	index = binary.BigEndian.Uint32(m.Payload[0:4])
	begin = binary.BigEndian.Uint32(m.Payload[4:8])
	
	// Return a copy of the block data
	block = make([]byte, len(m.Payload)-8)
	copy(block, m.Payload[8:])
	
	return index, begin, block, nil
}

// ParseCancel parses a cancel message and returns index, begin, length
func (m *Message) ParseCancel() (index, begin, length uint32, err error) {
	if m.ID != MsgCancel {
		return 0, 0, 0, fmt.Errorf("not a cancel message: ID %d", m.ID)
	}
	
	if len(m.Payload) != 12 {
		return 0, 0, 0, fmt.Errorf("invalid cancel payload length: %d", len(m.Payload))
	}
	
	index = binary.BigEndian.Uint32(m.Payload[0:4])
	begin = binary.BigEndian.Uint32(m.Payload[4:8])
	length = binary.BigEndian.Uint32(m.Payload[8:12])
	
	return index, begin, length, nil
}

// ParsePort parses a port message and returns the port number
func (m *Message) ParsePort() (uint16, error) {
	if m.ID != MsgPort {
		return 0, fmt.Errorf("not a port message: ID %d", m.ID)
	}
	
	if len(m.Payload) != 2 {
		return 0, fmt.Errorf("invalid port payload length: %d", len(m.Payload))
	}
	
	return binary.BigEndian.Uint16(m.Payload), nil
}

// String returns a string representation of the message
func (m *Message) String() string {
	if m == nil {
		return "KeepAlive"
	}
	
	names := map[uint8]string{
		MsgChoke:         "Choke",
		MsgUnchoke:       "Unchoke",
		MsgInterested:    "Interested",
		MsgNotInterested: "NotInterested",
		MsgHave:          "Have",
		MsgBitfield:      "Bitfield",
		MsgRequest:       "Request",
		MsgPiece:         "Piece",
		MsgCancel:        "Cancel",
		MsgPort:          "Port",
	}
	
	name, ok := names[m.ID]
	if !ok {
		name = fmt.Sprintf("Unknown(%d)", m.ID)
	}
	
	return fmt.Sprintf("%s(payload_len=%d)", name, len(m.Payload))
}

// IsValid checks if a message is valid
func (m *Message) IsValid() bool {
	if m == nil {
		return true // Keep-alive is valid
	}
	
	switch m.ID {
	case MsgChoke, MsgUnchoke, MsgInterested, MsgNotInterested:
		return len(m.Payload) == 0
	case MsgHave:
		return len(m.Payload) == 4
	case MsgBitfield:
		return len(m.Payload) > 0
	case MsgRequest, MsgCancel:
		return len(m.Payload) == 12
	case MsgPiece:
		return len(m.Payload) >= 8
	case MsgPort:
		return len(m.Payload) == 2
	default:
		return false
	}
}

// MessageLength returns the total length of the message when serialized
func (m *Message) MessageLength() int {
	if m == nil {
		return 4 // Keep-alive
	}
	return 4 + 1 + len(m.Payload) // length + ID + payload
}

// KeepAlive creates a keep-alive message (nil message)
func KeepAlive() *Message {
	return nil
}

// IsKeepAlive checks if a message is a keep-alive
func IsKeepAlive(msg *Message) bool {
	return msg == nil
}

// CopyMessage creates a copy of a message
func CopyMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}
	
	payload := make([]byte, len(msg.Payload))
	copy(payload, msg.Payload)
	
	return &Message{
		ID:      msg.ID,
		Payload: payload,
	}
}

// ReadMessageTimeout reads a message with a specific timeout
func ReadMessageTimeout(r io.Reader, timeout time.Duration) (*Message, error) {
	type result struct {
		msg *Message
		err error
	}
	
	ch := make(chan result, 1)
	
	go func() {
		msg, err := ReadMessage(r)
		ch <- result{msg, err}
	}()
	
	select {
	case res := <-ch:
		return res.msg, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("message read timeout after %v", timeout)
	}
}