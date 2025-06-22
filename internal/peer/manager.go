package peer

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mt/bittorrent-impl/internal/tracker"
)

const (
	// DefaultMaxPeers is the default maximum number of peer connections (increased for speed)
	DefaultMaxPeers = 50
	
	// DefaultMaxDownloadPeers is the default maximum number of download connections (increased for speed)
	DefaultMaxDownloadPeers = 20
	
	// ConnectionTimeout is the timeout for establishing connections
	ConnectionTimeout = 30 * time.Second
	
	// CleanupInterval is how often we clean up dead connections
	CleanupInterval = 1 * time.Minute
)

// Manager manages multiple peer connections
type Manager struct {
	mu              sync.RWMutex
	peers           map[string]*Peer
	infoHash        [20]byte
	peerID          [20]byte
	maxPeers        int
	maxDownloadPeers int
	bitfield        []byte
	ctx             context.Context
	cancel          context.CancelFunc
	
	// Channels
	incomingPeers   chan *Peer
	incomingMessages chan PeerMessage
	
	// Statistics
	stats PeerStats
	
	// Piece manager for piece operations
	pieceManager PieceManager
	
	// Piece handler for notifying about received pieces
	pieceHandler PieceHandler
}

// PieceManager interface for piece operations
type PieceManager interface {
	ReadBlockFromDisk(pieceIndex, begin, length int) ([]byte, error)
	AddBlockData(pieceIndex, begin int, data []byte) error
}

// PieceHandler interface for handling received pieces
type PieceHandler interface {
	HandlePieceReceived(pieceIndex, begin int)
}

// PeerMessage represents a message from a specific peer
type PeerMessage struct {
	Peer    *Peer
	Message *Message
}

// PeerStats contains statistics about peer connections
type PeerStats struct {
	mu               sync.RWMutex
	TotalConnected   int
	TotalDisconnected int
	ActivePeers      int
	DownloadingPeers int
	UploadingPeers   int
	BytesDownloaded  int64
	BytesUploaded    int64
}

// NewManager creates a new peer manager
func NewManager(infoHash, peerID [20]byte, numPieces int) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Initialize bitfield (all pieces missing initially)
	bitfieldSize := (numPieces + 7) / 8
	bitfield := make([]byte, bitfieldSize)
	
	return &Manager{
		peers:            make(map[string]*Peer),
		infoHash:         infoHash,
		peerID:           peerID,
		maxPeers:         DefaultMaxPeers,
		maxDownloadPeers: DefaultMaxDownloadPeers,
		bitfield:         bitfield,
		ctx:              ctx,
		cancel:           cancel,
		incomingPeers:    make(chan *Peer, 100),
		incomingMessages: make(chan PeerMessage, 1000),
	}
}

// Start begins the peer manager
func (m *Manager) Start() {
	go m.messageLoop()
	go m.cleanupLoop()
}

// Stop shuts down the peer manager and all connections
func (m *Manager) Stop() {
	m.cancel()
	
	m.mu.Lock()
	for _, peer := range m.peers {
		peer.Stop()
	}
	m.mu.Unlock()
	
	close(m.incomingPeers)
	close(m.incomingMessages)
}

// ConnectToPeers connects to a list of peers from tracker
func (m *Manager) ConnectToPeers(trackerPeers []tracker.Peer) {
	for _, trackerPeer := range trackerPeers {
		if m.GetActivePeerCount() >= m.maxPeers {
			break
		}
		
		go m.connectToPeer(trackerPeer)
	}
}

// connectToPeer connects to a single peer
func (m *Manager) connectToPeer(trackerPeer tracker.Peer) {
	addr := net.JoinHostPort(trackerPeer.IP.String(), fmt.Sprintf("%d", trackerPeer.Port))
	
	// Check if we're already connected to this peer
	if m.hasPeer(addr) {
		return
	}
	
	conn, err := net.DialTimeout("tcp", addr, ConnectionTimeout)
	if err != nil {
		return
	}
	
	peer := NewPeer(conn, m.infoHash, m.peerID)
	
	if err := peer.Start(); err != nil {
		peer.Stop()
		return
	}
	
	// Add to peer list
	if m.addPeer(peer) {
		go m.handlePeer(peer)
		
		// Send our bitfield if we have any pieces
		if m.hasPieces() {
			peer.SendBitfield(m.getBitfield())
		}
	} else {
		peer.Stop()
	}
}

// handlePeer handles messages from a specific peer
func (m *Manager) handlePeer(peer *Peer) {
	defer m.removePeer(peer)
	
	for {
		select {
		case <-peer.Done():
			return
		case <-m.ctx.Done():
			return
		default:
		}
		
		msg, err := peer.ReceiveMessage()
		if err != nil {
			return
		}
		
		// Forward message to main message loop
		select {
		case m.incomingMessages <- PeerMessage{Peer: peer, Message: msg}:
		case <-m.ctx.Done():
			return
		case <-peer.Done():
			return
		}
	}
}

// messageLoop processes incoming messages from all peers
func (m *Manager) messageLoop() {
	for {
		select {
		case peerMsg := <-m.incomingMessages:
			m.handlePeerMessage(peerMsg)
		case <-m.ctx.Done():
			return
		}
	}
}

// handlePeerMessage processes a message from a peer
func (m *Manager) handlePeerMessage(peerMsg PeerMessage) {
	peer := peerMsg.Peer
	msg := peerMsg.Message
	
	// Handle keep-alive (nil message)
	if msg == nil {
		return
	}
	
	switch msg.ID {
	case MsgRequest:
		index, begin, length, err := msg.ParseRequest()
		if err != nil {
			return
		}
		m.handlePieceRequest(peer, index, begin, length)
		
	case MsgPiece:
		index, begin, block, err := msg.ParsePiece()
		if err != nil {
			return
		}
		m.handlePieceData(peer, index, begin, block)
		
	case MsgCancel:
		index, begin, length, err := msg.ParseCancel()
		if err != nil {
			return
		}
		m.handleCancelRequest(peer, index, begin, length)
	}
}

// handlePieceRequest handles a piece request from a peer
func (m *Manager) handlePieceRequest(peer *Peer, index, begin, length uint32) {
	// Check if we have this piece
	if !m.hasPieceIndex(int(index)) {
		return
	}
	
	// Check if peer can upload
	if !peer.CanUpload() {
		return
	}
	
	// Read block data from disk through piece manager
	m.mu.RLock()
	pieceManager := m.pieceManager
	m.mu.RUnlock()
	
	if pieceManager != nil {
		blockData, err := pieceManager.ReadBlockFromDisk(int(index), int(begin), int(length))
		if err == nil {
			// Send the block data to the peer
			msg := NewPieceMessage(index, begin, blockData)
			peer.SendMessage(msg)
			
			// Update upload statistics
			m.stats.mu.Lock()
			m.stats.BytesUploaded += int64(len(blockData))
			m.stats.mu.Unlock()
		}
	}
}

// handlePieceData handles piece data from a peer
func (m *Manager) handlePieceData(peer *Peer, index, begin uint32, block []byte) {
	// Update statistics
	m.stats.mu.Lock()
	m.stats.BytesDownloaded += int64(len(block))
	m.stats.mu.Unlock()
	
	// Store the block data through piece manager
	m.mu.RLock()
	pieceManager := m.pieceManager
	pieceHandler := m.pieceHandler
	m.mu.RUnlock()
	
	if pieceManager != nil {
		// Add the block data to the piece manager
		// The piece manager will handle verification and disk storage
		err := pieceManager.AddBlockData(int(index), int(begin), block)
		if err != nil {
			// Log error or handle appropriately
			// For now, we just ignore the error
			_ = err
		}
		
		// Notify the piece handler about received piece
		if pieceHandler != nil {
			pieceHandler.HandlePieceReceived(int(index), int(begin))
		}
	}
}

// handleCancelRequest handles a cancel request from a peer
func (m *Manager) handleCancelRequest(peer *Peer, index, begin, length uint32) {
	// TODO: Cancel any pending piece sending
}

// cleanupLoop periodically cleans up dead connections
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.ctx.Done():
			return
		}
	}
}

// cleanup removes dead peer connections
func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for addr, peer := range m.peers {
		if !peer.IsConnected() {
			delete(m.peers, addr)
			m.stats.mu.Lock()
			m.stats.ActivePeers--
			m.stats.TotalDisconnected++
			m.stats.mu.Unlock()
		}
	}
}

// addPeer adds a peer to the manager
func (m *Manager) addPeer(peer *Peer) bool {
	addr := peer.Address().String()
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Check limits
	if len(m.peers) >= m.maxPeers {
		return false
	}
	
	// Check if already exists
	if _, exists := m.peers[addr]; exists {
		return false
	}
	
	m.peers[addr] = peer
	
	// Update statistics
	m.stats.mu.Lock()
	m.stats.ActivePeers++
	m.stats.TotalConnected++
	m.stats.mu.Unlock()
	
	return true
}

// removePeer removes a peer from the manager
func (m *Manager) removePeer(peer *Peer) {
	addr := peer.Address().String()
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, exists := m.peers[addr]; exists {
		delete(m.peers, addr)
		
		m.stats.mu.Lock()
		m.stats.ActivePeers--
		m.stats.TotalDisconnected++
		m.stats.mu.Unlock()
	}
}

// hasPeer checks if we're connected to a peer at the given address
func (m *Manager) hasPeer(addr string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	_, exists := m.peers[addr]
	return exists
}

// GetActivePeerCount returns the number of active peer connections
func (m *Manager) GetActivePeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// GetPeers returns a list of all connected peers
func (m *Manager) GetPeers() []*Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	peers := make([]*Peer, 0, len(m.peers))
	for _, peer := range m.peers {
		peers = append(peers, peer)
	}
	
	return peers
}

// GetDownloadingPeers returns peers we can download from
func (m *Manager) GetDownloadingPeers() []*Peer {
	peers := m.GetPeers()
	downloading := make([]*Peer, 0, len(peers))
	
	for _, peer := range peers {
		if peer.CanDownload() {
			downloading = append(downloading, peer)
		}
	}
	
	return downloading
}

// GetUploadingPeers returns peers we can upload to
func (m *Manager) GetUploadingPeers() []*Peer {
	peers := m.GetPeers()
	uploading := make([]*Peer, 0, len(peers))
	
	for _, peer := range peers {
		if peer.CanUpload() {
			uploading = append(uploading, peer)
		}
	}
	
	return uploading
}

// RequestPieceFromPeers requests a piece from available peers
func (m *Manager) RequestPieceFromPeers(index int, begin, length uint32) error {
	downloadingPeers := m.GetDownloadingPeers()
	
	for _, peer := range downloadingPeers {
		if peer.HasPiece(index) {
			return peer.RequestPiece(uint32(index), begin, length)
		}
	}
	
	return fmt.Errorf("no peers have piece %d", index)
}

// BroadcastHave broadcasts that we have a piece to all peers
func (m *Manager) BroadcastHave(index int) {
	peers := m.GetPeers()
	msg := NewHaveMessage(uint32(index))
	
	for _, peer := range peers {
		peer.SendMessage(msg)
	}
	
	// Update our bitfield
	m.setPiece(index)
}

// setPiece marks a piece as completed in our bitfield
func (m *Manager) setPiece(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	byteIndex := index / 8
	bitIndex := index % 8
	
	if byteIndex < len(m.bitfield) {
		m.bitfield[byteIndex] |= (1 << (7 - bitIndex))
	}
}

// hasPieceIndex checks if we have a specific piece
func (m *Manager) hasPieceIndex(index int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	byteIndex := index / 8
	bitIndex := index % 8
	
	if byteIndex >= len(m.bitfield) {
		return false
	}
	
	return (m.bitfield[byteIndex] & (1 << (7 - bitIndex))) != 0
}

// hasPieces checks if we have any pieces
func (m *Manager) hasPieces() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for _, b := range m.bitfield {
		if b != 0 {
			return true
		}
	}
	
	return false
}

// getBitfield returns a copy of our bitfield
func (m *Manager) getBitfield() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	bitfield := make([]byte, len(m.bitfield))
	copy(bitfield, m.bitfield)
	return bitfield
}

// GetStats returns current peer statistics
func (m *Manager) GetStats() PeerStats {
	m.stats.mu.RLock()
	defer m.stats.mu.RUnlock()
	
	// Update active counts
	downloadingPeers := len(m.GetDownloadingPeers())
	uploadingPeers := len(m.GetUploadingPeers())
	
	// Create a copy without the mutex
	return PeerStats{
		TotalConnected:   m.stats.TotalConnected,
		TotalDisconnected: m.stats.TotalDisconnected,
		ActivePeers:      m.stats.ActivePeers,
		DownloadingPeers: downloadingPeers,
		UploadingPeers:   uploadingPeers,
		BytesDownloaded:  m.stats.BytesDownloaded,
		BytesUploaded:    m.stats.BytesUploaded,
	}
}

// SetMaxPeers sets the maximum number of peer connections
func (m *Manager) SetMaxPeers(max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxPeers = max
}

// SetMaxDownloadPeers sets the maximum number of download connections
func (m *Manager) SetMaxDownloadPeers(max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxDownloadPeers = max
}

// SetPieceManager sets the piece manager for piece operations
func (m *Manager) SetPieceManager(pieceManager PieceManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pieceManager = pieceManager
}

// SetPieceHandler sets the piece handler for piece notifications
func (m *Manager) SetPieceHandler(pieceHandler PieceHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pieceHandler = pieceHandler
}

// FindPeersWithPiece returns peers that have a specific piece
func (m *Manager) FindPeersWithPiece(index int) []*Peer {
	peers := m.GetPeers()
	result := make([]*Peer, 0)
	
	for _, peer := range peers {
		if peer.HasPiece(index) {
			result = append(result, peer)
		}
	}
	
	return result
}

// GetPeerInfo returns information about all connected peers
func (m *Manager) GetPeerInfo() []PeerInfo {
	peers := m.GetPeers()
	info := make([]PeerInfo, len(peers))
	
	for i, peer := range peers {
		state := peer.GetState()
		info[i] = PeerInfo{
			Address:        peer.Address().String(),
			PeerID:         peer.RemotePeerID(),
			State:          state,
			LastSeen:       peer.LastSeen(),
			Extensions:     peer.GetExtensions(),
			IsConnected:    peer.IsConnected(),
			CanDownload:    peer.CanDownload(),
			CanUpload:      peer.CanUpload(),
		}
	}
	
	return info
}

// PeerInfo contains information about a peer
type PeerInfo struct {
	Address     string
	PeerID      [20]byte
	State       PeerState
	LastSeen    time.Time
	Extensions  Extensions
	IsConnected bool
	CanDownload bool
	CanUpload   bool
}

// GetConnectedPeers returns a list of all connected peers (alias for GetPeers)
func (m *Manager) GetConnectedPeers() []*Peer {
	return m.GetPeers()
}