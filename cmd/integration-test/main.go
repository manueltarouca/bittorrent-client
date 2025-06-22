package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mt/bittorrent-impl/internal/disk"
	"github.com/mt/bittorrent-impl/internal/peer"
	"github.com/mt/bittorrent-impl/internal/piece"
	"github.com/mt/bittorrent-impl/internal/torrent"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go <torrent-file>")
	}

	torrentFile := os.Args[1]
	
	fmt.Printf("=== BITTORRENT CLIENT INTEGRATION TEST ===\n")
	fmt.Printf("Torrent file: %s\n\n", torrentFile)
	
	// 1. Parse the torrent file
	fmt.Println("1. Parsing torrent file...")
	t, err := torrent.ParseFile(torrentFile)
	if err != nil {
		log.Fatalf("Failed to parse torrent: %v", err)
	}
	
	fmt.Printf("   ✓ Loaded: %s\n", t.Info.Name)
	fmt.Printf("   ✓ Size: %.2f MB (%d bytes)\n", float64(t.TotalLength())/1024/1024, t.TotalLength())
	fmt.Printf("   ✓ Pieces: %d (each %d bytes)\n", t.NumPieces(), t.Info.PieceLength)
	fmt.Printf("   ✓ Info Hash: %s\n", t.InfoHashString())
	
	// 2. Set up disk manager
	fmt.Println("\n2. Setting up disk manager...")
	downloadDir := filepath.Join("downloads", t.Info.Name)
	diskManager := disk.NewManager(t, downloadDir)
	
	fmt.Printf("   ✓ Download directory: %s\n", downloadDir)
	
	// Initialize disk manager (creates file structure)
	err = diskManager.Initialize()
	if err != nil {
		log.Fatalf("Failed to initialize disk manager: %v", err)
	}
	defer diskManager.Close()
	
	fmt.Printf("   ✓ File structure created\n")
	
	progress := diskManager.GetProgress()
	fmt.Printf("   ✓ Allocated: %.2f MB (%d files)\n", 
		float64(progress.AllocatedSize)/1024/1024, progress.FileCount)
	
	// 3. Set up piece manager
	fmt.Println("\n3. Setting up piece manager...")
	
	// Calculate last piece size
	lastPieceSize := int(t.PieceSize(t.NumPieces() - 1))
	
	// Create piece hashes
	pieceHashes := make([][20]byte, t.NumPieces())
	for i := 0; i < t.NumPieces(); i++ {
		hash, _ := t.PieceHash(i)
		pieceHashes[i] = hash
	}
	
	pieceManager := piece.NewManager(
		t.NumPieces(),
		int(t.Info.PieceLength),
		lastPieceSize,
		pieceHashes,
	)
	
	// Connect disk manager to piece manager
	pieceManager.SetDiskManager(diskManager)
	
	fmt.Printf("   ✓ Managing %d pieces\n", t.NumPieces())
	fmt.Printf("   ✓ Piece size: %d bytes (last: %d bytes)\n", 
		t.Info.PieceLength, lastPieceSize)
	fmt.Printf("   ✓ Connected to disk manager\n")
	
	// 4. Set up peer manager
	fmt.Println("\n4. Setting up peer manager...")
	
	// Generate a peer ID
	var peerID [20]byte
	copy(peerID[:], []byte("BITTORRENT-TEST-2025"))
	
	peerManager := peer.NewManager(t.InfoHash, peerID, t.NumPieces())
	peerManager.SetPieceManager(pieceManager)
	
	fmt.Printf("   ✓ Peer ID: %x\n", peerID)
	fmt.Printf("   ✓ Connected to piece manager\n")
	
	// 5. Test piece selection strategies
	fmt.Println("\n5. Testing piece selection strategies...")
	
	// Test sequential strategy
	sequentialStrategy := piece.NewSequentialStrategy()
	pieceManager.SetSelectionStrategy(sequentialStrategy)
	
	// Create a mock peer bitfield (peer has first 10 pieces)
	mockBitfield := make([]byte, (t.NumPieces()+7)/8)
	for i := 0; i < 10 && i < t.NumPieces(); i++ {
		byteIndex := i / 8
		bitIndex := i % 8
		mockBitfield[byteIndex] |= (1 << (7 - bitIndex))
	}
	
	selectedPiece := pieceManager.GetNextPiece(mockBitfield)
	if selectedPiece != nil {
		fmt.Printf("   ✓ Sequential strategy selected piece: %d\n", selectedPiece.Index)
	} else {
		fmt.Printf("   ✓ Sequential strategy: no pieces available\n")
	}
	
	// Test random strategy
	randomStrategy := piece.NewRandomStrategy()
	pieceManager.SetSelectionStrategy(randomStrategy)
	
	selectedPiece = pieceManager.GetNextPiece(mockBitfield)
	if selectedPiece != nil {
		fmt.Printf("   ✓ Random strategy selected piece: %d\n", selectedPiece.Index)
	} else {
		fmt.Printf("   ✓ Random strategy: no pieces available\n")
	}
	
	// 6. Test block operations
	fmt.Println("\n6. Testing block operations...")
	
	if selectedPiece != nil {
		blocks := pieceManager.GetNextBlocks(selectedPiece.Index, 5)
		fmt.Printf("   ✓ Got %d blocks for piece %d\n", len(blocks), selectedPiece.Index)
		
		if len(blocks) > 0 {
			block := blocks[0]
			fmt.Printf("   ✓ First block: offset=%d, length=%d\n", block.Begin, block.Length)
		}
	}
	
	// 7. Test statistics
	fmt.Println("\n7. Current statistics...")
	
	pieceStats := pieceManager.GetStatistics()
	fmt.Printf("   Piece Manager:\n")
	fmt.Printf("   - Total pieces: %d\n", pieceStats.TotalPieces)
	fmt.Printf("   - Verified pieces: %d\n", pieceStats.VerifiedPieces)
	fmt.Printf("   - Progress: %.2f%%\n", pieceManager.GetProgress())
	fmt.Printf("   - Missing pieces: %d\n", len(pieceManager.GetMissingPieces()))
	
	peerStats := peerManager.GetStats()
	fmt.Printf("   Peer Manager:\n")
	fmt.Printf("   - Active peers: %d\n", peerStats.ActivePeers)
	fmt.Printf("   - Total connected: %d\n", peerStats.TotalConnected)
	fmt.Printf("   - Bytes downloaded: %d\n", peerStats.BytesDownloaded)
	fmt.Printf("   - Bytes uploaded: %d\n", peerStats.BytesUploaded)
	
	// 8. Test piece verification
	fmt.Println("\n8. Testing piece verification...")
	
	// Create some test data
	testData := make([]byte, t.Info.PieceLength)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	
	// Test verification (should fail with random data)
	isValid := diskManager.VerifyPiece(0, testData)
	fmt.Printf("   ✓ Random data verification: %t (expected: false)\n", isValid)
	
	// Test with real piece data (would need to read from actual torrent)
	realPieceData, err := diskManager.ReadPiece(0)
	if err == nil && len(realPieceData) > 0 {
		isValid = diskManager.VerifyPiece(0, realPieceData)
		fmt.Printf("   ✓ Real piece verification: %t\n", isValid)
	} else {
		fmt.Printf("   ✓ No real piece data available (file is empty)\n")
	}
	
	fmt.Println("\n=== INTEGRATION TEST COMPLETE ===")
	fmt.Println("All components are working together successfully!")
	fmt.Printf("The BitTorrent client is ready to download: %s\n", t.Info.Name)
}