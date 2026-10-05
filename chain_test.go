package chain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testDifficulty = 2 // keep tests fast

func mine(t *testing.T, bc *Blockchain, data string) Block {
	t.Helper()
	b, err := bc.Mine(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMine_ProducesValidBlock(t *testing.T) {
	bc := New(testDifficulty)
	b := mine(t, bc, "hello")

	if !strings.HasPrefix(b.Hash, "00") {
		t.Errorf("hash %s does not meet difficulty", b.Hash)
	}
	if b.Hash != b.ComputeHash() {
		t.Error("stored hash differs from computed")
	}
	if blocks := bc.Blocks(); len(blocks) != 2 || blocks[1].PrevHash != blocks[0].Hash {
		t.Fatalf("chain not linked: %+v", blocks)
	}
}

func TestValidate_DetectsTampering(t *testing.T) {
	bc := New(testDifficulty)
	mine(t, bc, "a")
	mine(t, bc, "b")
	if i, err := bc.Validate(); err != nil {
		t.Fatalf("fresh chain invalid at %d: %v", i, err)
	}

	bc.blocks[1].Data = "tampered"
	i, err := bc.Validate()
	if !errors.Is(err, ErrInvalidHash) || i != 1 {
		t.Fatalf("Validate() = %d, %v; want 1, ErrInvalidHash", i, err)
	}
}

func TestAdd_RejectsBadBlocks(t *testing.T) {
	bc := New(testDifficulty)
	good := mine(t, bc, "x")

	stale := good // links to genesis, not to the current tip
	if err := bc.Add(stale); !errors.Is(err, ErrPrevHashMismatch) {
		t.Errorf("stale block: err = %v", err)
	}

	noPoW := Block{Index: good.Index + 1, Data: "y", PrevHash: good.Hash}
	noPoW.Hash = noPoW.ComputeHash()
	for strings.HasPrefix(noPoW.Hash, "00") { // make sure PoW is NOT satisfied
		noPoW.Nonce++
		noPoW.Hash = noPoW.ComputeHash()
	}
	if err := bc.Add(noPoW); !errors.Is(err, ErrDifficulty) {
		t.Errorf("no PoW: err = %v", err)
	}
}

func TestMine_ConcurrentKeepsChainConsistent(t *testing.T) {
	bc := New(testDifficulty)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := bc.Mine(context.Background(), "tx"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if n := len(bc.Blocks()); n != 9 {
		t.Fatalf("len = %d, want 9", n)
	}
	if i, err := bc.Validate(); err != nil {
		t.Fatalf("invalid at %d: %v", i, err)
	}
}

func TestMine_StopsOnCancel(t *testing.T) {
	bc := New(64) // impossible difficulty
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := bc.Mine(ctx, "never"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chain.json")
	bc := New(testDifficulty)
	mine(t, bc, "persisted")
	if err := bc.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path, 5) // stored difficulty wins
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Difficulty() != testDifficulty || len(loaded.Blocks()) != 2 {
		t.Fatalf("loaded difficulty=%d len=%d", loaded.Difficulty(), len(loaded.Blocks()))
	}

	if _, err := Load(filepath.Join(t.TempDir(), "missing.json"), 3); err != nil {
		t.Fatalf("missing file should create new chain: %v", err)
	}
}
