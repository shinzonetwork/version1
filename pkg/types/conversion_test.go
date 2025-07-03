package types

import (
	"math/big"
	"testing"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

func makeGethBlockAndTx() (*gethtypes.Block, *gethtypes.Transaction, common.Address) {
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x000000000000000000000000000000000000dead")
	tx := gethtypes.NewTransaction(1, to, big.NewInt(1000), 21000, big.NewInt(1), []byte("data"))
	head := &gethtypes.Header{
		Number:     big.NewInt(12345),
		ParentHash: common.HexToHash("0xabc"),
		Time:       1600000000,
		Extra:      []byte("extra"),
	}
	block := gethtypes.NewBlock(head, []*gethtypes.Transaction{tx}, nil, nil, trie.NewStackTrie(nil))
	return block, tx, from
}

func TestConvertBlock(t *testing.T) {
	block, tx, from := makeGethBlockAndTx()
	receipt := &gethtypes.Receipt{TxHash: tx.Hash(), Status: 1, Logs: []*gethtypes.Log{}, BlockNumber: big.NewInt(12345)}
	localReceipt := ConvertReceipt(receipt)
	localTx := ConvertTransaction(tx, from, block, localReceipt, true)
	localBlock := ConvertBlock(block, []Transaction{localTx})

	if localBlock.Hash != block.Hash().Hex() {
		t.Errorf("Block hash mismatch: got %s, want %s", localBlock.Hash, block.Hash().Hex())
	}
	if localBlock.Number != block.Number().String() {
		t.Errorf("Block number mismatch: got %s, want %s", localBlock.Number, block.Number().String())
	}
	if len(localBlock.Transactions) != 1 {
		t.Errorf("Expected 1 transaction, got %d", len(localBlock.Transactions))
	}
}

func TestConvertTransaction(t *testing.T) {
	block, tx, from := makeGethBlockAndTx()
	receipt := &gethtypes.Receipt{TxHash: tx.Hash(), Status: 1, Logs: []*gethtypes.Log{}, BlockNumber: big.NewInt(12345)}
	localReceipt := ConvertReceipt(receipt)
	localTx := ConvertTransaction(tx, from, block, localReceipt, true)

	if localTx.Hash != tx.Hash().Hex() {
		t.Errorf("Tx hash mismatch: got %s, want %s", localTx.Hash, tx.Hash().Hex())
	}
	if localTx.BlockHash != block.Hash().Hex() {
		t.Errorf("Block hash mismatch: got %s, want %s", localTx.BlockHash, block.Hash().Hex())
	}
	if localTx.From != from.Hex() {
		t.Errorf("From mismatch: got %s, want %s", localTx.From, from.Hex())
	}
}

func TestConvertReceipt(t *testing.T) {
	txHash := common.HexToHash("0xabc123")
	receipt := &gethtypes.Receipt{
		TxHash:           txHash,
		TransactionIndex: 2,
		BlockHash:        common.HexToHash("0xdef456"),
		BlockNumber:      big.NewInt(999), // Ensure BlockNumber is set
		CumulativeGasUsed: 100000,
		GasUsed:           21000,
		ContractAddress:   common.HexToAddress("0xdeadbeef"),
		Status:            1,
		Logs:              []*gethtypes.Log{},
	}
	local := ConvertReceipt(receipt)
	if local.TransactionHash != txHash.Hex() {
		t.Errorf("Receipt TxHash mismatch: got %s, want %s", local.TransactionHash, txHash.Hex())
	}
	if local.BlockHash != receipt.BlockHash.Hex() {
		t.Errorf("Receipt BlockHash mismatch: got %s, want %s", local.BlockHash, receipt.BlockHash.Hex())
	}
	if local.BlockNumber != receipt.BlockNumber.String() {
		t.Errorf("Receipt BlockNumber mismatch: got %s, want %s", local.BlockNumber, receipt.BlockNumber.String())
	}
	if local.Status != "1" {
		t.Errorf("Receipt Status mismatch: got %s, want %s", local.Status, "1")
	}
}

func TestConvertLog(t *testing.T) {
	log := &gethtypes.Log{
		Address:     common.HexToAddress("0xabc"),
		Topics:      []common.Hash{common.HexToHash("0x01"), common.HexToHash("0x02")},
		Data:        []byte("logdata"),
		BlockNumber: 123,
		TxHash:      common.HexToHash("0xaaa"),
		TxIndex:     1,
		BlockHash:   common.HexToHash("0xbbbb"),
		Index:       5,
		Removed:     false,
	}
	local := ConvertLog(log)
	if local.Address != log.Address.Hex() {
		t.Errorf("Log Address mismatch: got %s, want %s", local.Address, log.Address.Hex())
	}
	if local.BlockNumber != "123" {
		t.Errorf("Log BlockNumber mismatch: got %s, want %s", local.BlockNumber, "123")
	}
	if local.TransactionHash != log.TxHash.Hex() {
		t.Errorf("Log TxHash mismatch: got %s, want %s", local.TransactionHash, log.TxHash.Hex())
	}
	if local.LogIndex != "5" {
		t.Errorf("LogIndex mismatch: got %s, want %s", local.LogIndex, "5")
	}
}
