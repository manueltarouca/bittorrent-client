package disk

import (
	"crypto/sha1"
	"os"
	"path/filepath"
	"testing"

	"github.com/mt/bittorrent-impl/internal/torrent"
)

// Helper function to create a test torrent
func createTestTorrent(pieceLength int, files []torrent.File, singleFileLength int64) *torrent.Torrent {
	totalLength := int64(0)
	
	if len(files) == 0 {
		// Single file mode
		totalLength = singleFileLength
	} else {
		// Multi-file mode
		for _, file := range files {
			totalLength += file.Length
		}
	}

	// Calculate number of pieces
	numPieces := int((totalLength + int64(pieceLength) - 1) / int64(pieceLength))
	
	// Generate dummy piece data 
	pieces := make([]byte, numPieces*20)
	for i := 0; i < numPieces; i++ {
		hash := sha1.Sum([]byte{byte(i)})
		copy(pieces[i*20:(i+1)*20], hash[:])
	}

	info := torrent.Info{
		PieceLength: int64(pieceLength),
		Pieces:      pieces,
		Name:        "test-torrent",
		Files:       files,
	}

	// For single file torrents
	if len(files) == 0 {
		info.Length = totalLength
	}

	return &torrent.Torrent{
		Info: info,
	}
}

func TestNewManager(t *testing.T) {
	torrent := createTestTorrent(16384, nil, 16384)
	manager := NewManager(torrent, "/tmp/test")

	if manager.torrent != torrent {
		t.Error("Torrent not set correctly")
	}
	if manager.downloadDir != "/tmp/test" {
		t.Error("Download directory not set correctly")
	}
	if len(manager.files) != 0 {
		t.Error("Files map should be empty initially")
	}
	if manager.totalSize != 16384 {
		t.Errorf("Expected total size 16384, got %d", manager.totalSize)
	}
}

func TestInitializeSingleFile(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bittorrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create single file torrent
	torrent := createTestTorrent(16384, nil, 32768)

	manager := NewManager(torrent, tmpDir)
	err = manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer manager.Close()

	// Check if file was created
	expectedPath := filepath.Join(tmpDir, "test-torrent")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Error("Expected file was not created")
	}

	// Check file size
	if stat, err := os.Stat(expectedPath); err == nil {
		if stat.Size() != 32768 {
			t.Errorf("Expected file size 32768, got %d", stat.Size())
		}
	}
}

func TestInitializeMultiFile(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bittorrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multi-file torrent
	files := []torrent.File{
		{Length: 16384, Path: []string{"dir1", "file1.txt"}},
		{Length: 8192, Path: []string{"dir1", "file2.txt"}},
		{Length: 4096, Path: []string{"dir2", "file3.txt"}},
	}
	torrent := createTestTorrent(16384, files, 0)

	manager := NewManager(torrent, tmpDir)
	err = manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer manager.Close()

	// Check if files were created
	expectedFiles := []string{
		filepath.Join(tmpDir, "test-torrent", "dir1", "file1.txt"),
		filepath.Join(tmpDir, "test-torrent", "dir1", "file2.txt"),
		filepath.Join(tmpDir, "test-torrent", "dir2", "file3.txt"),
	}

	for i, expectedPath := range expectedFiles {
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not created", expectedPath)
		}

		// Check file size
		if stat, err := os.Stat(expectedPath); err == nil {
			expectedSize := files[i].Length
			if stat.Size() != expectedSize {
				t.Errorf("File %s: expected size %d, got %d", expectedPath, expectedSize, stat.Size())
			}
		}
	}
}

func TestWriteAndReadPieceSingleFile(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bittorrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create single file torrent
	torrent := createTestTorrent(16384, nil, 32768)

	manager := NewManager(torrent, tmpDir)
	err = manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer manager.Close()

	// Write test data
	testData := make([]byte, 16384)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	err = manager.WritePiece(0, testData)
	if err != nil {
		t.Fatalf("Failed to write piece: %v", err)
	}

	// Read back the data
	readData, err := manager.ReadPiece(0)
	if err != nil {
		t.Fatalf("Failed to read piece: %v", err)
	}

	// Compare data
	if len(readData) != len(testData) {
		t.Errorf("Data length mismatch: expected %d, got %d", len(testData), len(readData))
	}

	for i, b := range testData {
		if i < len(readData) && readData[i] != b {
			t.Errorf("Data mismatch at byte %d: expected %d, got %d", i, b, readData[i])
			break
		}
	}
}

func TestWriteAndReadPieceMultiFile(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bittorrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multi-file torrent that spans pieces across files
	files := []torrent.File{
		{Length: 10000, Path: []string{"file1.txt"}},
		{Length: 10000, Path: []string{"file2.txt"}},
		{Length: 8384, Path: []string{"file3.txt"}},
	}
	torrent := createTestTorrent(16384, files, 0)

	manager := NewManager(torrent, tmpDir)
	err = manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer manager.Close()

	// Write test data that spans across files
	testData := make([]byte, 16384)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	// Write first piece (spans file1 and part of file2)
	err = manager.WritePiece(0, testData)
	if err != nil {
		t.Fatalf("Failed to write piece: %v", err)
	}

	// Read back the data
	readData, err := manager.ReadPiece(0)
	if err != nil {
		t.Fatalf("Failed to read piece: %v", err)
	}

	// Compare data
	if len(readData) != len(testData) {
		t.Errorf("Data length mismatch: expected %d, got %d", len(testData), len(readData))
	}

	for i, b := range testData {
		if i < len(readData) && readData[i] != b {
			t.Errorf("Data mismatch at byte %d: expected %d, got %d", i, b, readData[i])
			break
		}
	}
}

func TestVerifyPiece(t *testing.T) {
	// Create test torrent with known hashes
	testData := []byte("Hello, World! This is test data for piece verification.")
	expectedHash := sha1.Sum(testData)

	// Create pieces data for torrent 
	pieces := make([]byte, 20)
	copy(pieces, expectedHash[:])

	info := torrent.Info{
		PieceLength: int64(len(testData)),
		Pieces:      pieces,
		Name:        "test",
		Length:      int64(len(testData)),
	}

	torrent := &torrent.Torrent{Info: info}
	manager := NewManager(torrent, "/tmp")

	// Test with correct data
	if !manager.VerifyPiece(0, testData) {
		t.Error("Piece verification failed for correct data")
	}

	// Test with incorrect data
	wrongData := []byte("Wrong data")
	if manager.VerifyPiece(0, wrongData) {
		t.Error("Piece verification passed for incorrect data")
	}

	// Test with invalid piece index
	if manager.VerifyPiece(1, testData) {
		t.Error("Piece verification passed for invalid piece index")
	}
}

func TestReadBlock(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bittorrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create single file torrent
	torrent := createTestTorrent(16384, nil, 16384)

	manager := NewManager(torrent, tmpDir)
	err = manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer manager.Close()

	// Write test data
	testData := make([]byte, 16384)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	err = manager.WritePiece(0, testData)
	if err != nil {
		t.Fatalf("Failed to write piece: %v", err)
	}

	// Read a block
	blockData, err := manager.ReadBlock(0, 1000, 2000)
	if err != nil {
		t.Fatalf("Failed to read block: %v", err)
	}

	// Verify block data
	expected := testData[1000:3000]
	if len(blockData) != len(expected) {
		t.Errorf("Block length mismatch: expected %d, got %d", len(expected), len(blockData))
	}

	for i, b := range expected {
		if i < len(blockData) && blockData[i] != b {
			t.Errorf("Block data mismatch at byte %d: expected %d, got %d", i, b, blockData[i])
			break
		}
	}
}

func TestGetProgress(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bittorrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multi-file torrent
	files := []torrent.File{
		{Length: 16384, Path: []string{"file1.txt"}},
		{Length: 8192, Path: []string{"file2.txt"}},
	}
	torrent := createTestTorrent(16384, files, 0)

	manager := NewManager(torrent, tmpDir)
	err = manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer manager.Close()

	progress := manager.GetProgress()

	if progress.TotalSize != 24576 {
		t.Errorf("Expected total size 24576, got %d", progress.TotalSize)
	}
	if progress.AllocatedSize != 24576 {
		t.Errorf("Expected allocated size 24576, got %d", progress.AllocatedSize)
	}
	if progress.FileCount != 2 {
		t.Errorf("Expected file count 2, got %d", progress.FileCount)
	}
	if progress.DownloadDir != tmpDir {
		t.Errorf("Expected download dir %s, got %s", tmpDir, progress.DownloadDir)
	}
}

func TestClose(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "bittorrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create single file torrent
	torrent := createTestTorrent(16384, nil, 16384)

	manager := NewManager(torrent, tmpDir)
	err = manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Check that files are open
	if len(manager.files) == 0 {
		t.Error("No files were opened")
	}

	// Close manager
	err = manager.Close()
	if err != nil {
		t.Errorf("Failed to close manager: %v", err)
	}

	// Check that files are closed
	if len(manager.files) != 0 {
		t.Error("Files were not cleared after close")
	}
}