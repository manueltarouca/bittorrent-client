package piece

import (
	"fmt"
	"testing"
)

// Helper function to create test pieces
func createTestPieces(count int) []*Piece {
	pieces := make([]*Piece, count)
	for i := 0; i < count; i++ {
		pieces[i] = NewPiece(i, 16384, [20]byte{byte(i)})
	}
	return pieces
}

// Helper function to create a bitfield with specific pieces set
func createBitfield(pieceCount int, availablePieces []int) []byte {
	bitfieldSize := (pieceCount + 7) / 8
	bitfield := make([]byte, bitfieldSize)
	
	for _, piece := range availablePieces {
		byteIndex := piece / 8
		bitIndex := piece % 8
		if byteIndex < len(bitfield) {
			bitfield[byteIndex] |= (1 << (7 - bitIndex))
		}
	}
	
	return bitfield
}

// Helper function to remove an element from a slice
func removeElement(slice []int, element int) []int {
	result := make([]int, 0, len(slice))
	for _, v := range slice {
		if v != element {
			result = append(result, v)
		}
	}
	return result
}

func TestSequentialStrategy(t *testing.T) {
	strategy := NewSequentialStrategy()
	pieces := createTestPieces(5)
	
	// Peer has pieces 1, 2, 3
	peerBitfield := createBitfield(5, []int{1, 2, 3})
	
	// Should select piece 1 (first available)
	selected := strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 1 {
		t.Errorf("Expected piece 1, got piece %d", selected.Index)
	}
	
	// Mark piece 1 as verified
	pieces[1].State = PieceStateVerified
	
	// Should now select piece 2
	selected = strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 2 {
		t.Errorf("Expected piece 2, got piece %d", selected.Index)
	}
}

func TestSequentialStrategyNoAvailablePieces(t *testing.T) {
	strategy := NewSequentialStrategy()
	pieces := createTestPieces(3)
	
	// Peer has no pieces
	peerBitfield := createBitfield(3, []int{})
	
	selected := strategy.SelectPiece(pieces, peerBitfield)
	if selected != nil {
		t.Error("Expected no piece to be selected when peer has none")
	}
	
	// Mark all pieces as verified
	for _, piece := range pieces {
		piece.State = PieceStateVerified
	}
	
	// Peer has all pieces but we have them too
	peerBitfield = createBitfield(3, []int{0, 1, 2})
	selected = strategy.SelectPiece(pieces, peerBitfield)
	if selected != nil {
		t.Error("Expected no piece to be selected when all are verified")
	}
}

func TestRandomStrategy(t *testing.T) {
	strategy := NewRandomStrategy()
	pieces := createTestPieces(10)
	
	// Peer has pieces 2, 5, 7
	peerBitfield := createBitfield(10, []int{2, 5, 7})
	
	// Should select one of the available pieces
	selected := strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	
	validSelections := map[int]bool{2: true, 5: true, 7: true}
	if !validSelections[selected.Index] {
		t.Errorf("Selected piece %d is not in available set {2, 5, 7}", selected.Index)
	}
}

func TestRarestFirstStrategy(t *testing.T) {
	strategy := NewRarestFirstStrategy()
	pieces := createTestPieces(5)
	
	// Set up peer bitfields
	// Peer1 has pieces 0, 1, 2
	// Peer2 has pieces 1, 2, 3  
	// Peer3 has pieces 2, 3, 4
	// So piece 0 and 4 are rarest (1 peer each), piece 2 is most common (3 peers)
	strategy.UpdatePeerBitfield("peer1", createBitfield(5, []int{0, 1, 2}))
	strategy.UpdatePeerBitfield("peer2", createBitfield(5, []int{1, 2, 3}))
	strategy.UpdatePeerBitfield("peer3", createBitfield(5, []int{2, 3, 4}))
	
	// Current peer has pieces 0, 1, 4
	currentPeerBitfield := createBitfield(5, []int{0, 1, 4})
	
	// Should select piece 0 or 4 (both rarest with 1 peer each)
	selected := strategy.SelectPiece(pieces, currentPeerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 0 && selected.Index != 4 {
		t.Errorf("Expected piece 0 or 4 (rarest), got piece %d", selected.Index)
	}
	
	firstSelected := selected.Index
	
	// Mark the first selected piece as verified
	pieces[firstSelected].State = PieceStateVerified
	
	// Should now select the other rarest piece or piece 1
	selected = strategy.SelectPiece(pieces, currentPeerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	
	// If we selected 0 first, should get 4 next (or 1)
	// If we selected 4 first, should get 0 next (or 1)
	validNext := []int{0, 1, 4}
	validNext = removeElement(validNext, firstSelected) // Remove the already selected piece
	
	found := false
	for _, valid := range validNext {
		if selected.Index == valid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected one of %v, got piece %d", validNext, selected.Index)
	}
}

func TestRarestFirstStrategyRemovePeer(t *testing.T) {
	strategy := NewRarestFirstStrategy()
	pieces := createTestPieces(3)
	
	strategy.UpdatePeerBitfield("peer1", createBitfield(3, []int{0, 1}))
	strategy.UpdatePeerBitfield("peer2", createBitfield(3, []int{1, 2}))
	
	// Remove peer1
	strategy.RemovePeer("peer1")
	
	// Current peer has all pieces
	currentPeerBitfield := createBitfield(3, []int{0, 1, 2})
	
	// Should select piece 0 (only peer2 doesn't have it, so it's rarest for remaining peers)
	selected := strategy.SelectPiece(pieces, currentPeerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	// Piece 0 should have rarity 0 (no remaining peers have it)
	// Pieces 1,2 should have rarity 1 (peer2 has them)
	// So piece 0 should be selected as rarest
	if selected.Index != 0 {
		t.Errorf("Expected piece 0 (rarest after peer removal), got piece %d", selected.Index)
	}
}

func TestEndGameStrategy(t *testing.T) {
	baseStrategy := NewSequentialStrategy()
	strategy := NewEndGameStrategy(2, baseStrategy) // End game when 2 or fewer pieces remain
	pieces := createTestPieces(5)
	
	// Peer has pieces 1, 3, 4
	peerBitfield := createBitfield(5, []int{1, 3, 4})
	
	// Mark pieces 0, 1, 2 as verified (3 remaining)
	pieces[0].State = PieceStateVerified
	pieces[1].State = PieceStateVerified
	pieces[2].State = PieceStateVerified
	
	// Not in end game yet (3 > 2), should use base strategy
	selected := strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 3 {
		t.Errorf("Expected piece 3 (sequential strategy), got piece %d", selected.Index)
	}
	
	// Mark piece 3 as verified (2 remaining)
	pieces[3].State = PieceStateVerified
	
	// Now in end game (2 <= 2), should select any available piece
	selected = strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 4 {
		t.Errorf("Expected piece 4 (only remaining available), got piece %d", selected.Index)
	}
}

func TestSmartStrategy(t *testing.T) {
	strategy := NewSmartStrategy()
	pieces := createTestPieces(20)
	
	// Update peer bitfields for rarest-first
	strategy.UpdatePeerBitfield("peer1", createBitfield(20, []int{0, 1, 2, 3, 4, 5}))
	strategy.UpdatePeerBitfield("peer2", createBitfield(20, []int{2, 3, 4, 5, 6, 7}))
	
	// Peer has first 10 pieces
	peerBitfield := createBitfield(20, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	
	// Should use sequential for first few pieces
	selected := strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 0 {
		t.Errorf("Expected piece 0 (sequential), got piece %d", selected.Index)
	}
	
	// Mark first 5 pieces as verified (should switch from sequential to rarest-first)
	for i := 0; i < 5; i++ {
		pieces[i].State = PieceStateVerified
	}
	
	// Should now use rarest-first and select piece that fewer peers have
	selected = strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	// Should prefer pieces that are rarer
}

func TestPriorityStrategy(t *testing.T) {
	baseStrategy := NewSequentialStrategy()
	strategy := NewPriorityStrategy(baseStrategy)
	pieces := createTestPieces(5)
	
	// Set priorities: piece 3 has highest priority
	strategy.SetPriority(3, 10)
	strategy.SetPriority(1, 5)
	
	// Peer has pieces 1, 2, 3
	peerBitfield := createBitfield(5, []int{1, 2, 3})
	
	// Should select piece 3 (highest priority)
	selected := strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 3 {
		t.Errorf("Expected piece 3 (highest priority), got piece %d", selected.Index)
	}
	
	// Mark piece 3 as verified
	pieces[3].State = PieceStateVerified
	
	// Should now select piece 1 (next highest priority)
	selected = strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 1 {
		t.Errorf("Expected piece 1 (next priority), got piece %d", selected.Index)
	}
	
	// Mark piece 1 as verified
	pieces[1].State = PieceStateVerified
	
	// Should fall back to base strategy for piece 2 (no explicit priority)
	selected = strategy.SelectPiece(pieces, peerBitfield)
	if selected == nil {
		t.Fatal("Expected a piece to be selected")
	}
	if selected.Index != 2 {
		t.Errorf("Expected piece 2 (base strategy), got piece %d", selected.Index)
	}
}

func TestPeerHasPiece(t *testing.T) {
	// Create bitfield: 10110000 01100000 (pieces 0, 2, 3, 9, 10 available)
	bitfield := []byte{0xB0, 0x60}
	
	tests := []struct {
		piece    int
		expected bool
	}{
		{0, true},
		{1, false},
		{2, true},
		{3, true},
		{4, false},
		{5, false},
		{6, false},
		{7, false},
		{8, false},
		{9, true},
		{10, true},
		{11, false},
		{15, false},
		{16, false}, // Out of range
	}
	
	for _, tt := range tests {
		result := peerHasPiece(bitfield, tt.piece)
		if result != tt.expected {
			t.Errorf("peerHasPiece(bitfield, %d) = %v, want %v", tt.piece, result, tt.expected)
		}
	}
	
	// Test with nil/empty bitfield
	if peerHasPiece(nil, 0) {
		t.Error("peerHasPiece with nil bitfield should return false")
	}
	
	if peerHasPiece([]byte{}, 0) {
		t.Error("peerHasPiece with empty bitfield should return false")
	}
}

func TestGetStrategyByName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"sequential", "*piece.SequentialStrategy"},
		{"random", "*piece.RandomStrategy"},
		{"rarest-first", "*piece.RarestFirstStrategy"},
		{"smart", "*piece.SmartStrategy"},
		{"unknown", "*piece.SequentialStrategy"}, // Should default to sequential
	}
	
	for _, tt := range tests {
		strategy := GetStrategyByName(tt.name)
		if strategy == nil {
			t.Errorf("GetStrategyByName(%s) returned nil", tt.name)
			continue
		}
		
		// Check type by name (since we can't easily compare interface types)
		strategyType := fmt.Sprintf("%T", strategy)
		if strategyType != tt.expected {
			t.Errorf("GetStrategyByName(%s) = %s, want %s", tt.name, strategyType, tt.expected)
		}
	}
}