package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mt/bittorrent-impl/internal/bencode"
)

// Peer represents a peer in the swarm
type Peer struct {
	IP   net.IP
	Port uint16
	ID   []byte
}

// TrackerResponse contains the response from a tracker
type TrackerResponse struct {
	Interval int
	Peers    []Peer
	Complete int
	Incomplete int
}

// AnnounceParams contains parameters for tracker announce
type AnnounceParams struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      string // "started", "stopped", "completed", or ""
	Compact    bool
}

// Client handles communication with trackers
type Client struct {
	httpClient *http.Client
	userAgent  string
}

// NewClient creates a new tracker client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent: "SimpleBittorrent/1.0",
	}
}

// Announce sends an announce request to the tracker
func (c *Client) Announce(announceURL string, params AnnounceParams) (*TrackerResponse, error) {
	// Build the request URL
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, fmt.Errorf("invalid announce URL: %w", err)
	}

	q := u.Query()
	q.Set("info_hash", string(params.InfoHash[:]))
	q.Set("peer_id", string(params.PeerID[:]))
	q.Set("port", strconv.Itoa(int(params.Port)))
	q.Set("uploaded", strconv.FormatInt(params.Uploaded, 10))
	q.Set("downloaded", strconv.FormatInt(params.Downloaded, 10))
	q.Set("left", strconv.FormatInt(params.Left, 10))
	
	if params.Event != "" {
		q.Set("event", params.Event)
	}
	
	// Request compact format
	if params.Compact {
		q.Set("compact", "1")
	}
	
	u.RawQuery = q.Encode()

	// Create the request
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("User-Agent", c.userAgent)

	// Send the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for HTTP error
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tracker returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	return c.parseResponse(body)
}

// parseResponse parses the bencode response from the tracker
func (c *Client) parseResponse(data []byte) (*TrackerResponse, error) {
	var resp map[string]interface{}
	if err := bencode.Decode(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for failure reason
	if reason, ok := resp["failure reason"].(string); ok {
		return nil, fmt.Errorf("tracker error: %s", reason)
	}

	response := &TrackerResponse{}

	// Extract interval
	if interval, ok := resp["interval"].(int64); ok {
		response.Interval = int(interval)
	}

	// Extract complete count
	if complete, ok := resp["complete"].(int64); ok {
		response.Complete = int(complete)
	}

	// Extract incomplete count  
	if incomplete, ok := resp["incomplete"].(int64); ok {
		response.Incomplete = int(incomplete)
	}

	// Extract peers
	if peersData, ok := resp["peers"]; ok {
		switch v := peersData.(type) {
		case string:
			// Compact format
			response.Peers = parseCompactPeers([]byte(v))
		case []interface{}:
			// Dictionary format
			response.Peers = parseDictPeers(v)
		default:
			return nil, errors.New("invalid peers format")
		}
	}

	return response, nil
}

// parseCompactPeers parses peers in compact format (6 bytes per peer)
func parseCompactPeers(data []byte) []Peer {
	if len(data)%6 != 0 {
		return nil
	}

	numPeers := len(data) / 6
	peers := make([]Peer, 0, numPeers)

	for i := 0; i < numPeers; i++ {
		offset := i * 6
		ip := net.IP(data[offset : offset+4])
		port := binary.BigEndian.Uint16(data[offset+4 : offset+6])
		
		peers = append(peers, Peer{
			IP:   ip,
			Port: port,
		})
	}

	return peers
}

// parseDictPeers parses peers in dictionary format
func parseDictPeers(peersData []interface{}) []Peer {
	peers := make([]Peer, 0, len(peersData))

	for _, peerData := range peersData {
		peerDict, ok := peerData.(map[string]interface{})
		if !ok {
			continue
		}

		var peer Peer

		// Extract peer ID if available
		if peerID, ok := peerDict["peer id"].(string); ok {
			peer.ID = []byte(peerID)
		}

		// Extract IP
		if ip, ok := peerDict["ip"].(string); ok {
			peer.IP = net.ParseIP(ip)
			if peer.IP == nil {
				continue
			}
		} else {
			continue
		}

		// Extract port
		if port, ok := peerDict["port"].(int64); ok {
			peer.Port = uint16(port)
		} else {
			continue
		}

		peers = append(peers, peer)
	}

	return peers
}

// String returns a string representation of a peer
func (p Peer) String() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

// GeneratePeerID generates a peer ID for our client
func GeneratePeerID() [20]byte {
	var peerID [20]byte
	
	// Use Azureus-style peer ID: -SB0100- followed by 12 random bytes
	copy(peerID[:], "-SB0100-")
	
	// Generate random bytes for the rest
	timestamp := time.Now().UnixNano()
	for i := 8; i < 20; i++ {
		peerID[i] = byte(timestamp >> (8 * (i - 8)))
	}
	
	return peerID
}

// CompactPeersToBytes converts a slice of peers to compact format
func CompactPeersToBytes(peers []Peer) []byte {
	buf := bytes.NewBuffer(nil)
	
	for _, peer := range peers {
		// Write IP (4 bytes)
		ip := peer.IP.To4()
		if ip == nil {
			continue // Skip IPv6 for now
		}
		buf.Write(ip)
		
		// Write port (2 bytes, big endian)
		binary.Write(buf, binary.BigEndian, peer.Port)
	}
	
	return buf.Bytes()
}