package piece

import (
	"fmt"
	"sync"
	"time"
)

const (
	// BlockSize is the standard block size for piece requests (16KB)
	BlockSize = 16384

	// MaxRequestsPerPeer is the maximum number of outstanding requests per peer
	MaxRequestsPerPeer = 5

	// RequestTimeout is how long to wait for a block before re-requesting
	RequestTimeout = 30 * time.Second
)

// PieceState represents the state of a piece
type PieceState int

const (
	PieceStateMissing PieceState = iota
	PieceStateRequested
	PieceStateDownloaded
	PieceStateVerified
)

// String returns a string representation of the piece state
func (ps PieceState) String() string {
	switch ps {
	case PieceStateMissing:
		return "missing"
	case PieceStateRequested:
		return "requested"
	case PieceStateDownloaded:
		return "downloaded"
	case PieceStateVerified:
		return "verified"
	default:
		return "unknown"
	}
}

// Block represents a block within a piece
type Block struct {
	Index       int       // Piece index
	Begin       int       // Offset within piece
	Length      int       // Block length
	Data        []byte    // Block data (nil if not downloaded)
	RequestedAt time.Time // When this block was requested
}

// Request represents a pending block request
type Request struct {
	Block     Block
	PeerID    string
	Timestamp time.Time
}

// Piece represents a piece and its blocks
type Piece struct {
	Index    int
	Length   int
	Hash     [20]byte
	State    PieceState
	Blocks   []Block
	Requests map[string]Request // PeerID -> Request
	mu       sync.RWMutex
}

// NewPiece creates a new piece
func NewPiece(index, length int, hash [20]byte) *Piece {
	// Calculate number of blocks
	numBlocks := (length + BlockSize - 1) / BlockSize
	blocks := make([]Block, numBlocks)
	
	for i := 0; i < numBlocks; i++ {
		blockLength := BlockSize
		if i == numBlocks-1 {
			// Last block might be smaller
			remaining := length - (i * BlockSize)
			if remaining < BlockSize {
				blockLength = remaining
			}
		}
		
		blocks[i] = Block{
			Index:  index,
			Begin:  i * BlockSize,
			Length: blockLength,
		}
	}
	
	return &Piece{
		Index:    index,
		Length:   length,
		Hash:     hash,
		State:    PieceStateMissing,
		Blocks:   blocks,
		Requests: make(map[string]Request),
	}
}

// IsComplete returns true if all blocks have been downloaded
func (p *Piece) IsComplete() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	for _, block := range p.Blocks {
		if block.Data == nil {
			return false
		}
	}
	return true
}

// GetMissingBlocks returns blocks that haven't been downloaded
func (p *Piece) GetMissingBlocks() []Block {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	var missing []Block
	for _, block := range p.Blocks {
		if block.Data == nil {
			missing = append(missing, block)
		}
	}
	return missing
}

// GetPendingBlocks returns blocks that are currently being requested
func (p *Piece) GetPendingBlocks() []Request {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	var pending []Request
	for _, req := range p.Requests {
		pending = append(pending, req)
	}
	return pending
}

// AddRequest adds a pending request for a block
func (p *Piece) AddRequest(peerID string, block Block) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	key := fmt.Sprintf("%s-%d-%d", peerID, block.Begin, block.Length)
	p.Requests[key] = Request{
		Block:     block,
		PeerID:    peerID,
		Timestamp: time.Now(),
	}
}

// RemoveRequest removes a pending request
func (p *Piece) RemoveRequest(peerID string, begin, length int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	key := fmt.Sprintf("%s-%d-%d", peerID, begin, length)
	delete(p.Requests, key)
}

// SetBlockData sets the data for a specific block
func (p *Piece) SetBlockData(begin int, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Find the block
	for i, block := range p.Blocks {
		if block.Begin == begin {
			if len(data) != block.Length {
				return fmt.Errorf("block data length mismatch: got %d, expected %d", len(data), block.Length)
			}
			
			// Create a copy of the data
			blockData := make([]byte, len(data))
			copy(blockData, data)
			p.Blocks[i].Data = blockData
			
			// Remove any pending requests for this block
			var toDelete []string
			for key, req := range p.Requests {
				if req.Block.Begin == begin {
					toDelete = append(toDelete, key)
				}
			}
			for _, key := range toDelete {
				delete(p.Requests, key)
			}
			
			return nil
		}
	}
	
	return fmt.Errorf("block with begin offset %d not found", begin)
}

// GetData returns the complete piece data if all blocks are available
func (p *Piece) GetData() ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if !p.IsComplete() {
		return nil, fmt.Errorf("piece %d is not complete", p.Index)
	}
	
	data := make([]byte, 0, p.Length)
	for _, block := range p.Blocks {
		data = append(data, block.Data...)
	}
	
	return data, nil
}

// Manager manages all pieces for a torrent
type Manager struct {
	mu       sync.RWMutex
	pieces   []*Piece
	bitfield []byte
	strategy SelectionStrategy
	
	// Statistics
	stats Statistics
	
	// Disk manager for I/O operations
	diskManager DiskManager
}

// DiskManager interface for disk I/O operations
type DiskManager interface {
	WritePiece(pieceIndex int, data []byte) error
	ReadPiece(pieceIndex int) ([]byte, error)
	ReadBlock(pieceIndex, begin, length int) ([]byte, error)
	VerifyPiece(pieceIndex int, data []byte) bool
}

// Statistics contains download statistics
type Statistics struct {
	mu                 sync.RWMutex
	TotalPieces        int
	CompletedPieces    int
	VerifiedPieces     int
	ActiveRequests     int
	BytesDownloaded    int64
	BytesVerified      int64
	DownloadSpeed      float64 // bytes per second
	lastUpdate         time.Time
	lastBytesDownloaded int64
}

// NewManager creates a new piece manager
func NewManager(numPieces int, pieceLength int, lastPieceLength int, pieceHashes [][20]byte) *Manager {
	pieces := make([]*Piece, numPieces)
	
	for i := 0; i < numPieces; i++ {
		length := pieceLength
		if i == numPieces-1 && lastPieceLength > 0 {
			length = lastPieceLength
		}
		
		var hash [20]byte
		if i < len(pieceHashes) {
			hash = pieceHashes[i]
		}
		
		pieces[i] = NewPiece(i, length, hash)
	}
	
	// Initialize bitfield (all pieces missing)
	bitfieldSize := (numPieces + 7) / 8
	bitfield := make([]byte, bitfieldSize)
	
	return &Manager{
		pieces:   pieces,
		bitfield: bitfield,
		strategy: NewSequentialStrategy(), // Default strategy
		stats: Statistics{
			TotalPieces: numPieces,
			lastUpdate:  time.Now(),
		},
	}
}

// SetSelectionStrategy sets the piece selection strategy
func (m *Manager) SetSelectionStrategy(strategy SelectionStrategy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strategy = strategy
}

// SetDiskManager sets the disk manager for I/O operations
func (m *Manager) SetDiskManager(diskManager DiskManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diskManager = diskManager
}

// GetBitfield returns a copy of the current bitfield
func (m *Manager) GetBitfield() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	bitfield := make([]byte, len(m.bitfield))
	copy(bitfield, m.bitfield)
	return bitfield
}

// HasPiece returns true if we have the specified piece
func (m *Manager) HasPiece(index int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if index < 0 || index >= len(m.pieces) {
		return false
	}
	
	return m.pieces[index].State == PieceStateVerified
}

// GetPiece returns the piece at the specified index
func (m *Manager) GetPiece(index int) *Piece {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if index < 0 || index >= len(m.pieces) {
		return nil
	}
	
	return m.pieces[index]
}

// GetNextPiece returns the next piece to download based on the selection strategy
func (m *Manager) GetNextPiece(peerBitfield []byte) *Piece {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return m.strategy.SelectPiece(m.pieces, peerBitfield)
}

// GetNextBlocks returns the next blocks to request for a piece
func (m *Manager) GetNextBlocks(pieceIndex int, maxBlocks int) []Block {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if pieceIndex < 0 || pieceIndex >= len(m.pieces) {
		return nil
	}
	
	piece := m.pieces[pieceIndex]
	missing := piece.GetMissingBlocks()
	
	// Limit the number of blocks returned
	if len(missing) > maxBlocks {
		missing = missing[:maxBlocks]
	}
	
	return missing
}

// AddBlockData adds block data for a piece
func (m *Manager) AddBlockData(pieceIndex, begin int, data []byte) error {
	m.mu.RLock()
	piece := m.pieces[pieceIndex]
	m.mu.RUnlock()
	
	if piece == nil {
		return fmt.Errorf("piece %d not found", pieceIndex)
	}
	
	err := piece.SetBlockData(begin, data)
	if err != nil {
		return err
	}
	
	// Update statistics
	m.stats.mu.Lock()
	m.stats.BytesDownloaded += int64(len(data))
	m.stats.mu.Unlock()
	
	// Check if piece is complete
	if piece.IsComplete() {
		piece.mu.Lock()
		piece.State = PieceStateDownloaded
		piece.mu.Unlock()
		
		// Try to verify and store the piece
		go m.verifyAndStorePiece(pieceIndex)
	}
	
	return nil
}

// MarkPieceVerified marks a piece as verified and updates the bitfield
func (m *Manager) MarkPieceVerified(index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if index < 0 || index >= len(m.pieces) {
		return fmt.Errorf("piece index %d out of range", index)
	}
	
	piece := m.pieces[index]
	piece.mu.Lock()
	piece.State = PieceStateVerified
	piece.mu.Unlock()
	
	// Update bitfield
	byteIndex := index / 8
	bitIndex := index % 8
	if byteIndex < len(m.bitfield) {
		m.bitfield[byteIndex] |= (1 << (7 - bitIndex))
	}
	
	// Update statistics
	m.stats.mu.Lock()
	m.stats.CompletedPieces++
	m.stats.VerifiedPieces++
	m.stats.BytesVerified += int64(piece.Length)
	m.stats.mu.Unlock()
	
	return nil
}

// GetStatistics returns current download statistics
func (m *Manager) GetStatistics() Statistics {
	m.stats.mu.Lock()
	defer m.stats.mu.Unlock()
	
	// Update download speed
	now := time.Now()
	if now.Sub(m.stats.lastUpdate) >= time.Second {
		duration := now.Sub(m.stats.lastUpdate).Seconds()
		bytesDiff := m.stats.BytesDownloaded - m.stats.lastBytesDownloaded
		m.stats.DownloadSpeed = float64(bytesDiff) / duration
		
		m.stats.lastUpdate = now
		m.stats.lastBytesDownloaded = m.stats.BytesDownloaded
	}
	
	// Return a copy
	return Statistics{
		TotalPieces:        m.stats.TotalPieces,
		CompletedPieces:    m.stats.CompletedPieces,
		VerifiedPieces:     m.stats.VerifiedPieces,
		ActiveRequests:     m.stats.ActiveRequests,
		BytesDownloaded:    m.stats.BytesDownloaded,
		BytesVerified:      m.stats.BytesVerified,
		DownloadSpeed:      m.stats.DownloadSpeed,
	}
}

// GetProgress returns the download progress as a percentage
func (m *Manager) GetProgress() float64 {
	stats := m.GetStatistics()
	if stats.TotalPieces == 0 {
		return 0.0
	}
	return float64(stats.VerifiedPieces) / float64(stats.TotalPieces) * 100.0
}

// IsComplete returns true if all pieces have been verified
func (m *Manager) IsComplete() bool {
	stats := m.GetStatistics()
	return stats.VerifiedPieces == stats.TotalPieces
}

// GetMissingPieces returns indices of pieces we don't have
func (m *Manager) GetMissingPieces() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var missing []int
	for i, piece := range m.pieces {
		if piece.State != PieceStateVerified {
			missing = append(missing, i)
		}
	}
	
	return missing
}

// GetTimeoutRequests returns requests that have timed out
func (m *Manager) GetTimeoutRequests() []Request {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	now := time.Now()
	var timeouts []Request
	
	for _, piece := range m.pieces {
		piece.mu.RLock()
		for _, req := range piece.Requests {
			if now.Sub(req.Timestamp) > RequestTimeout {
				timeouts = append(timeouts, req)
			}
		}
		piece.mu.RUnlock()
	}
	
	return timeouts
}

// CancelRequest cancels a pending request
func (m *Manager) CancelRequest(pieceIndex int, peerID string, begin, length int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if pieceIndex < 0 || pieceIndex >= len(m.pieces) {
		return
	}
	
	piece := m.pieces[pieceIndex]
	piece.RemoveRequest(peerID, begin, length)
}

// AddRequest adds a pending request
func (m *Manager) AddRequest(pieceIndex int, peerID string, block Block) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if pieceIndex < 0 || pieceIndex >= len(m.pieces) {
		return
	}
	
	piece := m.pieces[pieceIndex]
	piece.AddRequest(peerID, block)
	
	m.stats.mu.Lock()
	m.stats.ActiveRequests++
	m.stats.mu.Unlock()
}

// RemoveRequest removes a pending request
func (m *Manager) RemoveRequest(pieceIndex int, peerID string, begin, length int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if pieceIndex < 0 || pieceIndex >= len(m.pieces) {
		return
	}
	
	piece := m.pieces[pieceIndex]
	piece.RemoveRequest(peerID, begin, length)
	
	m.stats.mu.Lock()
	if m.stats.ActiveRequests > 0 {
		m.stats.ActiveRequests--
	}
	m.stats.mu.Unlock()
}

// GetPieceInfo returns information about all pieces
func (m *Manager) GetPieceInfo() []PieceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	info := make([]PieceInfo, len(m.pieces))
	for i, piece := range m.pieces {
		piece.mu.RLock()
		info[i] = PieceInfo{
			Index:         piece.Index,
			Length:        piece.Length,
			State:         piece.State,
			BlocksTotal:   len(piece.Blocks),
			BlocksMissing: len(piece.GetMissingBlocks()),
			PendingRequests: len(piece.Requests),
		}
		piece.mu.RUnlock()
	}
	
	return info
}

// verifyAndStorePiece verifies a completed piece and stores it to disk
func (m *Manager) verifyAndStorePiece(pieceIndex int) {
	m.mu.RLock()
	piece := m.pieces[pieceIndex]
	diskManager := m.diskManager
	m.mu.RUnlock()
	
	if piece == nil || diskManager == nil {
		return
	}
	
	// Get the complete piece data
	data, err := piece.GetData()
	if err != nil {
		return
	}
	
	// Verify the piece hash
	if !diskManager.VerifyPiece(pieceIndex, data) {
		// Hash verification failed, reset piece to missing
		piece.mu.Lock()
		piece.State = PieceStateMissing
		// Clear all block data
		for i := range piece.Blocks {
			piece.Blocks[i].Data = nil
		}
		piece.mu.Unlock()
		return
	}
	
	// Write piece to disk
	err = diskManager.WritePiece(pieceIndex, data)
	if err != nil {
		return
	}
	
	// Mark piece as verified
	m.MarkPieceVerified(pieceIndex)
}

// ReadBlockFromDisk reads a block from disk if the piece is verified
func (m *Manager) ReadBlockFromDisk(pieceIndex, begin, length int) ([]byte, error) {
	m.mu.RLock()
	piece := m.pieces[pieceIndex]
	diskManager := m.diskManager
	m.mu.RUnlock()
	
	if piece == nil {
		return nil, fmt.Errorf("piece %d not found", pieceIndex)
	}
	
	if piece.State != PieceStateVerified {
		return nil, fmt.Errorf("piece %d not verified", pieceIndex)
	}
	
	if diskManager == nil {
		return nil, fmt.Errorf("disk manager not set")
	}
	
	return diskManager.ReadBlock(pieceIndex, begin, length)
}

// PieceInfo contains information about a piece
type PieceInfo struct {
	Index           int
	Length          int
	State           PieceState
	BlocksTotal     int
	BlocksMissing   int
	PendingRequests int
}

// BlockRequest represents a request for a block
type BlockRequest struct {
	Begin  int
	Length int
}

// GetNeededPieces returns a list of piece indices that are not yet verified
func (m *Manager) GetNeededPieces() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	needed := make([]int, 0)
	for i, piece := range m.pieces {
		if piece.State != PieceStateVerified {
			needed = append(needed, i)
		}
	}
	return needed
}

// SelectPieceForPeer selects a piece for download using the current strategy
func (m *Manager) SelectPieceForPeer(peerBitfield []byte) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.strategy == nil {
		return -1, fmt.Errorf("no selection strategy set")
	}
	
	piece := m.strategy.SelectPiece(m.pieces, peerBitfield)
	if piece == nil {
		return -1, fmt.Errorf("no piece selected")
	}
	
	return piece.Index, nil
}

// GetBlockRequests returns block requests for a piece
func (m *Manager) GetBlockRequests(pieceIndex int) []BlockRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if pieceIndex < 0 || pieceIndex >= len(m.pieces) {
		return nil
	}
	
	piece := m.pieces[pieceIndex]
	piece.mu.RLock()
	defer piece.mu.RUnlock()
	
	requests := make([]BlockRequest, 0)
	for _, block := range piece.Blocks {
		if block.Data == nil {
			requests = append(requests, BlockRequest{
				Begin:  block.Begin,
				Length: block.Length,
			})
		}
	}
	
	return requests
}

// RequestBlock marks a block as requested
func (m *Manager) RequestBlock(pieceIndex, begin, length int) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if pieceIndex < 0 || pieceIndex >= len(m.pieces) {
		return fmt.Errorf("invalid piece index: %d", pieceIndex)
	}
	
	piece := m.pieces[pieceIndex]
	piece.mu.Lock()
	defer piece.mu.Unlock()
	
	// Find the block and mark it as requested
	for i, block := range piece.Blocks {
		if block.Begin == begin && block.Length == length {
			piece.Blocks[i].RequestedAt = time.Now()
			break
		}
	}
	
	return nil
}

// GetActiveRequests returns a map of active requests with their timestamps
func (m *Manager) GetActiveRequests() map[string]time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	requests := make(map[string]time.Time)
	
	for _, piece := range m.pieces {
		piece.mu.RLock()
		for _, block := range piece.Blocks {
			if !block.RequestedAt.IsZero() && block.Data == nil {
				key := fmt.Sprintf("%d:%d", piece.Index, block.Begin)
				requests[key] = block.RequestedAt
			}
		}
		piece.mu.RUnlock()
	}
	
	return requests
}

// GetProgressCounts returns (downloaded pieces, total pieces)
func (m *Manager) GetProgressCounts() (downloaded, total int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	downloaded = 0
	for _, piece := range m.pieces {
		if piece.State == PieceStateVerified {
			downloaded++
		}
	}
	
	return downloaded, len(m.pieces)
}