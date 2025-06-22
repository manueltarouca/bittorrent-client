package torrent

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mt/bittorrent-impl/internal/bencode"
)

type Torrent struct {
	Announce     string
	AnnounceList [][]string
	CreatedBy    string
	CreationDate int64
	Comment      string
	InfoHash     [20]byte
	Info         Info
}

type Info struct {
	PieceLength int64  `bencode:"piece length"`
	Pieces      []byte `bencode:"pieces"`
	Name        string `bencode:"name"`
	Length      int64  `bencode:"length"`
	Files       []File `bencode:"files"`
}

type File struct {
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"`
}

// rawTorrent is used for decoding the bencode data
type rawTorrent struct {
	Announce     string                 `bencode:"announce"`
	AnnounceList [][]string             `bencode:"announce-list"`
	CreatedBy    string                 `bencode:"created by"`
	CreationDate int64                  `bencode:"creation date"`
	Comment      string                 `bencode:"comment"`
	Info         map[string]interface{} `bencode:"info"`
}

// ParseFile parses a torrent file from disk
func ParseFile(path string) (*Torrent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open torrent file: %w", err)
	}
	defer file.Close()

	return Parse(file)
}

// Parse parses torrent data from an io.Reader
func Parse(r io.Reader) (*Torrent, error) {
	// Read all data to calculate info hash
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read torrent data: %w", err)
	}

	// Decode the torrent
	var raw map[string]interface{}
	if err := bencode.Decode(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode torrent: %w", err)
	}

	// Get the info dictionary to calculate hash
	infoDict, ok := raw["info"].(map[string]interface{})
	if !ok {
		return nil, errors.New("missing info dictionary")
	}

	// Calculate info hash
	infoBencoded, err := bencode.Encode(infoDict)
	if err != nil {
		return nil, fmt.Errorf("failed to encode info dictionary: %w", err)
	}
	infoHash := sha1.Sum(infoBencoded)

	// Create the torrent struct
	t := &Torrent{
		InfoHash: infoHash,
	}

	// Extract fields from raw map
	if announce, ok := raw["announce"].(string); ok {
		t.Announce = announce
	}

	if announceList, ok := raw["announce-list"].([]interface{}); ok {
		for _, tier := range announceList {
			if tierList, ok := tier.([]interface{}); ok {
				var tierUrls []string
				for _, url := range tierList {
					if urlStr, ok := url.(string); ok {
						tierUrls = append(tierUrls, urlStr)
					}
				}
				if len(tierUrls) > 0 {
					t.AnnounceList = append(t.AnnounceList, tierUrls)
				}
			}
		}
	}

	if createdBy, ok := raw["created by"].(string); ok {
		t.CreatedBy = createdBy
	}

	if creationDate, ok := raw["creation date"].(int64); ok {
		t.CreationDate = creationDate
	}

	if comment, ok := raw["comment"].(string); ok {
		t.Comment = comment
	}

	// Parse info dictionary
	if pieceLength, ok := infoDict["piece length"].(int64); ok {
		t.Info.PieceLength = pieceLength
	}

	if pieces, ok := infoDict["pieces"].(string); ok {
		t.Info.Pieces = []byte(pieces)
	}

	if name, ok := infoDict["name"].(string); ok {
		t.Info.Name = name
	}

	// Check for single file vs multi-file mode
	if length, ok := infoDict["length"].(int64); ok {
		// Single file mode
		t.Info.Length = length
	} else if files, ok := infoDict["files"].([]interface{}); ok {
		// Multi-file mode
		for _, file := range files {
			if fileDict, ok := file.(map[string]interface{}); ok {
				var f File
				
				if length, ok := fileDict["length"].(int64); ok {
					f.Length = length
				}
				
				if pathList, ok := fileDict["path"].([]interface{}); ok {
					for _, pathPart := range pathList {
						if pathStr, ok := pathPart.(string); ok {
							f.Path = append(f.Path, pathStr)
						}
					}
				}
				
				if f.Length > 0 && len(f.Path) > 0 {
					t.Info.Files = append(t.Info.Files, f)
				}
			}
		}
	}

	// Validate the torrent
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("invalid torrent: %w", err)
	}

	return t, nil
}

// Validate checks if the torrent data is valid
func (t *Torrent) Validate() error {
	// Allow torrents without announce URLs (DHT-only torrents)
	// if t.Announce == "" && len(t.AnnounceList) == 0 {
	//	return errors.New("no announce URL found")
	// }

	if t.Info.PieceLength <= 0 {
		return errors.New("invalid piece length")
	}

	if len(t.Info.Pieces)%20 != 0 {
		return errors.New("pieces hash length is not a multiple of 20")
	}

	if t.Info.Name == "" {
		return errors.New("torrent name is empty")
	}

	// Single file mode
	if len(t.Info.Files) == 0 && t.Info.Length <= 0 {
		return errors.New("invalid file length")
	}

	// Multi-file mode
	if len(t.Info.Files) > 0 {
		for i, file := range t.Info.Files {
			if file.Length < 0 {
				return fmt.Errorf("file %d has invalid length", i)
			}
			if len(file.Path) == 0 {
				return fmt.Errorf("file %d has empty path", i)
			}
		}
	}

	return nil
}

// IsSingleFile returns true if this torrent contains a single file
func (t *Torrent) IsSingleFile() bool {
	return len(t.Info.Files) == 0
}

// TotalLength returns the total length of all files in the torrent
func (t *Torrent) TotalLength() int64 {
	if t.IsSingleFile() {
		return t.Info.Length
	}

	var total int64
	for _, file := range t.Info.Files {
		total += file.Length
	}
	return total
}

// NumPieces returns the number of pieces in the torrent
func (t *Torrent) NumPieces() int {
	return len(t.Info.Pieces) / 20
}

// PieceHash returns the SHA1 hash for a specific piece
func (t *Torrent) PieceHash(index int) ([20]byte, error) {
	if index < 0 || index >= t.NumPieces() {
		return [20]byte{}, fmt.Errorf("piece index %d out of range", index)
	}

	var hash [20]byte
	start := index * 20
	copy(hash[:], t.Info.Pieces[start:start+20])
	return hash, nil
}

// PieceSize returns the size of a specific piece
func (t *Torrent) PieceSize(index int) int64 {
	if index < 0 || index >= t.NumPieces() {
		return 0
	}

	totalLength := t.TotalLength()
	pieceLength := t.Info.PieceLength

	// All pieces except the last one are full size
	if index < t.NumPieces()-1 {
		return pieceLength
	}

	// Last piece might be smaller
	lastPieceSize := totalLength % pieceLength
	if lastPieceSize == 0 {
		return pieceLength
	}
	return lastPieceSize
}

// GetFiles returns all files in the torrent with their absolute paths
func (t *Torrent) GetFiles() []FileInfo {
	if t.IsSingleFile() {
		return []FileInfo{
			{
				Path:   t.Info.Name,
				Length: t.Info.Length,
				Offset: 0,
			},
		}
	}

	var files []FileInfo
	var offset int64

	for _, file := range t.Info.Files {
		path := filepath.Join(t.Info.Name, filepath.Join(file.Path...))
		files = append(files, FileInfo{
			Path:   path,
			Length: file.Length,
			Offset: offset,
		})
		offset += file.Length
	}

	return files
}

// FileInfo represents a file in the torrent
type FileInfo struct {
	Path   string
	Length int64
	Offset int64 // Offset in the torrent data
}

// InfoHashString returns the info hash as a hex string
func (t *Torrent) InfoHashString() string {
	return hex.EncodeToString(t.InfoHash[:])
}

// GetAnnounceURLs returns all announce URLs
func (t *Torrent) GetAnnounceURLs() []string {
	var urls []string

	if t.Announce != "" {
		urls = append(urls, t.Announce)
	}

	// Add all URLs from announce-list
	for _, tier := range t.AnnounceList {
		for _, url := range tier {
			// Avoid duplicates
			found := false
			for _, existing := range urls {
				if existing == url {
					found = true
					break
				}
			}
			if !found {
				urls = append(urls, url)
			}
		}
	}

	return urls
}

// String returns a human-readable representation of the torrent
func (t *Torrent) String() string {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Torrent: %s\n", t.Info.Name)
	fmt.Fprintf(&buf, "Info Hash: %s\n", t.InfoHashString())
	fmt.Fprintf(&buf, "Piece Length: %d bytes\n", t.Info.PieceLength)
	fmt.Fprintf(&buf, "Total Length: %d bytes\n", t.TotalLength())
	fmt.Fprintf(&buf, "Number of Pieces: %d\n", t.NumPieces())

	if t.Comment != "" {
		fmt.Fprintf(&buf, "Comment: %s\n", t.Comment)
	}
	if t.CreatedBy != "" {
		fmt.Fprintf(&buf, "Created By: %s\n", t.CreatedBy)
	}

	fmt.Fprintf(&buf, "Files:\n")
	for _, file := range t.GetFiles() {
		fmt.Fprintf(&buf, "  %s (%d bytes)\n", file.Path, file.Length)
	}

	fmt.Fprintf(&buf, "Announce URLs:\n")
	for _, url := range t.GetAnnounceURLs() {
		fmt.Fprintf(&buf, "  %s\n", url)
	}

	return buf.String()
}