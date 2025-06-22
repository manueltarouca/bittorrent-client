package download

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mt/bittorrent-impl/internal/peer"
	"github.com/mt/bittorrent-impl/internal/piece"
)

// PeerManager interface for interacting with peers  
type PeerManager interface {
	GetConnectedPeers() []*peer.Peer
}

// PieceManager interface for piece management
type PieceManager interface {
	GetNeededPieces() []int
	SelectPieceForPeer(peerBitfield []byte) (int, error)
	GetBlockRequests(pieceIndex int) []piece.BlockRequest
	RequestBlock(pieceIndex, begin, length int) error
	GetActiveRequests() map[string]time.Time
	GetProgressCounts() (downloaded, total int)
}

// RequestInfo tracks active requests to peers
type RequestInfo struct {
	PieceIndex int
	Begin      int
	Length     int
	RequestedAt time.Time
	Peer       *peer.Peer
}

// Coordinator manages the download process by coordinating between peers and pieces
type Coordinator struct {
	mu           sync.RWMutex
	peerManager  PeerManager
	pieceManager PieceManager
	
	// Request tracking
	activeRequests map[string]*RequestInfo // key: "pieceIndex:begin"
	maxRequestsPerPeer int
	requestTimeout time.Duration
	
	// Statistics
	downloadedPieces int
	totalPieces     int
	
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewCoordinator creates a new download coordinator
func NewCoordinator(peerManager PeerManager, pieceManager PieceManager) *Coordinator {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Coordinator{
		peerManager:        peerManager,
		pieceManager:       pieceManager,
		activeRequests:     make(map[string]*RequestInfo),
		maxRequestsPerPeer: 10, // Maximum concurrent requests per peer (increased for speed)
		requestTimeout:     15 * time.Second, // Faster timeout for unresponsive peers
		ctx:                ctx,
		cancel:             cancel,
	}
}

// Start begins the download coordination process
func (c *Coordinator) Start() {
	c.wg.Add(2)
	go c.coordinationLoop()
	go c.timeoutLoop()
}

// Stop stops the download coordinator
func (c *Coordinator) Stop() {
	c.cancel()
	c.wg.Wait()
}

// coordinationLoop is the main coordination loop
func (c *Coordinator) coordinationLoop() {
	defer c.wg.Done()
	
	log.Printf("Download coordinator started")
	ticker := time.NewTicker(500 * time.Millisecond) // Ultra-responsive coordination for max speed
	defer ticker.Stop()
	
	for {
		select {
		case <-c.ctx.Done():
			log.Printf("Download coordinator stopped")
			return
		case <-ticker.C:
			c.processDownloadCycle()
		}
	}
}

// processDownloadCycle performs one cycle of download coordination
func (c *Coordinator) processDownloadCycle() {
	// Get connected peers
	peers := c.peerManager.GetConnectedPeers()
	if len(peers) == 0 {
		log.Printf("No connected peers")
		return
	}
	
	// Get needed pieces
	neededPieces := c.pieceManager.GetNeededPieces()
	if len(neededPieces) == 0 {
		log.Printf("Download complete - no needed pieces")
		return // Download complete
	}
	
	// Reduced logging for performance - only log every 10th cycle
	c.mu.Lock()
	cycleCount := c.downloadedPieces // Use as cycle counter
	c.mu.Unlock()
	
	if cycleCount%10 == 0 {
		log.Printf("Coordination cycle: %d peers, %d needed pieces", len(peers), len(neededPieces))
	}
	
	// Process more pieces for higher throughput
	if len(neededPieces) > 500 {
		neededPieces = neededPieces[:500] // Process first 500 pieces at a time for max speed
	}
	
	// Update interest states for all peers
	for _, p := range peers {
		if err := p.EnsureInterested(neededPieces); err != nil {
			log.Printf("Failed to update interest for peer %s: %v", p.Address(), err)
		}
	}
	
	// Request pieces from ALL peers that can provide them (parallel downloads)
	downloadablePeers := 0
	for _, p := range peers {
		if p.CanDownload() {
			downloadablePeers++
			c.requestPiecesFromPeer(p, neededPieces)
		}
	}
	
	if downloadablePeers > 0 {
		log.Printf("Requesting from %d downloadable peers", downloadablePeers)
	}
	
	// Update progress
	c.updateProgress()
}

// requestPiecesFromPeer requests pieces from a specific peer
func (c *Coordinator) requestPiecesFromPeer(p *peer.Peer, neededPieces []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Count active requests for this peer
	activeCount := c.countActiveRequestsForPeer(p)
	if activeCount >= c.maxRequestsPerPeer {
		return // Already at request limit for this peer
	}
	
	// Check if peer has a bitfield
	bitfield := p.GetBitfield()
	if bitfield == nil {
		log.Printf("Peer %s has no bitfield yet", p.Address())
		return
	}
	
	// Find pieces this peer has that we need
	availablePieces := make([]int, 0)
	for _, pieceIndex := range neededPieces {
		if p.HasPiece(pieceIndex) {
			availablePieces = append(availablePieces, pieceIndex)
		}
	}
	
	if len(availablePieces) == 0 {
		log.Printf("Peer %s has bitfield but no pieces we need (checked %d pieces)", p.Address(), len(neededPieces))
		return // Peer has no pieces we need
	}
	
	log.Printf("Peer %s has %d pieces we need", p.Address(), len(availablePieces))
	
	// Select a piece using the piece manager's strategy
	pieceIndex, err := c.pieceManager.SelectPieceForPeer(bitfield)
	if err != nil {
		return // No piece selected
	}
	
	// Get block requests for this piece
	blockRequests := c.pieceManager.GetBlockRequests(pieceIndex)
	
	// Request blocks aggressively until we hit the limit
	requestsToMake := c.maxRequestsPerPeer - activeCount
	requestsMade := 0
	for _, blockReq := range blockRequests {
		if requestsMade >= requestsToMake {
			break
		}
		
		// Check if this block is already requested
		requestKey := fmt.Sprintf("%d:%d", pieceIndex, blockReq.Begin)
		if _, exists := c.activeRequests[requestKey]; exists {
			continue
		}
		
		// Send the request
		if err := p.RequestPiece(uint32(pieceIndex), uint32(blockReq.Begin), uint32(blockReq.Length)); err != nil {
			log.Printf("Failed to request piece %d block %d from peer %s: %v", pieceIndex, blockReq.Begin, p.Address(), err)
			continue
		}
		
		// Track the request
		c.activeRequests[requestKey] = &RequestInfo{
			PieceIndex:  pieceIndex,
			Begin:       blockReq.Begin,
			Length:      blockReq.Length,
			RequestedAt: time.Now(),
			Peer:        p,
		}
		
		// Also track in piece manager
		if err := c.pieceManager.RequestBlock(pieceIndex, blockReq.Begin, blockReq.Length); err != nil {
			log.Printf("Failed to mark block as requested in piece manager: %v", err)
		}
		
		log.Printf("Requested block %d:%d (length %d) from peer %s", 
			pieceIndex, blockReq.Begin, blockReq.Length, p.Address())
		requestsMade++
	}
}

// countActiveRequestsForPeer counts active requests for a specific peer
func (c *Coordinator) countActiveRequestsForPeer(targetPeer *peer.Peer) int {
	count := 0
	for _, req := range c.activeRequests {
		if req.Peer == targetPeer {
			count++
		}
	}
	return count
}

// timeoutLoop handles request timeouts
func (c *Coordinator) timeoutLoop() {
	defer c.wg.Done()
	
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.cleanupTimedOutRequests()
		}
	}
}

// cleanupTimedOutRequests removes timed out requests
func (c *Coordinator) cleanupTimedOutRequests() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	for key, req := range c.activeRequests {
		if now.Sub(req.RequestedAt) > c.requestTimeout {
			log.Printf("Request timeout for block %d:%d from peer %s", 
				req.PieceIndex, req.Begin, req.Peer.Address())
			delete(c.activeRequests, key)
		}
	}
}

// HandlePieceReceived should be called when a piece block is received
func (c *Coordinator) HandlePieceReceived(pieceIndex, begin int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	requestKey := fmt.Sprintf("%d:%d", pieceIndex, begin)
	if req, exists := c.activeRequests[requestKey]; exists {
		duration := time.Since(req.RequestedAt)
		log.Printf("Received block %d:%d (length %d) from peer %s in %v", 
			pieceIndex, begin, req.Length, req.Peer.Address(), duration.Round(time.Millisecond))
		delete(c.activeRequests, requestKey)
	} else {
		log.Printf("Warning: Received unrequested block %d:%d", pieceIndex, begin)
	}
}

// updateProgress updates download statistics
func (c *Coordinator) updateProgress() {
	downloaded, total := c.pieceManager.GetProgressCounts()
	c.mu.Lock()
	c.downloadedPieces = downloaded
	c.totalPieces = total
	c.mu.Unlock()
}

// GetProgress returns current download progress
func (c *Coordinator) GetProgress() (downloaded, total int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.downloadedPieces, c.totalPieces
}

// GetActiveRequestCount returns the number of active requests
func (c *Coordinator) GetActiveRequestCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.activeRequests)
}

// IsDownloadComplete returns true if all pieces are downloaded
func (c *Coordinator) IsDownloadComplete() bool {
	downloaded, total := c.GetProgress()
	return downloaded == total && total > 0
}