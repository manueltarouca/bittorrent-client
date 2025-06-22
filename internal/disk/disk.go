package disk

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/mt/bittorrent-impl/internal/torrent"
)

// Manager handles file I/O operations for torrent downloads
type Manager struct {
	mu          sync.RWMutex
	torrent     *torrent.Torrent
	downloadDir string
	files       map[string]*os.File // filepath -> file handle
	totalSize   int64
	pieceHashes [][20]byte
}

// NewManager creates a new disk manager
func NewManager(torrent *torrent.Torrent, downloadDir string) *Manager {
	// Pre-calculate piece hashes for efficiency
	pieceHashes := make([][20]byte, torrent.NumPieces())
	for i := 0; i < torrent.NumPieces(); i++ {
		hash, _ := torrent.PieceHash(i)
		pieceHashes[i] = hash
	}

	return &Manager{
		torrent:     torrent,
		downloadDir: downloadDir,
		files:       make(map[string]*os.File),
		totalSize:   torrent.TotalLength(),
		pieceHashes: pieceHashes,
	}
}

// Initialize creates the directory structure and opens files
func (d *Manager) Initialize() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Create download directory
	if err := os.MkdirAll(d.downloadDir, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	// Handle single file torrents
	if d.torrent.IsSingleFile() {
		filePath := filepath.Join(d.downloadDir, d.torrent.Info.Name)
		file, err := d.createFile(filePath, d.torrent.Info.Length)
		if err != nil {
			return err
		}
		d.files[filePath] = file
		return nil
	}

	// Handle multi-file torrents
	for _, fileInfo := range d.torrent.Info.Files {
		// Build file path
		fullPath := filepath.Join(d.downloadDir, d.torrent.Info.Name)
		for _, pathComponent := range fileInfo.Path {
			fullPath = filepath.Join(fullPath, pathComponent)
		}

		// Create directory structure
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Create/open file
		file, err := d.createFile(fullPath, fileInfo.Length)
		if err != nil {
			return err
		}
		d.files[fullPath] = file
	}

	return nil
}

// createFile creates or opens a file with the specified size
func (d *Manager) createFile(path string, size int64) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", path, err)
	}

	// Allocate space for the file
	if err := file.Truncate(size); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to allocate space for file %s: %w", path, err)
	}

	return file, nil
}

// WritePiece writes piece data to the appropriate file(s)
func (d *Manager) WritePiece(pieceIndex int, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	pieceLength := d.torrent.Info.PieceLength
	pieceOffset := int64(pieceIndex) * int64(pieceLength)

	// Handle single file torrents
	if d.torrent.IsSingleFile() {
		filePath := filepath.Join(d.downloadDir, d.torrent.Info.Name)
		file, exists := d.files[filePath]
		if !exists {
			return fmt.Errorf("file not open: %s", filePath)
		}

		_, err := file.WriteAt(data, pieceOffset)
		if err != nil {
			return fmt.Errorf("failed to write to file %s at offset %d: %w", filePath, pieceOffset, err)
		}

		return file.Sync()
	}

	// Handle multi-file torrents
	return d.writeMultiFile(pieceOffset, data)
}

// writeMultiFile writes data across multiple files for multi-file torrents
func (d *Manager) writeMultiFile(offset int64, data []byte) error {
	currentOffset := int64(0)
	dataOffset := 0

	for _, fileInfo := range d.torrent.Info.Files {
		fileEnd := currentOffset + fileInfo.Length

		// Skip files that are before our offset
		if offset >= fileEnd {
			currentOffset = fileEnd
			continue
		}

		// Calculate how much to write to this file
		fileWriteStart := int64(0)
		if offset > currentOffset {
			fileWriteStart = offset - currentOffset
		}

		bytesToWrite := int64(len(data)) - int64(dataOffset)
		if fileWriteStart+bytesToWrite > fileInfo.Length {
			bytesToWrite = fileInfo.Length - fileWriteStart
		}

		if bytesToWrite <= 0 {
			break
		}

		// Get file path
		fullPath := filepath.Join(d.downloadDir, d.torrent.Info.Name)
		for _, pathComponent := range fileInfo.Path {
			fullPath = filepath.Join(fullPath, pathComponent)
		}

		file, exists := d.files[fullPath]
		if !exists {
			return fmt.Errorf("file not open: %s", fullPath)
		}

		// Write data
		writeData := data[dataOffset : dataOffset+int(bytesToWrite)]
		_, err := file.WriteAt(writeData, fileWriteStart)
		if err != nil {
			return fmt.Errorf("failed to write to file %s: %w", fullPath, err)
		}

		if err := file.Sync(); err != nil {
			return fmt.Errorf("failed to sync file %s: %w", fullPath, err)
		}

		dataOffset += int(bytesToWrite)
		currentOffset = fileEnd

		// If we've written all data, we're done
		if dataOffset >= len(data) {
			break
		}
	}

	return nil
}

// ReadPiece reads piece data from the appropriate file(s)
func (d *Manager) ReadPiece(pieceIndex int) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	actualLength := int(d.torrent.PieceSize(pieceIndex))
	pieceOffset := int64(pieceIndex) * int64(d.torrent.Info.PieceLength)
	data := make([]byte, actualLength)

	// Handle single file torrents
	if d.torrent.IsSingleFile() {
		filePath := filepath.Join(d.downloadDir, d.torrent.Info.Name)
		file, exists := d.files[filePath]
		if !exists {
			return nil, fmt.Errorf("file not open: %s", filePath)
		}

		_, err := file.ReadAt(data, pieceOffset)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read from file %s at offset %d: %w", filePath, pieceOffset, err)
		}

		return data, nil
	}

	// Handle multi-file torrents
	err := d.readMultiFile(pieceOffset, data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// readMultiFile reads data from multiple files for multi-file torrents
func (d *Manager) readMultiFile(offset int64, data []byte) error {
	currentOffset := int64(0)
	dataOffset := 0

	for _, fileInfo := range d.torrent.Info.Files {
		fileEnd := currentOffset + fileInfo.Length

		// Skip files that are before our offset
		if offset >= fileEnd {
			currentOffset = fileEnd
			continue
		}

		// Calculate how much to read from this file
		fileReadStart := int64(0)
		if offset > currentOffset {
			fileReadStart = offset - currentOffset
		}

		bytesToRead := int64(len(data)) - int64(dataOffset)
		if fileReadStart+bytesToRead > fileInfo.Length {
			bytesToRead = fileInfo.Length - fileReadStart
		}

		if bytesToRead <= 0 {
			break
		}

		// Get file path
		fullPath := filepath.Join(d.downloadDir, d.torrent.Info.Name)
		for _, pathComponent := range fileInfo.Path {
			fullPath = filepath.Join(fullPath, pathComponent)
		}

		file, exists := d.files[fullPath]
		if !exists {
			return fmt.Errorf("file not open: %s", fullPath)
		}

		// Read data
		readData := data[dataOffset : dataOffset+int(bytesToRead)]
		_, err := file.ReadAt(readData, fileReadStart)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read from file %s: %w", fullPath, err)
		}

		dataOffset += int(bytesToRead)
		currentOffset = fileEnd

		// If we've read all data, we're done
		if dataOffset >= len(data) {
			break
		}
	}

	return nil
}

// VerifyPiece verifies a piece using SHA-1 hash
func (d *Manager) VerifyPiece(pieceIndex int, data []byte) bool {
	if pieceIndex < 0 || pieceIndex >= len(d.pieceHashes) {
		return false
	}

	hash := sha1.Sum(data)
	expectedHash := d.pieceHashes[pieceIndex]

	return hash == expectedHash
}

// ReadBlock reads a specific block from a piece
func (d *Manager) ReadBlock(pieceIndex, begin, length int) ([]byte, error) {
	pieceData, err := d.ReadPiece(pieceIndex)
	if err != nil {
		return nil, err
	}

	if begin < 0 || begin >= len(pieceData) {
		return nil, fmt.Errorf("block begin offset %d out of range for piece %d", begin, pieceIndex)
	}

	end := begin + length
	if end > len(pieceData) {
		end = len(pieceData)
	}

	return pieceData[begin:end], nil
}

// Close closes all open files
func (d *Manager) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var errs []error
	for path, file := range d.files {
		if err := file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close file %s: %w", path, err))
		}
	}

	d.files = make(map[string]*os.File)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing files: %v", errs)
	}

	return nil
}

// GetProgress returns download progress information
func (d *Manager) GetProgress() ProgressInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Calculate how much space is allocated
	var totalAllocated int64
	for _, file := range d.files {
		if stat, err := file.Stat(); err == nil {
			totalAllocated += stat.Size()
		}
	}

	return ProgressInfo{
		TotalSize:      d.totalSize,
		AllocatedSize:  totalAllocated,
		DownloadDir:    d.downloadDir,
		FileCount:      len(d.files),
	}
}

// ProgressInfo contains information about download progress
type ProgressInfo struct {
	TotalSize     int64
	AllocatedSize int64
	DownloadDir   string
	FileCount     int
}