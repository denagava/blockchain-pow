// Package chain implements a minimal proof-of-work blockchain.
package chain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrPrevHashMismatch = errors.New("previous hash mismatch")
	ErrInvalidHash      = errors.New("hash does not match block contents")
	ErrDifficulty       = errors.New("hash does not meet difficulty")
)

type Block struct {
	Index     int    `json:"index"`
	Timestamp string `json:"timestamp"`
	Data      string `json:"data"`
	PrevHash  string `json:"prev_hash"`
	Hash      string `json:"hash"`
	Nonce     int    `json:"nonce"`
}

// ComputeHash returns the SHA-256 of the block fields. Fields are separated so that
// different field values can never produce the same input string.
func (b Block) ComputeHash() string {
	record := strings.Join([]string{
		strconv.Itoa(b.Index), b.Timestamp, b.Data, b.PrevHash, strconv.Itoa(b.Nonce),
	}, "|")
	sum := sha256.Sum256([]byte(record))
	return hex.EncodeToString(sum[:])
}

// Blockchain is safe for concurrent use.
type Blockchain struct {
	mu         sync.RWMutex
	blocks     []Block
	difficulty int
	prefix     string
}

// New creates a chain with a genesis block.
func New(difficulty int) *Blockchain {
	bc := &Blockchain{difficulty: difficulty, prefix: strings.Repeat("0", difficulty)}
	genesis := Block{Index: 0, Timestamp: time.Now().UTC().Format(time.RFC3339), Data: "Genesis Block", PrevHash: "0"}
	genesis.Hash = genesis.ComputeHash()
	bc.blocks = []Block{genesis}
	return bc
}

func (bc *Blockchain) Difficulty() int { return bc.difficulty }

func (bc *Blockchain) meetsDifficulty(hash string) bool {
	return strings.HasPrefix(hash, bc.prefix)
}

// Blocks returns a copy of the chain.
func (bc *Blockchain) Blocks() []Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return append([]Block(nil), bc.blocks...)
}

func (bc *Blockchain) last() Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.blocks[len(bc.blocks)-1]
}

// Mine searches for a nonce for a new block on top of the current tip and
// appends it. The search runs without holding the lock; if another block was
// added meanwhile, mining restarts on the new tip. It stops when ctx is done.
func (bc *Blockchain) Mine(ctx context.Context, data string) (Block, error) {
	for {
		prev := bc.last()
		b := Block{
			Index:     prev.Index + 1,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Data:      data,
			PrevHash:  prev.Hash,
		}
		for nonce := 0; ; nonce++ {
			if nonce%10_000 == 0 {
				if err := ctx.Err(); err != nil {
					return Block{}, err
				}
			}
			b.Nonce = nonce
			if h := b.ComputeHash(); bc.meetsDifficulty(h) {
				b.Hash = h
				break
			}
		}

		err := bc.Add(b)
		if errors.Is(err, ErrPrevHashMismatch) {
			continue // someone else extended the chain first
		}
		return b, err
	}
}

// Add appends a block after verifying it links to the tip and has valid PoW.
func (bc *Blockchain) Add(b Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if err := bc.verify(b, bc.blocks[len(bc.blocks)-1]); err != nil {
		return err
	}
	bc.blocks = append(bc.blocks, b)
	return nil
}

func (bc *Blockchain) verify(b, prev Block) error {
	switch {
	case b.PrevHash != prev.Hash || b.Index != prev.Index+1:
		return ErrPrevHashMismatch
	case b.ComputeHash() != b.Hash:
		return ErrInvalidHash
	case !bc.meetsDifficulty(b.Hash):
		return ErrDifficulty
	}
	return nil
}

// Validate checks every link of the chain and returns the index of the first
// invalid block, or -1 if the chain is valid.
func (bc *Blockchain) Validate() (int, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	for i := 1; i < len(bc.blocks); i++ {
		if err := bc.verify(bc.blocks[i], bc.blocks[i-1]); err != nil {
			return i, err
		}
	}
	return -1, nil
}

type fileFormat struct {
	Difficulty int     `json:"difficulty"`
	Blocks     []Block `json:"blocks"`
}

// Save writes the chain atomically to path.
func (bc *Blockchain) Save(path string) error {
	bc.mu.RLock()
	data, err := json.MarshalIndent(fileFormat{Difficulty: bc.difficulty, Blocks: bc.blocks}, "", "  ")
	bc.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal chain: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// Load reads a chain from path and validates it. A missing file yields a new chain.
func Load(path string, difficulty int) (*Blockchain, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(difficulty), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(f.Blocks) == 0 {
		return nil, fmt.Errorf("%s contains no blocks", path)
	}
	bc := &Blockchain{blocks: f.Blocks, difficulty: f.Difficulty, prefix: strings.Repeat("0", f.Difficulty)}
	if i, err := bc.Validate(); err != nil {
		return nil, fmt.Errorf("stored chain is invalid at block %d: %w", i, err)
	}
	return bc, nil
}
