package types

import (
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"strconv"
)

// ConvertBlock converts a geth Block to local Block type
func ConvertBlock(block *gethtypes.Block, txs []Transaction) *Block {
	if block == nil {
		return nil
	}
	return &Block{
		Hash:             block.Hash().Hex(),
		Number:           block.Number().String(),
		Timestamp:        big.NewInt(int64(block.Time())).String(),
		ParentHash:       block.ParentHash().Hex(),
		Difficulty:       block.Difficulty().String(),
		GasUsed:          big.NewInt(int64(block.GasUsed())).String(),
		GasLimit:         big.NewInt(int64(block.GasLimit())).String(),
		Nonce:            strconv.FormatUint(block.Nonce(), 10),
		Miner:            block.Coinbase().Hex(),
		Size:             "", // Not available directly
		StateRoot:        block.Root().Hex(),
		Sha3Uncles:       block.UncleHash().Hex(),
		TransactionsRoot: block.TxHash().Hex(),
		ReceiptsRoot:     block.ReceiptHash().Hex(),
		ExtraData:        string(block.Extra()),
		Transactions:     txs,
	}
}

// ConvertTransaction converts a geth Transaction to local Transaction type
func ConvertTransaction(tx *gethtypes.Transaction, msgSender common.Address, block *gethtypes.Block, receipt *TransactionReceipt, status bool) Transaction {
	var to string
	if tx.To() != nil {
		to = tx.To().Hex()
	}
	return Transaction{
		Hash:             tx.Hash().Hex(),
		BlockHash:        block.Hash().Hex(),
		BlockNumber:      block.Number().String(),
		From:             msgSender.Hex(),
		To:               to,
		Value:            tx.Value().String(),
		Gas:              big.NewInt(int64(tx.Gas())).String(),
		GasPrice:         tx.GasPrice().String(),
		Input:            common.Bytes2Hex(tx.Data()),
		Nonce:            big.NewInt(int64(tx.Nonce())).String(),
		TransactionIndex: "", // Not available directly
		Status:           status,
		Logs:             receipt.Logs,
	}
}

// ConvertReceipt converts a geth Receipt to local TransactionReceipt type
func ConvertReceipt(receipt *gethtypes.Receipt) *TransactionReceipt {
	if receipt == nil {
		return nil
	}
	logs := make([]Log, len(receipt.Logs))
	for i, l := range receipt.Logs {
		logs[i] = ConvertLog(l)
	}
	return &TransactionReceipt{
		TransactionHash:   receipt.TxHash.Hex(),
		TransactionIndex:  big.NewInt(int64(receipt.TransactionIndex)).String(),
		BlockHash:         receipt.BlockHash.Hex(),
		BlockNumber:       big.NewInt(int64(receipt.BlockNumber.Uint64())).String(),
		From:              "", // Not available directly
		To:                "", // Not available directly
		CumulativeGasUsed: big.NewInt(int64(receipt.CumulativeGasUsed)).String(),
		GasUsed:           big.NewInt(int64(receipt.GasUsed)).String(),
		ContractAddress:   receipt.ContractAddress.Hex(),
		Logs:              logs,
		Status:            big.NewInt(int64(receipt.Status)).String(),
	}
}

// ConvertLog converts a geth Log to local Log type
func ConvertLog(l *gethtypes.Log) Log {
	topics := make([]string, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = t.Hex()
	}
	return Log{
		Address:          l.Address.Hex(),
		Topics:           topics,
		Data:             common.Bytes2Hex(l.Data),
		BlockNumber:      big.NewInt(int64(l.BlockNumber)).String(),
		TransactionHash:  l.TxHash.Hex(),
		TransactionIndex: big.NewInt(int64(l.TxIndex)).String(),
		BlockHash:        l.BlockHash.Hex(),
		LogIndex:         big.NewInt(int64(l.Index)).String(),
		Removed:          l.Removed,
		Events:           nil, // Event extraction not implemented
	}
}
