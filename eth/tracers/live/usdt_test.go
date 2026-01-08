// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package live

import (
	"bufio"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
)

// TestBlockInfoWrite verifies that block info is correctly written to jsonl file.
func TestBlockInfoWrite(t *testing.T) {
	// Create temp directory for test output
	tmpDir, err := os.MkdirTemp("", "usdt_tracer_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create tracer with test config
	cfg := []byte(`{"path":"` + tmpDir + `"}`)
	hooks, err := newUsdtTracer(json.RawMessage(cfg))
	if err != nil {
		t.Fatalf("failed to create tracer: %v", err)
	}

	// Simulate block processing
	timestamp := uint64(time.Date(2025, 1, 8, 12, 0, 0, 0, time.UTC).Unix())
	header := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Time:       timestamp,
	}
	block := types.NewBlockWithHeader(header)

	// Call block start and end hooks
	hooks.OnBlockStart(tracing.BlockEvent{Block: block})
	hooks.OnBlockEnd(nil)

	// Close the tracer to flush
	hooks.OnClose()

	// Verify block info file was created
	expectedDate := time.Unix(int64(timestamp), 0).UTC().Format("20060102")
	blockInfoPath := filepath.Join(tmpDir, "block_info_"+expectedDate+".jsonl")

	if _, err := os.Stat(blockInfoPath); os.IsNotExist(err) {
		t.Fatalf("block info file not created: %s", blockInfoPath)
	}

	// Read and verify content
	file, err := os.Open(blockInfoPath)
	if err != nil {
		t.Fatalf("failed to open block info file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("no content in block info file")
	}

	var info blockInfo
	if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
		t.Fatalf("failed to parse block info: %v", err)
	}

	// Verify fields
	if info.BlockNumber != 100 {
		t.Errorf("expected block number 100, got %d", info.BlockNumber)
	}
	if info.ParentHash != header.ParentHash {
		t.Errorf("parent hash mismatch: expected %s, got %s", header.ParentHash.Hex(), info.ParentHash.Hex())
	}
	if info.Timestamp != timestamp {
		t.Errorf("timestamp mismatch: expected %d, got %d", timestamp, info.Timestamp)
	}
}

// TestBlockInfoMultipleBlocks verifies that multiple blocks are written correctly.
func TestBlockInfoMultipleBlocks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "usdt_tracer_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := []byte(`{"path":"` + tmpDir + `"}`)
	hooks, err := newUsdtTracer(json.RawMessage(cfg))
	if err != nil {
		t.Fatalf("failed to create tracer: %v", err)
	}

	// Simulate 3 consecutive blocks
	baseTime := uint64(time.Date(2025, 1, 8, 12, 0, 0, 0, time.UTC).Unix())
	blocks := []struct {
		number     uint64
		parentHash common.Hash
	}{
		{100, common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000099")},
		{101, common.Hash{}}, // will be set to previous block hash
		{102, common.Hash{}}, // will be set to previous block hash
	}

	var prevHash common.Hash
	for i, b := range blocks {
		parentHash := b.parentHash
		if i > 0 {
			parentHash = prevHash
		}
		header := &types.Header{
			Number:     big.NewInt(int64(b.number)),
			ParentHash: parentHash,
			Time:       baseTime + uint64(i*12), // 12 second block time
		}
		block := types.NewBlockWithHeader(header)
		prevHash = block.Hash()

		hooks.OnBlockStart(tracing.BlockEvent{Block: block})
		hooks.OnBlockEnd(nil)
	}

	hooks.OnClose()

	// Verify file content
	expectedDate := time.Unix(int64(baseTime), 0).UTC().Format("20060102")
	blockInfoPath := filepath.Join(tmpDir, "block_info_"+expectedDate+".jsonl")

	file, err := os.Open(blockInfoPath)
	if err != nil {
		t.Fatalf("failed to open block info file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	var lastInfo blockInfo
	for scanner.Scan() {
		var info blockInfo
		if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
			t.Fatalf("failed to parse block info at line %d: %v", count+1, err)
		}
		count++
		lastInfo = info
	}

	if count != 3 {
		t.Errorf("expected 3 block info entries, got %d", count)
	}
	if lastInfo.BlockNumber != 102 {
		t.Errorf("expected last block number 102, got %d", lastInfo.BlockNumber)
	}
}

// TestBlockInfoReorgScenario simulates a reorg scenario where same block number appears twice.
func TestBlockInfoReorgScenario(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "usdt_tracer_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := []byte(`{"path":"` + tmpDir + `"}`)
	hooks, err := newUsdtTracer(json.RawMessage(cfg))
	if err != nil {
		t.Fatalf("failed to create tracer: %v", err)
	}

	baseTime := uint64(time.Date(2025, 1, 8, 12, 0, 0, 0, time.UTC).Unix())
	parentHash := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000099")

	// Block 100 - first version
	header1 := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: parentHash,
		Time:       baseTime,
		GasLimit:   1000000, // different gas limit to get different hash
	}
	block1 := types.NewBlockWithHeader(header1)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block1})
	hooks.OnBlockEnd(nil)

	// Block 101 - on first chain
	header2 := &types.Header{
		Number:     big.NewInt(101),
		ParentHash: block1.Hash(),
		Time:       baseTime + 12,
		GasLimit:   1000000,
	}
	block2 := types.NewBlockWithHeader(header2)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block2})
	hooks.OnBlockEnd(nil)

	// Reorg: Block 100 again with different hash (simulating reorg)
	header3 := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: parentHash,
		Time:       baseTime,
		GasLimit:   2000000, // different gas limit = different hash
	}
	block3 := types.NewBlockWithHeader(header3)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block3})
	hooks.OnBlockEnd(nil)

	// Block 101 - on new chain
	header4 := &types.Header{
		Number:     big.NewInt(101),
		ParentHash: block3.Hash(),
		Time:       baseTime + 12,
		GasLimit:   2000000,
	}
	block4 := types.NewBlockWithHeader(header4)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block4})
	hooks.OnBlockEnd(nil)

	hooks.OnClose()

	// Verify file content - should have 4 entries (including reorg)
	expectedDate := time.Unix(int64(baseTime), 0).UTC().Format("20060102")
	blockInfoPath := filepath.Join(tmpDir, "block_info_"+expectedDate+".jsonl")

	file, err := os.Open(blockInfoPath)
	if err != nil {
		t.Fatalf("failed to open block info file: %v", err)
	}
	defer file.Close()

	var infos []blockInfo
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var info blockInfo
		if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
			t.Fatalf("failed to parse block info: %v", err)
		}
		infos = append(infos, info)
	}

	if len(infos) != 4 {
		t.Errorf("expected 4 block info entries (including reorg), got %d", len(infos))
	}

	// Verify block 100 appears twice with different hashes
	block100Count := 0
	var block100Hashes []common.Hash
	for _, info := range infos {
		if info.BlockNumber == 100 {
			block100Count++
			block100Hashes = append(block100Hashes, info.BlockHash)
		}
	}

	if block100Count != 2 {
		t.Errorf("expected block 100 to appear 2 times (reorg), got %d", block100Count)
	}

	if len(block100Hashes) == 2 && block100Hashes[0] == block100Hashes[1] {
		t.Error("block 100 should have different hashes after reorg")
	}
}

// TestDateRotation verifies that log files rotate correctly on date change.
func TestDateRotation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "usdt_tracer_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := []byte(`{"path":"` + tmpDir + `"}`)
	hooks, err := newUsdtTracer(json.RawMessage(cfg))
	if err != nil {
		t.Fatalf("failed to create tracer: %v", err)
	}

	// Day 1: 2025-01-08 23:59:00 UTC
	day1Time := uint64(time.Date(2025, 1, 8, 23, 59, 0, 0, time.UTC).Unix())
	header1 := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: common.Hash{},
		Time:       day1Time,
	}
	block1 := types.NewBlockWithHeader(header1)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block1})
	hooks.OnBlockEnd(nil)

	// Day 2: 2025-01-09 00:01:00 UTC
	day2Time := uint64(time.Date(2025, 1, 9, 0, 1, 0, 0, time.UTC).Unix())
	header2 := &types.Header{
		Number:     big.NewInt(101),
		ParentHash: block1.Hash(),
		Time:       day2Time,
	}
	block2 := types.NewBlockWithHeader(header2)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block2})
	hooks.OnBlockEnd(nil)

	hooks.OnClose()

	// Verify two separate files exist
	day1File := filepath.Join(tmpDir, "block_info_20250108.jsonl")
	day2File := filepath.Join(tmpDir, "block_info_20250109.jsonl")

	if _, err := os.Stat(day1File); os.IsNotExist(err) {
		t.Errorf("day 1 file not created: %s", day1File)
	}
	if _, err := os.Stat(day2File); os.IsNotExist(err) {
		t.Errorf("day 2 file not created: %s", day2File)
	}
}
