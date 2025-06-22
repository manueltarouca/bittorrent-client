package torrent

import (
	"bytes"
	"testing"

	"github.com/mt/bittorrent-impl/internal/bencode"
)

func TestParseSingleFileTorrent(t *testing.T) {
	// Create a test torrent
	torrentData := map[string]interface{}{
		"announce": "http://tracker.example.com:8080/announce",
		"created by": "test",
		"creation date": int64(1234567890),
		"comment": "Test torrent",
		"info": map[string]interface{}{
			"piece length": int64(16384),
			"pieces": "12345678901234567890", // 20 bytes (1 piece)
			"name": "test.txt",
			"length": int64(1024),
		},
	}

	encoded, err := bencode.Encode(torrentData)
	if err != nil {
		t.Fatalf("Failed to encode test torrent: %v", err)
	}

	torrent, err := Parse(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Failed to parse torrent: %v", err)
	}

	// Verify fields
	if torrent.Announce != "http://tracker.example.com:8080/announce" {
		t.Errorf("Announce = %v, want %v", torrent.Announce, "http://tracker.example.com:8080/announce")
	}

	if torrent.Info.Name != "test.txt" {
		t.Errorf("Name = %v, want %v", torrent.Info.Name, "test.txt")
	}

	if torrent.Info.Length != 1024 {
		t.Errorf("Length = %v, want %v", torrent.Info.Length, 1024)
	}

	if torrent.Info.PieceLength != 16384 {
		t.Errorf("PieceLength = %v, want %v", torrent.Info.PieceLength, 16384)
	}

	if !torrent.IsSingleFile() {
		t.Error("Expected single file torrent")
	}

	if torrent.NumPieces() != 1 {
		t.Errorf("NumPieces = %v, want %v", torrent.NumPieces(), 1)
	}
}

func TestParseMultiFileTorrent(t *testing.T) {
	// Create a multi-file torrent
	torrentData := map[string]interface{}{
		"announce": "http://tracker.example.com:8080/announce",
		"info": map[string]interface{}{
			"piece length": int64(16384),
			"pieces": "1234567890123456789012345678901234567890", // 40 bytes (2 pieces)
			"name": "test_dir",
			"files": []interface{}{
				map[string]interface{}{
					"length": int64(1024),
					"path": []interface{}{"file1.txt"},
				},
				map[string]interface{}{
					"length": int64(2048),
					"path": []interface{}{"subdir", "file2.txt"},
				},
			},
		},
	}

	encoded, err := bencode.Encode(torrentData)
	if err != nil {
		t.Fatalf("Failed to encode test torrent: %v", err)
	}

	torrent, err := Parse(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Failed to parse torrent: %v", err)
	}

	if torrent.IsSingleFile() {
		t.Error("Expected multi-file torrent")
	}

	if len(torrent.Info.Files) != 2 {
		t.Errorf("Number of files = %v, want %v", len(torrent.Info.Files), 2)
	}

	if torrent.TotalLength() != 3072 {
		t.Errorf("TotalLength = %v, want %v", torrent.TotalLength(), 3072)
	}

	if torrent.NumPieces() != 2 {
		t.Errorf("NumPieces = %v, want %v", torrent.NumPieces(), 2)
	}

	files := torrent.GetFiles()
	if len(files) != 2 {
		t.Fatalf("GetFiles returned %d files, want 2", len(files))
	}

	if files[0].Path != "test_dir/file1.txt" {
		t.Errorf("First file path = %v, want %v", files[0].Path, "test_dir/file1.txt")
	}

	if files[1].Path != "test_dir/subdir/file2.txt" {
		t.Errorf("Second file path = %v, want %v", files[1].Path, "test_dir/subdir/file2.txt")
	}
}

func TestPieceHash(t *testing.T) {
	pieces := make([]byte, 60) // 3 pieces
	for i := 0; i < 60; i++ {
		pieces[i] = byte(i)
	}

	torrent := &Torrent{
		Info: Info{
			Pieces: pieces,
		},
	}

	// Test valid piece indices
	for i := 0; i < 3; i++ {
		hash, err := torrent.PieceHash(i)
		if err != nil {
			t.Errorf("PieceHash(%d) returned error: %v", i, err)
		}

		// Verify the hash starts at the right offset
		if hash[0] != byte(i*20) {
			t.Errorf("PieceHash(%d) first byte = %v, want %v", i, hash[0], byte(i*20))
		}
	}

	// Test invalid piece index
	_, err := torrent.PieceHash(3)
	if err == nil {
		t.Error("PieceHash(3) should return error for out of range index")
	}
}

func TestPieceSize(t *testing.T) {
	torrent := &Torrent{
		Info: Info{
			PieceLength: 16384,
			Length:      50000, // Not evenly divisible by piece length
			Pieces:      make([]byte, 80), // 4 pieces
		},
	}

	tests := []struct {
		index    int
		expected int64
	}{
		{0, 16384},
		{1, 16384},
		{2, 16384},
		{3, 848}, // Last piece: 50000 % 16384 = 848
	}

	for _, tt := range tests {
		size := torrent.PieceSize(tt.index)
		if size != tt.expected {
			t.Errorf("PieceSize(%d) = %v, want %v", tt.index, size, tt.expected)
		}
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		torrent *Torrent
		wantErr bool
	}{
		{
			name: "valid single file",
			torrent: &Torrent{
				Announce: "http://tracker.example.com",
				Info: Info{
					PieceLength: 16384,
					Pieces:      make([]byte, 20),
					Name:        "test.txt",
					Length:      1024,
				},
			},
			wantErr: false,
		},
		{
			name: "missing announce",
			torrent: &Torrent{
				Info: Info{
					PieceLength: 16384,
					Pieces:      make([]byte, 20),
					Name:        "test.txt",
					Length:      1024,
				},
			},
			wantErr: false, // Now allowing DHT-only torrents
		},
		{
			name: "invalid piece length",
			torrent: &Torrent{
				Announce: "http://tracker.example.com",
				Info: Info{
					PieceLength: 0,
					Pieces:      make([]byte, 20),
					Name:        "test.txt",
					Length:      1024,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid pieces length",
			torrent: &Torrent{
				Announce: "http://tracker.example.com",
				Info: Info{
					PieceLength: 16384,
					Pieces:      make([]byte, 19), // Not multiple of 20
					Name:        "test.txt",
					Length:      1024,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.torrent.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetAnnounceURLs(t *testing.T) {
	torrent := &Torrent{
		Announce: "http://tracker1.com",
		AnnounceList: [][]string{
			{"http://tracker2.com", "http://tracker3.com"},
			{"http://tracker4.com", "http://tracker1.com"}, // Duplicate
		},
	}

	urls := torrent.GetAnnounceURLs()
	expected := []string{
		"http://tracker1.com",
		"http://tracker2.com",
		"http://tracker3.com",
		"http://tracker4.com",
	}

	if len(urls) != len(expected) {
		t.Errorf("GetAnnounceURLs returned %d URLs, want %d", len(urls), len(expected))
	}

	for i, url := range expected {
		found := false
		for _, u := range urls {
			if u == url {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("URL %s not found in result", expected[i])
		}
	}
}