package peer

import (
	"bytes"
	"testing"
	"time"
)

func TestMessageSerialization(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantLen int
	}{
		{
			name:    "keep-alive",
			msg:     nil,
			wantLen: 4,
		},
		{
			name:    "choke",
			msg:     NewChokeMessage(),
			wantLen: 5,
		},
		{
			name:    "have",
			msg:     NewHaveMessage(42),
			wantLen: 9,
		},
		{
			name:    "bitfield",
			msg:     NewBitfieldMessage([]byte{0xFF, 0x00, 0xAA}),
			wantLen: 8,
		},
		{
			name:    "request",
			msg:     NewRequestMessage(1, 0, BlockSize),
			wantLen: 17,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.msg.Serialize()
			if len(data) != tt.wantLen {
				t.Errorf("Serialized length = %d, want %d", len(data), tt.wantLen)
			}
		})
	}
}

func TestMessageRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  *Message
	}{
		{"choke", NewChokeMessage()},
		{"unchoke", NewUnchokeMessage()},
		{"interested", NewInterestedMessage()},
		{"not-interested", NewNotInterestedMessage()},
		{"have", NewHaveMessage(42)},
		{"bitfield", NewBitfieldMessage([]byte{0xFF, 0x00, 0xAA, 0x55})},
		{"request", NewRequestMessage(1, 0, BlockSize)},
		{"piece", NewPieceMessage(1, 0, []byte("test data"))},
		{"cancel", NewCancelMessage(1, 0, BlockSize)},
		{"port", NewPortMessage(6881)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize
			data := tt.msg.Serialize()
			
			// Deserialize
			buf := bytes.NewBuffer(data)
			msg, err := ReadMessage(buf)
			if err != nil {
				t.Fatalf("ReadMessage failed: %v", err)
			}

			// Compare
			if msg.ID != tt.msg.ID {
				t.Errorf("Message ID = %d, want %d", msg.ID, tt.msg.ID)
			}
			
			if !bytes.Equal(msg.Payload, tt.msg.Payload) {
				t.Errorf("Payload mismatch: got %v, want %v", msg.Payload, tt.msg.Payload)
			}
		})
	}
}

func TestKeepAliveMessage(t *testing.T) {
	// Test serialization
	data := KeepAlive().Serialize()
	expected := []byte{0, 0, 0, 0}
	if !bytes.Equal(data, expected) {
		t.Errorf("Keep-alive serialization = %v, want %v", data, expected)
	}

	// Test deserialization
	buf := bytes.NewBuffer(data)
	msg, err := ReadMessage(buf)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if !IsKeepAlive(msg) {
		t.Errorf("Expected keep-alive message, got %v", msg)
	}
}

func TestParseHave(t *testing.T) {
	msg := NewHaveMessage(42)
	index, err := msg.ParseHave()
	if err != nil {
		t.Fatalf("ParseHave failed: %v", err)
	}

	if index != 42 {
		t.Errorf("Parsed index = %d, want 42", index)
	}

	// Test with wrong message type
	wrongMsg := NewChokeMessage()
	_, err = wrongMsg.ParseHave()
	if err == nil {
		t.Error("Expected error for wrong message type")
	}
}

func TestParseBitfield(t *testing.T) {
	original := []byte{0xFF, 0x00, 0xAA, 0x55}
	msg := NewBitfieldMessage(original)
	
	bitfield, err := msg.ParseBitfield()
	if err != nil {
		t.Fatalf("ParseBitfield failed: %v", err)
	}

	if !bytes.Equal(bitfield, original) {
		t.Errorf("Parsed bitfield = %v, want %v", bitfield, original)
	}

	// Test that returned slice is a copy
	bitfield[0] = 0x00
	if original[0] != 0xFF {
		t.Error("Bitfield was not copied, original was modified")
	}
}

func TestParseRequest(t *testing.T) {
	msg := NewRequestMessage(1, 16384, BlockSize)
	
	index, begin, length, err := msg.ParseRequest()
	if err != nil {
		t.Fatalf("ParseRequest failed: %v", err)
	}

	if index != 1 {
		t.Errorf("Parsed index = %d, want 1", index)
	}
	if begin != 16384 {
		t.Errorf("Parsed begin = %d, want 16384", begin)
	}
	if length != BlockSize {
		t.Errorf("Parsed length = %d, want %d", length, BlockSize)
	}
}

func TestParsePiece(t *testing.T) {
	testData := []byte("hello world")
	msg := NewPieceMessage(1, 0, testData)
	
	index, begin, block, err := msg.ParsePiece()
	if err != nil {
		t.Fatalf("ParsePiece failed: %v", err)
	}

	if index != 1 {
		t.Errorf("Parsed index = %d, want 1", index)
	}
	if begin != 0 {
		t.Errorf("Parsed begin = %d, want 0", begin)
	}
	if !bytes.Equal(block, testData) {
		t.Errorf("Parsed block = %v, want %v", block, testData)
	}

	// Test that returned block is a copy
	block[0] = 'H'
	if testData[0] != 'h' {
		t.Error("Block was not copied, original was modified")
	}
}

func TestParseCancel(t *testing.T) {
	msg := NewCancelMessage(2, 32768, BlockSize)
	
	index, begin, length, err := msg.ParseCancel()
	if err != nil {
		t.Fatalf("ParseCancel failed: %v", err)
	}

	if index != 2 {
		t.Errorf("Parsed index = %d, want 2", index)
	}
	if begin != 32768 {
		t.Errorf("Parsed begin = %d, want 32768", begin)
	}
	if length != BlockSize {
		t.Errorf("Parsed length = %d, want %d", length, BlockSize)
	}
}

func TestParsePort(t *testing.T) {
	msg := NewPortMessage(6881)
	
	port, err := msg.ParsePort()
	if err != nil {
		t.Fatalf("ParsePort failed: %v", err)
	}

	if port != 6881 {
		t.Errorf("Parsed port = %d, want 6881", port)
	}
}

func TestMessageValidation(t *testing.T) {
	tests := []struct {
		name  string
		msg   *Message
		valid bool
	}{
		{"keep-alive", nil, true},
		{"choke", NewChokeMessage(), true},
		{"have", NewHaveMessage(42), true},
		{"bitfield", NewBitfieldMessage([]byte{0xFF}), true},
		{"request", NewRequestMessage(1, 0, BlockSize), true},
		{"piece", NewPieceMessage(1, 0, []byte("data")), true},
		{"invalid-have", &Message{ID: MsgHave, Payload: []byte{1, 2, 3}}, false},
		{"invalid-request", &Message{ID: MsgRequest, Payload: []byte{1, 2, 3}}, false},
		{"unknown-message", &Message{ID: 255, Payload: nil}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.IsValid() != tt.valid {
				t.Errorf("IsValid() = %v, want %v", tt.msg.IsValid(), tt.valid)
			}
		})
	}
}

func TestMessageString(t *testing.T) {
	tests := []struct {
		msg      *Message
		expected string
	}{
		{nil, "KeepAlive"},
		{NewChokeMessage(), "Choke(payload_len=0)"},
		{NewHaveMessage(42), "Have(payload_len=4)"},
		{&Message{ID: 255, Payload: nil}, "Unknown(255)(payload_len=0)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.msg.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMessageLength(t *testing.T) {
	tests := []struct {
		msg      *Message
		expected int
	}{
		{nil, 4},
		{NewChokeMessage(), 5},
		{NewHaveMessage(42), 9},
		{NewBitfieldMessage(make([]byte, 10)), 15},
	}

	for _, tt := range tests {
		t.Run(tt.msg.String(), func(t *testing.T) {
			if got := tt.msg.MessageLength(); got != tt.expected {
				t.Errorf("MessageLength() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCopyMessage(t *testing.T) {
	original := NewPieceMessage(1, 0, []byte("test data"))
	copy := CopyMessage(original)

	// Test that they're equal
	if copy.ID != original.ID {
		t.Errorf("Copy ID = %d, want %d", copy.ID, original.ID)
	}
	
	if !bytes.Equal(copy.Payload, original.Payload) {
		t.Errorf("Copy payload mismatch")
	}

	// Test that they're independent
	copy.Payload[0] = 0xFF
	if original.Payload[0] == 0xFF {
		t.Error("Original was modified when copy was changed")
	}

	// Test nil copy
	nilCopy := CopyMessage(nil)
	if nilCopy != nil {
		t.Error("Expected nil copy of nil message")
	}
}

func TestReadMessageTimeout(t *testing.T) {
	// Test successful read
	msg := NewHaveMessage(42)
	data := msg.Serialize()
	buf := bytes.NewBuffer(data)

	result, err := ReadMessageTimeout(buf, 1*time.Second)
	if err != nil {
		t.Errorf("ReadMessageTimeout failed: %v", err)
	}
	if result == nil {
		t.Error("Expected message, got nil")
	}

	// Test timeout
	slowReader := &slowReader{}
	_, err = ReadMessageTimeout(slowReader, 100*time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestLargeMessage(t *testing.T) {
	// Test message that exceeds max length
	largePayload := make([]byte, MaxMessageLength)
	data := make([]byte, 4+1+len(largePayload))
	
	// Write length prefix (too large)
	data[0] = 0xFF
	data[1] = 0xFF
	data[2] = 0xFF
	data[3] = 0xFF
	
	buf := bytes.NewBuffer(data)
	_, err := ReadMessage(buf)
	if err == nil {
		t.Error("Expected error for oversized message")
	}
}

// slowReader blocks for a while before returning data
type slowReader struct{}

func (r *slowReader) Read(p []byte) (n int, err error) {
	time.Sleep(200 * time.Millisecond)
	return 0, nil
}