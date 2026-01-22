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

// TestDuplicateOnExitCallsCauseDuplicateTransfers reproduces the bug where
// if onExit(depth=0) is called multiple times (which can happen in certain EVM scenarios),
// the same transfer logs get processed multiple times, causing duplicate records.
//
// The root cause is that callStack[0] and frame.Logs are not cleared after processing,
// so calling internalTxsHandler(callStack[0]) again will process the same logs again.
func TestDuplicateOnExitCallsCauseDuplicateTransfers(t *testing.T) {
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

	baseTime := uint64(time.Date(2025, 12, 19, 12, 0, 0, 0, time.UTC).Unix())

	// Block A
	headerA := &types.Header{
		Number:     big.NewInt(24044488),
		ParentHash: common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		Time:       baseTime,
		BaseFee:    big.NewInt(1000000000),
	}
	blockA := types.NewBlockWithHeader(headerA)
	hooks.OnBlockStart(tracing.BlockEvent{Block: blockA})

	// Simulate USDT transfer transaction
	txFrom := common.HexToAddress("0x32462a330e4c7342345f7930177a6eb5361307e9")
	txTo := usdtContractAddress
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(27153148),
		Gas:      100000,
		To:       &txTo,
		Value:    big.NewInt(0),
	})

	vmCtx := &tracing.VMContext{BaseFee: big.NewInt(1000000000)}
	hooks.OnTxStart(vmCtx, tx, txFrom)
	hooks.OnEnter(0, 0, txFrom, usdtContractAddress, nil, 100000, big.NewInt(0))

	// Add Transfer log
	log := &types.Log{
		Address: usdtContractAddress,
		Topics: []common.Hash{
			transferTopic,
			common.BytesToHash(common.LeftPadBytes(txFrom.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress("0x598eb993b41b469604ceef332b46657cd6518360").Bytes(), 32)),
		},
		Data: common.LeftPadBytes(big.NewInt(1627090000).Bytes(), 32),
	}
	hooks.OnLog(log)

	// BUG REPRODUCTION: Call onExit(depth=0) multiple times
	// This simulates what might happen in certain EVM execution scenarios
	hooks.OnExit(0, nil, 63209, nil, false) // First call - normal
	hooks.OnExit(0, nil, 0, nil, false)     // Second call - causes duplicate!
	hooks.OnExit(0, nil, 0, nil, false)     // Third call - causes another duplicate!

	receipt := &types.Receipt{
		Status:  types.ReceiptStatusSuccessful,
		GasUsed: 63209,
	}
	hooks.OnTxEnd(receipt, nil)
	hooks.OnBlockEnd(nil)
	hooks.OnClose()

	// Check for duplicates
	expectedDate := time.Unix(int64(baseTime), 0).UTC().Format("20060102")
	transferPath := filepath.Join(tmpDir, "usdt_transfer_"+expectedDate+".jsonl")

	file, err := os.Open(transferPath)
	if err != nil {
		t.Fatalf("failed to open transfer file: %v", err)
	}
	defer file.Close()

	var transfers []usdtTransfer
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var transfer usdtTransfer
		if err := json.Unmarshal(scanner.Bytes(), &transfer); err != nil {
			t.Fatalf("failed to parse transfer: %v", err)
		}
		transfers = append(transfers, transfer)
	}

	// Should be exactly 1 transfer, not 3!
	if len(transfers) != 1 {
		t.Errorf("BUG REPRODUCED: expected 1 transfer, got %d (duplicates due to multiple onExit calls)", len(transfers))
		for i, tr := range transfers {
			t.Logf("  transfer %d: blockNumber=%d, gasUsed=%d", i, tr.BlockNumber, tr.GasUsed)
		}
	}
}

// TestSystemCallCausesDuplicateTransfers reproduces the bug where
// post-execution system calls (like EIP-7002) trigger OnEnter/OnExit without
// OnTxStart/OnTxEnd. If callStack is not cleared after onExit(depth=0),
// the old frame from the last transaction gets reprocessed during system call.
//
// This is the ROOT CAUSE of the production bug where:
// - Last tx in block A is USDT transfer
// - System call after transactions triggers OnEnter/OnExit
// - Old frame gets reprocessed, causing duplicates
// - pendingTransfers leaks to next block
func TestSystemCallCausesDuplicateTransfers(t *testing.T) {
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

	baseTime := uint64(time.Date(2025, 12, 19, 12, 0, 0, 0, time.UTC).Unix())
	vmCtx := &tracing.VMContext{BaseFee: big.NewInt(1000000000)}

	// ============ Block A ============
	headerA := &types.Header{
		Number:     big.NewInt(24044488),
		ParentHash: common.Hash{},
		Time:       baseTime,
		BaseFee:    big.NewInt(1000000000),
	}
	blockA := types.NewBlockWithHeader(headerA)
	hooks.OnBlockStart(tracing.BlockEvent{Block: blockA})

	// Last transaction in block A: USDT transfer
	txFrom := common.HexToAddress("0x32462a330e4c7342345f7930177a6eb5361307e9")
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(27153148),
		Gas:      100000,
		To:       &usdtContractAddress,
	})
	hooks.OnTxStart(vmCtx, tx, txFrom)
	hooks.OnEnter(0, 0, txFrom, usdtContractAddress, nil, 100000, big.NewInt(0))

	// USDT Transfer log
	log := &types.Log{
		Address: usdtContractAddress,
		Topics: []common.Hash{
			transferTopic,
			common.BytesToHash(common.LeftPadBytes(txFrom.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress("0x598eb993b41b469604ceef332b46657cd6518360").Bytes(), 32)),
		},
		Data: common.LeftPadBytes(big.NewInt(1627090000).Bytes(), 32),
	}
	hooks.OnLog(log)
	hooks.OnExit(0, nil, 63209, nil, false)

	// TODO implement me
	receipt := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 63210}
	hooks.OnTxEnd(receipt, nil)

	// ============ Simulate Post-execution System Call (EIP-7002 style) ============
	// System calls trigger OnEnter/OnExit but NOT OnTxStart/OnTxEnd!
	// This is the key difference that causes the bug.
	systemCallTo := common.HexToAddress("0x00000000000000000000000000000000000000ff") // some system contract
	hooks.OnEnter(0, 0, common.Address{}, systemCallTo, nil, 30_000_000, big.NewInt(0))
	hooks.OnExit(0, nil, 1000, nil, false)
	// Note: NO OnTxEnd here! pendingTransfers might have residual data

	hooks.OnBlockEnd(nil)

	// ============ Block B ============
	headerB := &types.Header{
		Number:     big.NewInt(24044489),
		ParentHash: blockA.Hash(),
		Time:       baseTime + 12,
		BaseFee:    big.NewInt(1000000000),
	}
	blockB := types.NewBlockWithHeader(headerB)
	hooks.OnBlockStart(tracing.BlockEvent{Block: blockB})

	// Simulate pre-execution system call (like ProcessBeaconBlockRoot)
	beaconRootContract := common.HexToAddress("0x000F3df6D732807Ef1319fB7B8bB8522d0Beac02")
	hooks.OnEnter(0, 0, common.Address{}, beaconRootContract, nil, 30_000_000, big.NewInt(0))
	hooks.OnExit(0, nil, 1000, nil, false)

	// Normal transaction in block B
	tx2To := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx2 := types.NewTx(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(27153148),
		Gas:      21000,
		To:       &tx2To,
	})
	hooks.OnTxStart(vmCtx, tx2, txFrom)
	hooks.OnEnter(0, 0, txFrom, tx2To, nil, 21000, big.NewInt(1000000000))
	hooks.OnExit(0, nil, 21000, nil, false)

	receipt2 := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 21000}
	hooks.OnTxEnd(receipt2, nil)

	hooks.OnBlockEnd(nil)
	hooks.OnClose()

	// Verify results
	expectedDate := time.Unix(int64(baseTime), 0).UTC().Format("20060102")
	transferPath := filepath.Join(tmpDir, "usdt_transfer_"+expectedDate+".jsonl")

	file, err := os.Open(transferPath)
	if err != nil {
		t.Fatalf("failed to open transfer file: %v", err)
	}
	defer file.Close()

	var transfers []usdtTransfer
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var transfer usdtTransfer
		if err := json.Unmarshal(scanner.Bytes(), &transfer); err != nil {
			t.Fatalf("failed to parse transfer: %v", err)
		}
		transfers = append(transfers, transfer)
	}

	// Should be exactly 1 transfer from block A
	if len(transfers) != 1 {
		t.Errorf("BUG: expected 1 transfer, got %d (system call caused duplicates or leakage)", len(transfers))
		for i, tr := range transfers {
			t.Logf("  transfer %d: blockNumber=%d, gasUsed=%d", i, tr.BlockNumber, tr.GasUsed)
		}
	}

	// All transfers should be from block A (24044488)
	for i, tr := range transfers {
		if tr.BlockNumber != 24044488 {
			t.Errorf("transfer %d leaked to wrong block: expected 24044488, got %d", i, tr.BlockNumber)
		}
	}
}

// TestCallStackNotClearedLeaksToNextBlock reproduces the bug where
// callStack[0] is not cleared after onExit(depth=0), causing the old frame
// to potentially be reprocessed if the tracer state is not properly reset.
func TestCallStackNotClearedLeaksToNextBlock(t *testing.T) {
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

	baseTime := uint64(time.Date(2025, 12, 19, 12, 0, 0, 0, time.UTC).Unix())
	vmCtx := &tracing.VMContext{BaseFee: big.NewInt(1000000000)}

	// Block A - USDT transfer as last transaction
	headerA := &types.Header{
		Number:     big.NewInt(24044488),
		ParentHash: common.Hash{},
		Time:       baseTime,
		BaseFee:    big.NewInt(1000000000),
	}
	blockA := types.NewBlockWithHeader(headerA)
	hooks.OnBlockStart(tracing.BlockEvent{Block: blockA})

	txFrom := common.HexToAddress("0x32462a330e4c7342345f7930177a6eb5361307e9")
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(27153148),
		Gas:      100000,
		To:       &usdtContractAddress,
	})
	hooks.OnTxStart(vmCtx, tx, txFrom)
	hooks.OnEnter(0, 0, txFrom, usdtContractAddress, nil, 100000, big.NewInt(0))

	log := &types.Log{
		Address: usdtContractAddress,
		Topics: []common.Hash{
			transferTopic,
			common.BytesToHash(common.LeftPadBytes(txFrom.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress("0x598eb993b41b469604ceef332b46657cd6518360").Bytes(), 32)),
		},
		Data: common.LeftPadBytes(big.NewInt(1627090000).Bytes(), 32),
	}
	hooks.OnLog(log)
	hooks.OnExit(0, nil, 63209, nil, false)

	receipt := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 63209}
	hooks.OnTxEnd(receipt, nil)
	hooks.OnBlockEnd(nil)

	// Block B - Simulate a scenario where onBlockStart doesn't clear state properly
	// and onExit is called with callStack still containing old data
	headerB := &types.Header{
		Number:     big.NewInt(24044489),
		ParentHash: blockA.Hash(),
		Time:       baseTime + 12,
		BaseFee:    big.NewInt(1000000000),
	}
	blockB := types.NewBlockWithHeader(headerB)
	hooks.OnBlockStart(tracing.BlockEvent{Block: blockB})

	// Get access to the tracer to check internal state
	// This simulates checking if callStack was properly cleared
	// In the buggy version, if we directly call onExit without onTxStart clearing callStack,
	// the old frame might still be there

	tx2To := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx2 := types.NewTx(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(27153148),
		Gas:      21000,
		To:       &tx2To,
	})
	hooks.OnTxStart(vmCtx, tx2, txFrom)
	hooks.OnEnter(0, 0, txFrom, tx2To, nil, 21000, big.NewInt(1000000000))
	hooks.OnExit(0, nil, 21000, nil, false)

	receipt2 := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 21000}
	hooks.OnTxEnd(receipt2, nil)
	hooks.OnBlockEnd(nil)
	hooks.OnClose()

	// Verify only 1 transfer from block A
	expectedDate := time.Unix(int64(baseTime), 0).UTC().Format("20060102")
	transferPath := filepath.Join(tmpDir, "usdt_transfer_"+expectedDate+".jsonl")

	file, err := os.Open(transferPath)
	if err != nil {
		t.Fatalf("failed to open transfer file: %v", err)
	}
	defer file.Close()

	var transfers []usdtTransfer
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var transfer usdtTransfer
		if err := json.Unmarshal(scanner.Bytes(), &transfer); err != nil {
			t.Fatalf("failed to parse transfer: %v", err)
		}
		transfers = append(transfers, transfer)
	}

	if len(transfers) != 1 {
		t.Errorf("expected 1 transfer, got %d", len(transfers))
		for i, tr := range transfers {
			t.Logf("transfer %d: block=%d, gasUsed=%d", i, tr.BlockNumber, tr.GasUsed)
		}
	}
}

// TestPendingTransfersCleanup verifies that pendingTransfers is correctly cleaned up
// between transactions and blocks to prevent data leakage.
func TestPendingTransfersCleanup(t *testing.T) {
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

	baseTime := uint64(time.Date(2025, 12, 19, 12, 0, 0, 0, time.UTC).Unix())

	// Block 1
	header1 := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: common.Hash{},
		Time:       baseTime,
		BaseFee:    big.NewInt(1000000000),
	}
	block1 := types.NewBlockWithHeader(header1)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block1})

	// Transaction 1: USDT transfer
	txFrom := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx1 := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      100000,
		To:       &usdtContractAddress,
		Value:    big.NewInt(0),
	})

	vmCtx := &tracing.VMContext{BaseFee: big.NewInt(1000000000)}
	hooks.OnTxStart(vmCtx, tx1, txFrom)
	hooks.OnEnter(0, 0, txFrom, usdtContractAddress, nil, 100000, big.NewInt(0))

	log1 := &types.Log{
		Address: usdtContractAddress,
		Topics: []common.Hash{
			transferTopic,
			common.BytesToHash(common.LeftPadBytes(txFrom.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes(), 32)),
		},
		Data: common.LeftPadBytes(big.NewInt(1000000).Bytes(), 32),
	}
	hooks.OnLog(log1)
	hooks.OnExit(0, nil, 50000, nil, false)

	receipt1 := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 50000}
	hooks.OnTxEnd(receipt1, nil)

	// Transaction 2: Another USDT transfer in same block
	tx2 := types.NewTx(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      100000,
		To:       &usdtContractAddress,
		Value:    big.NewInt(0),
	})

	hooks.OnTxStart(vmCtx, tx2, txFrom)
	hooks.OnEnter(0, 0, txFrom, usdtContractAddress, nil, 100000, big.NewInt(0))

	log2 := &types.Log{
		Address: usdtContractAddress,
		Topics: []common.Hash{
			transferTopic,
			common.BytesToHash(common.LeftPadBytes(txFrom.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress("0x3333333333333333333333333333333333333333").Bytes(), 32)),
		},
		Data: common.LeftPadBytes(big.NewInt(2000000).Bytes(), 32),
	}
	hooks.OnLog(log2)
	hooks.OnExit(0, nil, 60000, nil, false)

	receipt2 := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 60000}
	hooks.OnTxEnd(receipt2, nil)

	hooks.OnBlockEnd(nil)

	// Block 2: Should not contain any transfers from block 1
	header2 := &types.Header{
		Number:     big.NewInt(101),
		ParentHash: block1.Hash(),
		Time:       baseTime + 12,
		BaseFee:    big.NewInt(1000000000),
	}
	block2 := types.NewBlockWithHeader(header2)

	hooks.OnBlockStart(tracing.BlockEvent{Block: block2})

	// Transaction 3: Non-USDT transaction
	nonUsdtTo := common.HexToAddress("0x4444444444444444444444444444444444444444")
	tx3 := types.NewTx(&types.LegacyTx{
		Nonce:    2,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &nonUsdtTo,
		Value:    big.NewInt(1000000000),
	})

	hooks.OnTxStart(vmCtx, tx3, txFrom)
	hooks.OnEnter(0, 0, txFrom, nonUsdtTo, nil, 21000, big.NewInt(1000000000))
	hooks.OnExit(0, nil, 21000, nil, false)

	receipt3 := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 21000}
	hooks.OnTxEnd(receipt3, nil)

	hooks.OnBlockEnd(nil)

	hooks.OnClose()

	// Verify transfers
	expectedDate := time.Unix(int64(baseTime), 0).UTC().Format("20060102")
	transferPath := filepath.Join(tmpDir, "usdt_transfer_"+expectedDate+".jsonl")

	file, err := os.Open(transferPath)
	if err != nil {
		t.Fatalf("failed to open transfer file: %v", err)
	}
	defer file.Close()

	var transfers []usdtTransfer
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var transfer usdtTransfer
		if err := json.Unmarshal(scanner.Bytes(), &transfer); err != nil {
			t.Fatalf("failed to parse transfer: %v", err)
		}
		transfers = append(transfers, transfer)
	}

	// Should have exactly 2 transfers (from block 1)
	if len(transfers) != 2 {
		t.Errorf("expected 2 USDT transfers, got %d", len(transfers))
	}

	// All transfers should be from block 100
	for i, transfer := range transfers {
		if transfer.BlockNumber != 100 {
			t.Errorf("transfer %d: expected block 100, got %d", i, transfer.BlockNumber)
		}
	}
}

// TestConcurrentBlockAccess tests that the tracer properly handles state isolation
// by ensuring onBlockStart properly clears state even if called unexpectedly.
func TestConcurrentBlockAccess(t *testing.T) {
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

	baseTime := uint64(time.Date(2025, 12, 19, 12, 0, 0, 0, time.UTC).Unix())
	vmCtx := &tracing.VMContext{BaseFee: big.NewInt(1000000000)}

	// Block 1 starts
	header1 := &types.Header{
		Number:     big.NewInt(100),
		ParentHash: common.Hash{},
		Time:       baseTime,
		BaseFee:    big.NewInt(1000000000),
	}
	block1 := types.NewBlockWithHeader(header1)
	hooks.OnBlockStart(tracing.BlockEvent{Block: block1})

	// Start a USDT transaction
	txFrom := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx1 := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      100000,
		To:       &usdtContractAddress,
	})
	hooks.OnTxStart(vmCtx, tx1, txFrom)
	hooks.OnEnter(0, 0, txFrom, usdtContractAddress, nil, 100000, big.NewInt(0))

	log1 := &types.Log{
		Address: usdtContractAddress,
		Topics: []common.Hash{
			transferTopic,
			common.BytesToHash(common.LeftPadBytes(txFrom.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes(), 32)),
		},
		Data: common.LeftPadBytes(big.NewInt(1000000).Bytes(), 32),
	}
	hooks.OnLog(log1)
	hooks.OnExit(0, nil, 50000, nil, false)
	receipt1 := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 50000}
	hooks.OnTxEnd(receipt1, nil)
	hooks.OnBlockEnd(nil)

	// Simulate a scenario where block 2 starts but something left residual state
	// (this tests the fix for clearing state in onBlockStart)
	header2 := &types.Header{
		Number:     big.NewInt(101),
		ParentHash: block1.Hash(),
		Time:       baseTime + 12,
		BaseFee:    big.NewInt(1000000000),
	}
	block2 := types.NewBlockWithHeader(header2)

	// Block 2 starts - this should clear any residual state
	hooks.OnBlockStart(tracing.BlockEvent{Block: block2})

	// Non-USDT transaction in block 2
	nonUsdtTo := common.HexToAddress("0x3333333333333333333333333333333333333333")
	tx2 := types.NewTx(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &nonUsdtTo,
	})
	hooks.OnTxStart(vmCtx, tx2, txFrom)
	hooks.OnEnter(0, 0, txFrom, nonUsdtTo, nil, 21000, big.NewInt(1000000000))
	hooks.OnExit(0, nil, 21000, nil, false)
	receipt2 := &types.Receipt{Status: types.ReceiptStatusSuccessful, GasUsed: 21000}
	hooks.OnTxEnd(receipt2, nil)
	hooks.OnBlockEnd(nil)

	hooks.OnClose()

	// Verify only 1 transfer from block 100
	expectedDate := time.Unix(int64(baseTime), 0).UTC().Format("20060102")
	transferPath := filepath.Join(tmpDir, "usdt_transfer_"+expectedDate+".jsonl")

	file, err := os.Open(transferPath)
	if err != nil {
		t.Fatalf("failed to open transfer file: %v", err)
	}
	defer file.Close()

	var transfers []usdtTransfer
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var transfer usdtTransfer
		if err := json.Unmarshal(scanner.Bytes(), &transfer); err != nil {
			t.Fatalf("failed to parse transfer: %v", err)
		}
		transfers = append(transfers, transfer)
	}

	if len(transfers) != 1 {
		t.Errorf("expected 1 transfer, got %d", len(transfers))
		for i, tr := range transfers {
			t.Logf("transfer %d: block=%d", i, tr.BlockNumber)
		}
	}

	if len(transfers) > 0 && transfers[0].BlockNumber != 100 {
		t.Errorf("expected transfer from block 100, got block %d", transfers[0].BlockNumber)
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
