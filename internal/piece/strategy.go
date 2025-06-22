package piece

import (
	"math/rand"
	"sort"
)

// SelectionStrategy defines how pieces are selected for download
type SelectionStrategy interface {
	SelectPiece(pieces []*Piece, peerBitfield []byte) *Piece
}

// SequentialStrategy downloads pieces in order
type SequentialStrategy struct{}

// NewSequentialStrategy creates a new sequential strategy
func NewSequentialStrategy() *SequentialStrategy {
	return &SequentialStrategy{}
}

// SelectPiece selects the first missing piece
func (s *SequentialStrategy) SelectPiece(pieces []*Piece, peerBitfield []byte) *Piece {
	for i, piece := range pieces {
		// Check if we already have this piece
		if piece.State == PieceStateVerified {
			continue
		}
		
		// Check if peer has this piece
		if !peerHasPiece(peerBitfield, i) {
			continue
		}
		
		return piece
	}
	
	return nil
}

// RandomStrategy selects pieces randomly
type RandomStrategy struct {
	rand *rand.Rand
}

// NewRandomStrategy creates a new random strategy
func NewRandomStrategy() *RandomStrategy {
	return &RandomStrategy{
		rand: rand.New(rand.NewSource(42)), // Fixed seed for reproducibility
	}
}

// SelectPiece selects a random missing piece
func (s *RandomStrategy) SelectPiece(pieces []*Piece, peerBitfield []byte) *Piece {
	var available []*Piece
	
	for i, piece := range pieces {
		// Check if we already have this piece
		if piece.State == PieceStateVerified {
			continue
		}
		
		// Check if peer has this piece
		if !peerHasPiece(peerBitfield, i) {
			continue
		}
		
		available = append(available, piece)
	}
	
	if len(available) == 0 {
		return nil
	}
	
	return available[s.rand.Intn(len(available))]
}

// RarestFirstStrategy implements the rarest-first algorithm
type RarestFirstStrategy struct {
	peerBitfields map[string][]byte // peerID -> bitfield
}

// NewRarestFirstStrategy creates a new rarest-first strategy
func NewRarestFirstStrategy() *RarestFirstStrategy {
	return &RarestFirstStrategy{
		peerBitfields: make(map[string][]byte),
	}
}

// UpdatePeerBitfield updates a peer's bitfield
func (s *RarestFirstStrategy) UpdatePeerBitfield(peerID string, bitfield []byte) {
	s.peerBitfields[peerID] = bitfield
}

// RemovePeer removes a peer's bitfield
func (s *RarestFirstStrategy) RemovePeer(peerID string) {
	delete(s.peerBitfields, peerID)
}

// pieceRarity represents how rare a piece is
type pieceRarity struct {
	index  int
	rarity int // number of peers who have this piece
}

// SelectPiece selects the rarest piece that the peer has
func (s *RarestFirstStrategy) SelectPiece(pieces []*Piece, peerBitfield []byte) *Piece {
	// Calculate rarity for each piece
	var candidates []pieceRarity
	
	for i, piece := range pieces {
		// Check if we already have this piece
		if piece.State == PieceStateVerified {
			continue
		}
		
		// Check if this peer has the piece
		if !peerHasPiece(peerBitfield, i) {
			continue
		}
		
		// Count how many peers have this piece
		rarity := 0
		for _, otherBitfield := range s.peerBitfields {
			if peerHasPiece(otherBitfield, i) {
				rarity++
			}
		}
		
		candidates = append(candidates, pieceRarity{
			index:  i,
			rarity: rarity,
		})
	}
	
	if len(candidates) == 0 {
		return nil
	}
	
	// Sort by rarity (ascending - rarest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].rarity < candidates[j].rarity
	})
	
	// Return the rarest piece
	return pieces[candidates[0].index]
}

// EndGameStrategy is used when only a few pieces remain
type EndGameStrategy struct {
	threshold int // switch to end game when this many pieces remain
	baseStrategy SelectionStrategy
}

// NewEndGameStrategy creates a new end game strategy
func NewEndGameStrategy(threshold int, baseStrategy SelectionStrategy) *EndGameStrategy {
	return &EndGameStrategy{
		threshold:    threshold,
		baseStrategy: baseStrategy,
	}
}

// SelectPiece uses aggressive downloading when few pieces remain
func (s *EndGameStrategy) SelectPiece(pieces []*Piece, peerBitfield []byte) *Piece {
	// Count missing pieces
	missing := 0
	for _, piece := range pieces {
		if piece.State != PieceStateVerified {
			missing++
		}
	}
	
	// If we're in end game mode, request any available piece
	if missing <= s.threshold {
		for i, piece := range pieces {
			if piece.State != PieceStateVerified && peerHasPiece(peerBitfield, i) {
				return piece
			}
		}
		return nil
	}
	
	// Otherwise use the base strategy
	return s.baseStrategy.SelectPiece(pieces, peerBitfield)
}

// SmartStrategy combines multiple strategies
type SmartStrategy struct {
	sequential   *SequentialStrategy
	rarestFirst  *RarestFirstStrategy
	endGame      *EndGameStrategy
	random       *RandomStrategy
	
	// Configuration
	sequentialThreshold int // use sequential for first N pieces
	endGameThreshold    int // switch to end game when N pieces remain
}

// NewSmartStrategy creates a new smart strategy
func NewSmartStrategy() *SmartStrategy {
	rarestFirst := NewRarestFirstStrategy()
	
	return &SmartStrategy{
		sequential:          &SequentialStrategy{},
		rarestFirst:         rarestFirst,
		endGame:            NewEndGameStrategy(5, rarestFirst),
		random:             NewRandomStrategy(),
		sequentialThreshold: 4,  // Download first 4 pieces sequentially
		endGameThreshold:    10, // Switch to end game at 10 pieces
	}
}

// UpdatePeerBitfield updates peer information for rarest-first
func (s *SmartStrategy) UpdatePeerBitfield(peerID string, bitfield []byte) {
	s.rarestFirst.UpdatePeerBitfield(peerID, bitfield)
}

// RemovePeer removes peer information
func (s *SmartStrategy) RemovePeer(peerID string) {
	s.rarestFirst.RemovePeer(peerID)
}

// SelectPiece uses the most appropriate strategy based on download state
func (s *SmartStrategy) SelectPiece(pieces []*Piece, peerBitfield []byte) *Piece {
	// Count completed pieces
	completed := 0
	total := len(pieces)
	
	for _, piece := range pieces {
		if piece.State == PieceStateVerified {
			completed++
		}
	}
	
	remaining := total - completed
	
	// Use sequential for the first few pieces
	if completed < s.sequentialThreshold {
		if piece := s.sequential.SelectPiece(pieces, peerBitfield); piece != nil {
			return piece
		}
	}
	
	// Use end game for the last few pieces
	if remaining <= s.endGameThreshold {
		return s.endGame.SelectPiece(pieces, peerBitfield)
	}
	
	// Use rarest-first for the middle
	return s.rarestFirst.SelectPiece(pieces, peerBitfield)
}

// PriorityStrategy allows manual piece prioritization
type PriorityStrategy struct {
	priorities   map[int]int // piece index -> priority (higher = more important)
	baseStrategy SelectionStrategy
}

// NewPriorityStrategy creates a new priority strategy
func NewPriorityStrategy(baseStrategy SelectionStrategy) *PriorityStrategy {
	return &PriorityStrategy{
		priorities:   make(map[int]int),
		baseStrategy: baseStrategy,
	}
}

// SetPriority sets the priority for a piece
func (s *PriorityStrategy) SetPriority(pieceIndex, priority int) {
	s.priorities[pieceIndex] = priority
}

// SelectPiece selects the highest priority piece available
func (s *PriorityStrategy) SelectPiece(pieces []*Piece, peerBitfield []byte) *Piece {
	var candidates []struct {
		piece    *Piece
		priority int
	}
	
	for i, piece := range pieces {
		// Check if we already have this piece
		if piece.State == PieceStateVerified {
			continue
		}
		
		// Check if peer has this piece
		if !peerHasPiece(peerBitfield, i) {
			continue
		}
		
		priority := s.priorities[i] // default 0 if not set
		candidates = append(candidates, struct {
			piece    *Piece
			priority int
		}{piece, priority})
	}
	
	if len(candidates) == 0 {
		return nil
	}
	
	// Sort by priority (descending - highest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].priority > candidates[j].priority
	})
	
	// If highest priority piece has priority 0, use base strategy
	if candidates[0].priority == 0 {
		return s.baseStrategy.SelectPiece(pieces, peerBitfield)
	}
	
	return candidates[0].piece
}

// Utility functions

// peerHasPiece checks if a peer has a specific piece based on their bitfield
func peerHasPiece(bitfield []byte, pieceIndex int) bool {
	if len(bitfield) == 0 {
		return false
	}
	
	byteIndex := pieceIndex / 8
	bitIndex := pieceIndex % 8
	
	if byteIndex >= len(bitfield) {
		return false
	}
	
	return (bitfield[byteIndex] & (1 << (7 - bitIndex))) != 0
}

// GetStrategyByName returns a strategy by name
func GetStrategyByName(name string) SelectionStrategy {
	switch name {
	case "sequential":
		return &SequentialStrategy{}
	case "random":
		return NewRandomStrategy()
	case "rarest-first":
		return NewRarestFirstStrategy()
	case "smart":
		return NewSmartStrategy()
	default:
		return &SequentialStrategy{} // default
	}
}