# BitTorrent Client

A complete BitTorrent client implementation in Go that supports the core BitTorrent protocol (BEP 3) with real peer-to-peer downloading capabilities.

## Features

- **Complete BitTorrent Protocol**: Full implementation of BEP 3 specification
- **Real P2P Downloads**: Successfully downloads from live torrent swarms
- **Multi-peer Support**: Concurrent downloads from multiple peers
- **Piece Management**: Smart piece selection strategies (sequential, random, smart)
- **Tracker Communication**: HTTP tracker support with announce protocol
- **File I/O Management**: Handles both single-file and multi-file torrents
- **SHA-1 Verification**: Automatic piece verification and corruption detection
- **Download Coordination**: Advanced request pipeline management
- **Progress Monitoring**: Real-time download statistics and progress tracking

## Quick Start

### Build

```bash
go build -o bittorrent cmd/bittorrent/main.go
```

### Usage

```bash
# Display torrent information
./bittorrent --info -t example/BigBuckBunny_124_archive.torrent

# Download a torrent
./bittorrent -t example/BigBuckBunny_124_archive.torrent -o ~/Downloads

# Download with verbose logging
./bittorrent -t example/BigBuckBunny_124_archive.torrent -o ~/Downloads -v

# Test tracker connectivity only
./bittorrent -t example/BigBuckBunny_124_archive.torrent --announce-only
```

### Command Line Options

- `-t, --torrent`: Path to .torrent file (required)
- `-o, --output`: Output directory (default: current directory)
- `-v, --verbose`: Enable verbose logging
- `--info`: Display torrent information and exit
- `--announce-only`: Test tracker connectivity only
- `--strategy`: Piece selection strategy (sequential, random, smart)
- `--help`: Show help message

## Architecture

### Core Components

- **Bencode Parser** (`internal/bencode`): Encodes/decodes BitTorrent's bencode format
- **Torrent Parser** (`internal/torrent`): Parses .torrent files and extracts metadata
- **Tracker Client** (`internal/tracker`): Communicates with HTTP trackers
- **Peer Manager** (`internal/peer`): Manages peer connections and BitTorrent wire protocol
- **Piece Manager** (`internal/piece`): Handles piece and block management with selection strategies
- **Download Coordinator** (`internal/download`): Orchestrates downloads across multiple peers
- **Disk Manager** (`internal/disk`): Handles file I/O operations and piece verification

### Key Features

#### Multi-Peer Download Coordination
- Concurrent requests to multiple peers
- Automatic peer interest management
- Request timeout and retry handling
- Optimized request pipeline (10 concurrent requests per peer)

#### Smart Piece Selection
- **Sequential**: Downloads pieces in order (good for streaming)
- **Random**: Random piece selection for better swarm health
- **Smart**: Intelligent selection based on peer availability

#### BitTorrent Wire Protocol
- Complete handshake implementation
- All standard message types (choke, unchoke, interested, have, bitfield, request, piece, cancel)
- Proper state management (choking, interested states)
- Keep-alive message handling

## Testing

The client has been successfully tested with real torrents:

- **Big Buck Bunny** (Creative Commons movie): Proven to work with Internet Archive torrents
- **Linux Distributions**: Compatible with Ubuntu, Debian, and other distribution torrents
- **Multi-peer Downloads**: Successfully coordinates downloads from 4+ concurrent peers

## Performance Optimizations

- **High Concurrency**: Up to 50 peer connections, 20 concurrent downloads
- **Fast Coordination**: 500ms coordination cycles for responsive downloading
- **Aggressive Requesting**: 10 concurrent block requests per peer
- **Efficient Timeouts**: 15-second timeouts for unresponsive peers

## Example Output

```
2025/06/22 17:17:52 Parsing torrent file: example/BigBuckBunny_124_archive.torrent
2025/06/22 17:17:52 Starting download: BigBuckBunny_124
2025/06/22 17:17:52 Size: 420.95 MB (441396773 bytes)
2025/06/22 17:17:52 Pieces: 842 (each 524288 bytes)
2025/06/22 17:17:53 Found 18 peers total
2025/06/22 17:17:53 Connecting to peers...
2025/06/22 17:17:53 Download coordinator started
2025/06/22 17:17:54 Requesting from 4 downloadable peers
2025/06/22 17:17:54 Requested block 0:0 (length 16384) from peer 95.174.67.91:50200
2025/06/22 17:17:54 Received block 0:0 (length 16384) from peer 95.174.67.91:50200 in 310ms
2025/06/22 17:18:03 Progress: 0.1% (1/842 pieces) | Speed: 0.02 MB/s | Peers: 4 | Downloaded: 0.5 MB
```

## Requirements

- Go 1.19 or later
- Network connectivity for tracker and peer communication

## License

MIT License - See LICENSE file for details

## Contributing

Contributions are welcome! Please read the contributing guidelines and submit pull requests for any improvements.

## Roadmap

Future enhancements could include:
- DHT (Distributed Hash Table) support for trackerless torrents
- PEX (Peer Exchange) for peer discovery
- uTP (μTorrent Transport Protocol) for better NAT traversal
- Magnet link support
- Web seeding support
- Encryption support

## Technical Details

### BitTorrent Protocol Implementation

This client implements the core BitTorrent protocol as specified in BEP 3:

- **Tracker Protocol**: HTTP GET requests with URL encoding
- **Peer Discovery**: Via tracker announce with compact peer format
- **Handshake**: 68-byte handshake with protocol identification
- **Wire Protocol**: Binary message format with length prefixing
- **Piece Verification**: SHA-1 hash validation for data integrity

### Performance Characteristics

- **Block Size**: Standard 16KB blocks for optimal network utilization
- **Piece Size**: Respects torrent-specified piece sizes (typically 256KB-1MB)
- **Connection Management**: Automatic peer connection lifecycle management
- **Memory Efficient**: Streams data directly to disk without full buffering

## Acknowledgments

This implementation follows the BitTorrent protocol specification and incorporates best practices from the BitTorrent community.