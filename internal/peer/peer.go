package peer

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// PeerState represents the state of a peer connection
type PeerState struct {
	AmChoking      bool // Are we choking this peer?
	AmInterested   bool // Are we interested in this peer?
	PeerChoking    bool // Is this peer choking us?
	PeerInterested bool // Is this peer interested in us?
}

// NewPeerState creates a new peer state with default values
func NewPeerState() *PeerState {
	return &PeerState{
		AmChoking:      true,  // Start choking
		AmInterested:   false, // Start not interested
		PeerChoking:    true,  // Assume peer is choking us
		PeerInterested: false, // Assume peer is not interested
	}
}

// Peer represents a peer connection
type Peer struct {
	mu           sync.RWMutex
	conn         net.Conn
	infoHash     [20]byte
	peerID       [20]byte
	remotePeerID [20]byte
	state        *PeerState
	bitfield     []byte
	sendCh       chan *Message
	receiveCh    chan *Message
	doneCh       chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	extensions   Extensions
	lastSeen     time.Time
}

// NewPeer creates a new peer connection
func NewPeer(conn net.Conn, infoHash, peerID [20]byte) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Peer{
		conn:      conn,
		infoHash:  infoHash,
		peerID:    peerID,
		state:     NewPeerState(),
		sendCh:    make(chan *Message, 100),
		receiveCh: make(chan *Message, 100),
		doneCh:    make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
		lastSeen:  time.Now(),
	}
}

// Start begins the peer communication loops
func (p *Peer) Start() error {
	// Perform handshake
	handshake, err := DoHandshake(p.conn, p.infoHash, p.peerID)
	if err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}
	
	p.mu.Lock()
	p.remotePeerID = handshake.PeerID
	p.extensions = handshake.ParseExtensions()
	p.mu.Unlock()
	
	// Start send and receive loops
	go p.sendLoop()
	go p.receiveLoop()
	
	return nil
}

// Stop closes the peer connection and stops all loops
func (p *Peer) Stop() {
	p.cancel()
	p.conn.Close()
	close(p.doneCh)
}

// SendMessage sends a message to the peer
func (p *Peer) SendMessage(msg *Message) error {
	select {
	case p.sendCh <- msg:
		return nil
	case <-p.ctx.Done():
		return fmt.Errorf("peer connection closed")
	case <-time.After(5 * time.Second):
		return fmt.Errorf("send channel full")
	}
}

// ReceiveMessage receives a message from the peer
func (p *Peer) ReceiveMessage() (*Message, error) {
	select {
	case msg := <-p.receiveCh:
		return msg, nil
	case <-p.ctx.Done():
		return nil, fmt.Errorf("peer connection closed")
	}
}

// GetState returns a copy of the peer state
func (p *Peer) GetState() PeerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return *p.state
}

// GetBitfield returns a copy of the peer's bitfield
func (p *Peer) GetBitfield() []byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if p.bitfield == nil {
		return nil
	}
	
	bitfield := make([]byte, len(p.bitfield))
	copy(bitfield, p.bitfield)
	return bitfield
}

// HasPiece checks if the peer has a specific piece
func (p *Peer) HasPiece(index int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if p.bitfield == nil {
		return false
	}
	
	byteIndex := index / 8
	bitIndex := index % 8
	
	if byteIndex >= len(p.bitfield) {
		return false
	}
	
	return (p.bitfield[byteIndex] & (1 << (7 - bitIndex))) != 0
}

// SetPiece marks a piece as available from this peer
func (p *Peer) SetPiece(index int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.bitfield == nil {
		return
	}
	
	byteIndex := index / 8
	bitIndex := index % 8
	
	if byteIndex >= len(p.bitfield) {
		return
	}
	
	p.bitfield[byteIndex] |= (1 << (7 - bitIndex))
}

// String returns a string representation of the peer
func (p *Peer) String() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return fmt.Sprintf("Peer{%s, ID: %x}", p.conn.RemoteAddr(), p.remotePeerID[:8])
}

// Address returns the peer's network address
func (p *Peer) Address() net.Addr {
	return p.conn.RemoteAddr()
}

// IsConnected returns true if the peer is still connected
func (p *Peer) IsConnected() bool {
	select {
	case <-p.ctx.Done():
		return false
	default:
		return true
	}
}

// LastSeen returns the time when we last received a message from this peer
func (p *Peer) LastSeen() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastSeen
}

// sendLoop handles sending messages to the peer
func (p *Peer) sendLoop() {
	defer p.cancel()
	
	keepAliveTicker := time.NewTicker(2 * time.Minute)
	defer keepAliveTicker.Stop()
	
	for {
		select {
		case msg := <-p.sendCh:
			if err := WriteMessage(p.conn, msg); err != nil {
				return
			}
			
		case <-keepAliveTicker.C:
			// Send keep-alive message
			if err := WriteMessage(p.conn, KeepAlive()); err != nil {
				return
			}
			
		case <-p.ctx.Done():
			return
		}
	}
}

// receiveLoop handles receiving messages from the peer
func (p *Peer) receiveLoop() {
	defer p.cancel()
	
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		
		// Set read timeout
		p.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		
		msg, err := ReadMessage(p.conn)
		if err != nil {
			return
		}
		
		p.mu.Lock()
		p.lastSeen = time.Now()
		p.mu.Unlock()
		
		// Handle the message
		if err := p.handleMessage(msg); err != nil {
			return
		}
		
		// Forward to receive channel if not a control message
		if msg != nil && !p.isControlMessage(msg) {
			select {
			case p.receiveCh <- msg:
			case <-p.ctx.Done():
				return
			default:
				// Channel full, drop message
			}
		}
	}
}

// handleMessage processes incoming messages and updates peer state
func (p *Peer) handleMessage(msg *Message) error {
	if msg == nil {
		// Keep-alive message, nothing to do
		return nil
	}
	
	p.mu.Lock()
	defer p.mu.Unlock()
	
	switch msg.ID {
	case MsgChoke:
		p.state.PeerChoking = true
		
	case MsgUnchoke:
		p.state.PeerChoking = false
		
	case MsgInterested:
		p.state.PeerInterested = true
		
	case MsgNotInterested:
		p.state.PeerInterested = false
		
	case MsgHave:
		index, err := msg.ParseHave()
		if err != nil {
			return err
		}
		p.setPieceUnsafe(int(index))
		
	case MsgBitfield:
		bitfield, err := msg.ParseBitfield()
		if err != nil {
			return err
		}
		p.bitfield = bitfield
	}
	
	return nil
}

// setPieceUnsafe marks a piece as available (must hold lock)
func (p *Peer) setPieceUnsafe(index int) {
	if p.bitfield == nil {
		return
	}
	
	byteIndex := index / 8
	bitIndex := index % 8
	
	if byteIndex >= len(p.bitfield) {
		return
	}
	
	p.bitfield[byteIndex] |= (1 << (7 - bitIndex))
}

// isControlMessage returns true for messages that update peer state
func (p *Peer) isControlMessage(msg *Message) bool {
	switch msg.ID {
	case MsgChoke, MsgUnchoke, MsgInterested, MsgNotInterested, MsgHave, MsgBitfield:
		return true
	default:
		return false
	}
}

// Choke sends a choke message to the peer
func (p *Peer) Choke() error {
	p.mu.Lock()
	p.state.AmChoking = true
	p.mu.Unlock()
	
	return p.SendMessage(NewChokeMessage())
}

// Unchoke sends an unchoke message to the peer
func (p *Peer) Unchoke() error {
	p.mu.Lock()
	p.state.AmChoking = false
	p.mu.Unlock()
	
	return p.SendMessage(NewUnchokeMessage())
}

// Interested sends an interested message to the peer
func (p *Peer) Interested() error {
	p.mu.Lock()
	p.state.AmInterested = true
	p.mu.Unlock()
	
	return p.SendMessage(NewInterestedMessage())
}

// NotInterested sends a not interested message to the peer
func (p *Peer) NotInterested() error {
	p.mu.Lock()
	p.state.AmInterested = false
	p.mu.Unlock()
	
	return p.SendMessage(NewNotInterestedMessage())
}

// RequestPiece sends a request for a piece block
func (p *Peer) RequestPiece(index, begin, length uint32) error {
	state := p.GetState()
	if state.PeerChoking {
		return fmt.Errorf("peer is choking us")
	}
	
	return p.SendMessage(NewRequestMessage(index, begin, length))
}

// SendPiece sends a piece block to the peer
func (p *Peer) SendPiece(index, begin uint32, data []byte) error {
	state := p.GetState()
	if state.AmChoking {
		return fmt.Errorf("we are choking peer")
	}
	
	return p.SendMessage(NewPieceMessage(index, begin, data))
}

// Cancel sends a cancel message for a piece block
func (p *Peer) Cancel(index, begin, length uint32) error {
	return p.SendMessage(NewCancelMessage(index, begin, length))
}

// SendBitfield sends our bitfield to the peer
func (p *Peer) SendBitfield(bitfield []byte) error {
	return p.SendMessage(NewBitfieldMessage(bitfield))
}

// CanDownload returns true if we can download from this peer
func (p *Peer) CanDownload() bool {
	state := p.GetState()
	return !state.PeerChoking && state.AmInterested
}

// CanUpload returns true if we can upload to this peer
func (p *Peer) CanUpload() bool {
	state := p.GetState()
	return !state.AmChoking && state.PeerInterested
}

// GetExtensions returns the peer's supported extensions
func (p *Peer) GetExtensions() Extensions {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.extensions
}

// RemotePeerID returns the remote peer's ID
func (p *Peer) RemotePeerID() [20]byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.remotePeerID
}

// Done returns a channel that's closed when the peer disconnects
func (p *Peer) Done() <-chan struct{} {
	return p.ctx.Done()
}

// NeedsPieces checks if we should be interested in this peer based on available pieces
func (p *Peer) NeedsPieces(neededPieces []int) bool {
	if p.bitfield == nil {
		return false
	}
	
	for _, pieceIndex := range neededPieces {
		if p.HasPiece(pieceIndex) {
			return true
		}
	}
	return false
}

// EnsureInterested ensures we express interest if peer has pieces we need
func (p *Peer) EnsureInterested(neededPieces []int) error {
	shouldBeInterested := p.NeedsPieces(neededPieces)
	state := p.GetState()
	
	if shouldBeInterested && !state.AmInterested {
		return p.Interested()
	} else if !shouldBeInterested && state.AmInterested {
		return p.NotInterested()
	}
	
	return nil
}