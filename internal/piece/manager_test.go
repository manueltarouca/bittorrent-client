package piece

import (
	"testing"
	"time"
)

func TestNewPiece(t *testing.T) {
	hash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	piece := NewPiece(0, 32768, hash) // 2 blocks

	if piece.Index != 0 {
		t.Errorf("Expected index 0, got %d", piece.Index)
	}
	if piece.Length != 32768 {
		t.Errorf("Expected length 32768, got %d", piece.Length)
	}
	if len(piece.Blocks) != 2 {
		t.Errorf("Expected 2 blocks, got %d", len(piece.Blocks))
	}
	if piece.State != PieceStateMissing {
		t.Errorf("Expected state missing, got %v", piece.State)
	}
	if piece.Hash != hash {
		t.Errorf("Hash mismatch")
	}

	// Check block sizes
	if piece.Blocks[0].Length != BlockSize {
		t.Errorf("First block should be %d bytes, got %d", BlockSize, piece.Blocks[0].Length)
	}
	if piece.Blocks[1].Length != BlockSize {
		t.Errorf("Second block should be %d bytes, got %d", BlockSize, piece.Blocks[1].Length)
	}
}

func TestNewPieceWithOddSize(t *testing.T) {
	piece := NewPiece(0, 20000, [20]byte{}) // 1 full block + 1 partial block

	if len(piece.Blocks) != 2 {
		t.Errorf("Expected 2 blocks, got %d", len(piece.Blocks))
	}
	if piece.Blocks[0].Length != BlockSize {
		t.Errorf("First block should be %d bytes, got %d", BlockSize, piece.Blocks[0].Length)
	}
	if piece.Blocks[1].Length != 3616 { // 20000 - 16384
		t.Errorf("Second block should be 3616 bytes, got %d", piece.Blocks[1].Length)
	}
}

func TestPieceBlockOperations(t *testing.T) {
	piece := NewPiece(0, 32768, [20]byte{})

	// Initially not complete
	if piece.IsComplete() {
		t.Error("Piece should not be complete initially")
	}

	// All blocks should be missing
	missing := piece.GetMissingBlocks()
	if len(missing) != 2 {
		t.Errorf("Expected 2 missing blocks, got %d", len(missing))
	}

	// Add first block
	data1 := make([]byte, BlockSize)
	for i := range data1 {
		data1[i] = byte(i % 256)
	}

	err := piece.SetBlockData(0, data1)
	if err != nil {
		t.Errorf("Failed to set block data: %v", err)
	}

	// Should have 1 missing block now
	missing = piece.GetMissingBlocks()
	if len(missing) != 1 {
		t.Errorf("Expected 1 missing block, got %d", len(missing))
	}

	// Still not complete
	if piece.IsComplete() {
		t.Error("Piece should not be complete with only 1 block")
	}

	// Add second block
	data2 := make([]byte, BlockSize)
	for i := range data2 {
		data2[i] = byte((i + 100) % 256)
	}

	err = piece.SetBlockData(BlockSize, data2)
	if err != nil {
		t.Errorf("Failed to set second block data: %v", err)
	}

	// Should be complete now
	if !piece.IsComplete() {
		t.Error("Piece should be complete with all blocks")
	}

	// No missing blocks
	missing = piece.GetMissingBlocks()
	if len(missing) != 0 {
		t.Errorf("Expected 0 missing blocks, got %d", len(missing))
	}

	// Get complete data
	completeData, err := piece.GetData()
	if err != nil {
		t.Errorf("Failed to get complete data: %v", err)
	}
	if len(completeData) != 32768 {
		t.Errorf("Expected 32768 bytes, got %d", len(completeData))
	}
}

func TestPieceRequests(t *testing.T) {
	piece := NewPiece(0, 16384, [20]byte{})
	block := piece.Blocks[0]

	// Add a request
	piece.AddRequest("peer1", block)

	pending := piece.GetPendingBlocks()
	if len(pending) != 1 {
		t.Errorf("Expected 1 pending request, got %d", len(pending))
	}
	if pending[0].PeerID != "peer1" {
		t.Errorf("Expected peer1, got %s", pending[0].PeerID)
	}

	// Remove the request
	piece.RemoveRequest("peer1", block.Begin, block.Length)

	pending = piece.GetPendingBlocks()
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending requests, got %d", len(pending))
	}
}

func TestNewManager(t *testing.T) {
	hashes := make([][20]byte, 10)
	for i := range hashes {
		hashes[i] = [20]byte{byte(i)}
	}

	manager := NewManager(10, 16384, 8192, hashes)

	if len(manager.pieces) != 10 {
		t.Errorf("Expected 10 pieces, got %d", len(manager.pieces))
	}

	// Check bitfield size
	expectedBitfieldSize := (10 + 7) / 8 // 2 bytes
	if len(manager.bitfield) != expectedBitfieldSize {
		t.Errorf("Expected bitfield size %d, got %d", expectedBitfieldSize, len(manager.bitfield))
	}

	// Initially no pieces
	if manager.HasPiece(0) {
		t.Error("Should not have any pieces initially")
	}

	// Check last piece has correct size
	lastPiece := manager.GetPiece(9)
	if lastPiece.Length != 8192 {
		t.Errorf("Last piece should be 8192 bytes, got %d", lastPiece.Length)
	}
}

func TestManagerBitfield(t *testing.T) {
	manager := NewManager(16, 16384, 0, nil)

	// Initially empty bitfield
	bitfield := manager.GetBitfield()
	for _, b := range bitfield {
		if b != 0 {
			t.Error("Bitfield should be all zeros initially")
		}
	}

	// Mark piece as verified
	err := manager.MarkPieceVerified(0)
	if err != nil {
		t.Errorf("Failed to mark piece verified: %v", err)
	}

	if !manager.HasPiece(0) {
		t.Error("Should have piece 0 after marking verified")
	}

	// Check bitfield
	bitfield = manager.GetBitfield()
	if bitfield[0] != 0x80 { // First bit set
		t.Errorf("Expected first bit set, got %x", bitfield[0])
	}

	// Mark another piece
	err = manager.MarkPieceVerified(7)
	if err != nil {
		t.Errorf("Failed to mark piece 7 verified: %v", err)
	}

	bitfield = manager.GetBitfield()
	if bitfield[0] != 0x81 { // First and last bit of first byte
		t.Errorf("Expected 0x81, got %x", bitfield[0])
	}
}

func TestManagerBlockOperations(t *testing.T) {
	manager := NewManager(1, 32768, 0, nil) // 1 piece, 2 blocks

	// Get next blocks
	blocks := manager.GetNextBlocks(0, 5)
	if len(blocks) != 2 {
		t.Errorf("Expected 2 blocks, got %d", len(blocks))
	}

	// Add block data
	data := make([]byte, BlockSize)
	err := manager.AddBlockData(0, 0, data)
	if err != nil {
		t.Errorf("Failed to add block data: %v", err)
	}

	// Should have 1 block remaining
	blocks = manager.GetNextBlocks(0, 5)
	if len(blocks) != 1 {
		t.Errorf("Expected 1 block remaining, got %d", len(blocks))
	}

	// Add second block
	err = manager.AddBlockData(0, BlockSize, data)
	if err != nil {
		t.Errorf("Failed to add second block data: %v", err)
	}

	// Piece should be complete
	piece := manager.GetPiece(0)
	if piece.State != PieceStateDownloaded {
		t.Errorf("Expected piece state downloaded, got %v", piece.State)
	}
}

func TestManagerRequests(t *testing.T) {
	manager := NewManager(1, 16384, 0, nil)
	block := Block{Index: 0, Begin: 0, Length: 16384}

	// Add request
	manager.AddRequest(0, "peer1", block)

	stats := manager.GetStatistics()
	if stats.ActiveRequests != 1 {
		t.Errorf("Expected 1 active request, got %d", stats.ActiveRequests)
	}

	// Remove request
	manager.RemoveRequest(0, "peer1", 0, 16384)

	stats = manager.GetStatistics()
	if stats.ActiveRequests != 0 {
		t.Errorf("Expected 0 active requests, got %d", stats.ActiveRequests)
	}
}

func TestManagerProgress(t *testing.T) {
	manager := NewManager(4, 16384, 0, nil)

	// Initially 0% progress
	progress := manager.GetProgress()
	if progress != 0.0 {
		t.Errorf("Expected 0%% progress, got %f", progress)
	}

	if manager.IsComplete() {
		t.Error("Should not be complete initially")
	}

	// Mark 2 pieces verified
	manager.MarkPieceVerified(0)
	manager.MarkPieceVerified(1)

	progress = manager.GetProgress()
	if progress != 50.0 {
		t.Errorf("Expected 50%% progress, got %f", progress)
	}

	// Mark all pieces verified
	manager.MarkPieceVerified(2)
	manager.MarkPieceVerified(3)

	progress = manager.GetProgress()
	if progress != 100.0 {
		t.Errorf("Expected 100%% progress, got %f", progress)
	}

	if !manager.IsComplete() {
		t.Error("Should be complete with all pieces verified")
	}
}

func TestManagerMissingPieces(t *testing.T) {
	manager := NewManager(5, 16384, 0, nil)

	// All pieces should be missing initially
	missing := manager.GetMissingPieces()
	if len(missing) != 5 {
		t.Errorf("Expected 5 missing pieces, got %d", len(missing))
	}

	// Mark some pieces verified
	manager.MarkPieceVerified(1)
	manager.MarkPieceVerified(3)

	missing = manager.GetMissingPieces()
	expected := []int{0, 2, 4}
	if len(missing) != len(expected) {
		t.Errorf("Expected %d missing pieces, got %d", len(expected), len(missing))
	}

	for i, pieceIndex := range expected {
		if missing[i] != pieceIndex {
			t.Errorf("Expected missing piece %d, got %d", pieceIndex, missing[i])
		}
	}
}

func TestManagerTimeoutRequests(t *testing.T) {
	manager := NewManager(1, 16384, 0, nil)
	piece := manager.GetPiece(0)
	block := piece.Blocks[0]

	// Add a request with old timestamp
	piece.mu.Lock()
	piece.Requests["peer1-0-16384"] = Request{
		Block:     block,
		PeerID:    "peer1",
		Timestamp: time.Now().Add(-RequestTimeout - time.Second),
	}
	piece.mu.Unlock()

	// Should find timeout request
	timeouts := manager.GetTimeoutRequests()
	if len(timeouts) != 1 {
		t.Errorf("Expected 1 timeout request, got %d", len(timeouts))
	}
	if timeouts[0].PeerID != "peer1" {
		t.Errorf("Expected peer1, got %s", timeouts[0].PeerID)
	}
}

func TestManagerStatistics(t *testing.T) {
	manager := NewManager(2, 16384, 0, nil)

	// Add some data
	data := make([]byte, 16384)
	manager.AddBlockData(0, 0, data)

	stats := manager.GetStatistics()
	if stats.TotalPieces != 2 {
		t.Errorf("Expected 2 total pieces, got %d", stats.TotalPieces)
	}
	if stats.BytesDownloaded != 16384 {
		t.Errorf("Expected 16384 bytes downloaded, got %d", stats.BytesDownloaded)
	}

	// Mark piece verified
	manager.MarkPieceVerified(0)

	stats = manager.GetStatistics()
	if stats.VerifiedPieces != 1 {
		t.Errorf("Expected 1 verified piece, got %d", stats.VerifiedPieces)
	}
	if stats.BytesVerified != 16384 {
		t.Errorf("Expected 16384 bytes verified, got %d", stats.BytesVerified)
	}
}

func TestManagerGetPieceInfo(t *testing.T) {
	manager := NewManager(2, 32768, 0, nil) // 2 pieces, 2 blocks each

	info := manager.GetPieceInfo()
	if len(info) != 2 {
		t.Errorf("Expected 2 piece infos, got %d", len(info))
	}

	// Check first piece info
	if info[0].Index != 0 {
		t.Errorf("Expected index 0, got %d", info[0].Index)
	}
	if info[0].State != PieceStateMissing {
		t.Errorf("Expected state missing, got %v", info[0].State)
	}
	if info[0].BlocksTotal != 2 {
		t.Errorf("Expected 2 total blocks, got %d", info[0].BlocksTotal)
	}
	if info[0].BlocksMissing != 2 {
		t.Errorf("Expected 2 missing blocks, got %d", info[0].BlocksMissing)
	}
}

func TestPieceStateString(t *testing.T) {
	tests := []struct {
		state    PieceState
		expected string
	}{
		{PieceStateMissing, "missing"},
		{PieceStateRequested, "requested"},
		{PieceStateDownloaded, "downloaded"},
		{PieceStateVerified, "verified"},
		{PieceState(999), "unknown"},
	}

	for _, tt := range tests {
		result := tt.state.String()
		if result != tt.expected {
			t.Errorf("State %d: expected %s, got %s", int(tt.state), tt.expected, result)
		}
	}
}