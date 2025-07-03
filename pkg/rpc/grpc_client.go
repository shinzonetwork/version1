package rpc

import (
	"context"
	"fmt"
	"log"
	"math/big"

	"shinzo/version1/pkg/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCEthereumClient wraps both gRPC and fallback HTTP client
type GRPCEthereumClient struct {
	grpcConn   *grpc.ClientConn
	httpClient *ethclient.Client
	nodeURL    string
	grpcAddr   string
}

// NewGRPCEthereumClient creates a new gRPC Ethereum client with HTTP fallback
func NewGRPCEthereumClient(grpcAddr, httpNodeURL string) (*GRPCEthereumClient, error) {
	client := &GRPCEthereumClient{
		grpcAddr: grpcAddr,
		nodeURL:  httpNodeURL,
	}

	// Try to establish gRPC connection
	if grpcAddr != "" {
		conn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Failed to connect to gRPC, will use HTTP fallback: %v", err)
		} else {
			client.grpcConn = conn
		}
	}

	// Always establish HTTP client as fallback
	if httpNodeURL != "" {
		httpClient, err := ethclient.Dial(httpNodeURL)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to HTTP client: %w", err)
		}
		client.httpClient = httpClient
	}

	if client.grpcConn == nil && client.httpClient == nil {
		return nil, fmt.Errorf("no valid connection established")
	}

	return client, nil
}

// GetLatestBlock fetches the latest block
func (c *GRPCEthereumClient) GetLatestBlock(ctx context.Context) (*types.Block, error) {
	// For now, use HTTP client (you can implement gRPC here when you have the proto definitions)
	if c.httpClient == nil {
		return nil, fmt.Errorf("no HTTP client available")
	}

	// Get the latest block number first
	latestHeader, err := c.httpClient.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest header: %w", err)
	}

	// Use a block that's 2 blocks behind to avoid transaction type issues
	// targetBlockNum := new(big.Int).Sub(latestHeader.Number, big.NewInt(2))

	var gethBlock *ethtypes.Block

	for retries := 0; retries < 3; retries++ {
		gethBlock, err = c.httpClient.BlockByNumber(ctx, latestHeader.Number)
		if err != nil {
			if retries < 2 && (err.Error() == "transaction type not supported" ||
				err.Error() == "invalid transaction type") {
				log.Printf("Retry %d: Transaction type error, trying again...", retries+1)
				// Try a block that's 1 block behind
				latestHeader.Number = new(big.Int).Sub(latestHeader.Number, big.NewInt(1))
				continue
			}
			return nil, fmt.Errorf("failed to get latest block: %w", err)
		}
		break
	}

	return c.convertGethBlock(gethBlock), nil
}

// GetBlockByNumber fetches a block by number
func (c *GRPCEthereumClient) GetBlockByNumber(ctx context.Context, blockNumber *big.Int) (*types.Block, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("no HTTP client available")
	}

	gethBlock, err := c.httpClient.BlockByNumber(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get block %v: %w", blockNumber, err)
	}

	return c.convertGethBlock(gethBlock), nil
}

// GetNetworkID returns the network ID
func (c *GRPCEthereumClient) GetNetworkID(ctx context.Context) (*big.Int, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("no HTTP client available")
	}

	return c.httpClient.NetworkID(ctx)
}

// GetTransactionReceipt fetches a transaction receipt by hash
func (c *GRPCEthereumClient) GetTransactionReceipt(ctx context.Context, txHash string) (*types.TransactionReceipt, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("no HTTP client available")
	}

	hash := common.HexToHash(txHash)
	receipt, err := c.httpClient.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	return c.convertGethReceipt(receipt), nil
}

// convertGethReceipt converts go-ethereum receipt to our custom receipt type
func (c *GRPCEthereumClient) convertGethReceipt(receipt *ethtypes.Receipt) *types.TransactionReceipt {
	if receipt == nil {
		return nil
	}

	// Convert logs
	logs := make([]types.Log, len(receipt.Logs))
	for i, log := range receipt.Logs {
		logs[i] = c.convertGethLog(log)
	}

	return &types.TransactionReceipt{
		TransactionHash:   receipt.TxHash.Hex(),
		TransactionIndex:  fmt.Sprintf("%d", receipt.TransactionIndex),
		BlockHash:         receipt.BlockHash.Hex(),
		BlockNumber:       fmt.Sprintf("%d", receipt.BlockNumber.Uint64()),
		From:              "", // Will be populated from transaction
		To:                "", // Will be populated from transaction
		CumulativeGasUsed: fmt.Sprintf("%d", receipt.CumulativeGasUsed),
		GasUsed:           fmt.Sprintf("%d", receipt.GasUsed),
		ContractAddress:   getContractAddress(receipt),
		Logs:              logs,
		Status:            getReceiptStatus(receipt),
	}
}

// convertGethLog converts go-ethereum log to our custom log type
func (c *GRPCEthereumClient) convertGethLog(log *ethtypes.Log) types.Log {
	// Convert topics
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	return types.Log{
		Address:          log.Address.Hex(),
		Topics:           topics,
		Data:             common.Bytes2Hex(log.Data),
		BlockNumber:      fmt.Sprintf("%d", log.BlockNumber),
		TransactionHash:  log.TxHash.Hex(),
		TransactionIndex: fmt.Sprintf("%d", log.TxIndex),
		BlockHash:        log.BlockHash.Hex(),
		LogIndex:         fmt.Sprintf("%d", log.Index),
		Removed:          log.Removed,
		Events:           []types.Event{}, // Will be populated by event decoder
	}
}

// Helper functions for receipt conversion
func getContractAddress(receipt *ethtypes.Receipt) string {
	if receipt.ContractAddress == (common.Address{}) {
		return ""
	}
	return receipt.ContractAddress.Hex()
}

func getReceiptStatus(receipt *ethtypes.Receipt) string {
	if receipt.Status == ethtypes.ReceiptStatusSuccessful {
		return "1"
	}
	return "0"
}

// convertGethBlock converts go-ethereum Block to our custom Block type
func (c *GRPCEthereumClient) convertGethBlock(gethBlock *ethtypes.Block) *types.Block {
	if gethBlock == nil {
		return nil
	}

	// Convert transactions
	transactions := make([]types.Transaction, 0, len(gethBlock.Transactions()))

	for i, tx := range gethBlock.Transactions() {
		// Skip transaction conversion if it fails (continue with others)
		localTx, err := c.convertTransaction(tx, gethBlock, i)
		if err != nil {
			log.Printf("Warning: Failed to convert transaction %s: %v", tx.Hash().Hex(), err)
			continue
		}

		transactions = append(transactions, localTx)
	}

	// Convert uncles
	uncles := make([]string, len(gethBlock.Uncles()))
	for i, uncle := range gethBlock.Uncles() {
		uncles[i] = uncle.Hash().Hex()
	}

	// Convert the block
	return &types.Block{
		Hash:             gethBlock.Hash().Hex(),
		Number:           fmt.Sprintf("%d", gethBlock.NumberU64()),
		Timestamp:        fmt.Sprintf("%d", gethBlock.Time()),
		ParentHash:       gethBlock.ParentHash().Hex(),
		Difficulty:       gethBlock.Difficulty().String(),
		TotalDifficulty:  "", // Will be populated separately if needed
		GasUsed:          fmt.Sprintf("%d", gethBlock.GasUsed()),
		GasLimit:         fmt.Sprintf("%d", gethBlock.GasLimit()),
		BaseFeePerGas:    getBaseFeePerGas(gethBlock),
		Nonce:            fmt.Sprintf("%d", gethBlock.Nonce()),
		Miner:            gethBlock.Coinbase().Hex(),
		Coinbase:         gethBlock.Coinbase().Hex(),
		Size:             fmt.Sprintf("%d", gethBlock.Size()),
		StateRoot:        gethBlock.Root().Hex(),
		Sha3Uncles:       gethBlock.UncleHash().Hex(),
		TransactionsRoot: gethBlock.TxHash().Hex(),
		ReceiptsRoot:     gethBlock.ReceiptHash().Hex(),
		LogsBloom:        common.Bytes2Hex(gethBlock.Bloom().Bytes()),
		ExtraData:        common.Bytes2Hex(gethBlock.Extra()),
		MixHash:          gethBlock.MixDigest().Hex(),
		Uncles:           uncles,
		Transactions:     transactions,
	}
}

// convertTransaction safely converts a single transaction
func (c *GRPCEthereumClient) convertTransaction(tx *ethtypes.Transaction, gethBlock *ethtypes.Block, index int) (types.Transaction, error) {
	// Get transaction details with error handling
	fromAddr := getFromAddress(tx)
	toAddr := getToAddress(tx)

	// Handle different transaction types
	var gasPrice *big.Int
	switch tx.Type() {
	case ethtypes.LegacyTxType, ethtypes.AccessListTxType:
		gasPrice = tx.GasPrice()
	case ethtypes.DynamicFeeTxType:
		// For EIP-1559 transactions, use effective gas price if available
		// Fall back to gas fee cap if not
		gasPrice = tx.GasFeeCap()
	default:
		// For unknown transaction types, try to get gas price
		// If it fails, we'll catch it in the calling function
		gasPrice = tx.GasPrice()
	}

	// Extract signature components
	v, r, s := tx.RawSignatureValues()

	// Get access list for EIP-2930/EIP-1559 transactions
	accessList := make([]types.AccessListEntry, 0)
	if tx.AccessList() != nil {
		for _, entry := range tx.AccessList() {
			storageKeys := make([]string, len(entry.StorageKeys))
			for i, key := range entry.StorageKeys {
				storageKeys[i] = key.Hex()
			}
			accessList = append(accessList, types.AccessListEntry{
				Address:     entry.Address.Hex(),
				StorageKeys: storageKeys,
			})
		}
	}

	localTx := types.Transaction{
		Hash:                 tx.Hash().Hex(),
		BlockHash:            gethBlock.Hash().Hex(),
		BlockNumber:          fmt.Sprintf("%d", gethBlock.NumberU64()),
		From:                 fromAddr.Hex(),
		To:                   toAddr,
		Value:                tx.Value().String(),
		Gas:                  fmt.Sprintf("%d", tx.Gas()),
		GasPrice:             gasPrice.String(),
		MaxFeePerGas:         getMaxFeePerGas(tx),
		MaxPriorityFeePerGas: getMaxPriorityFeePerGas(tx),
		Input:                common.Bytes2Hex(tx.Data()),
		Nonce:                fmt.Sprintf("%d", tx.Nonce()),
		TransactionIndex:     fmt.Sprintf("%d", index),
		Type:                 fmt.Sprintf("%d", tx.Type()),
		ChainId:              getChainId(tx),
		AccessList:           accessList,
		V:                    v.String(),
		R:                    r.String(),
		S:                    s.String(),
		Status:               true, // Default to true, will be updated from receipt
	}

	return localTx, nil
}

// Helper functions for transaction conversion
func getFromAddress(tx *ethtypes.Transaction) common.Address {
	// Try different signers to handle various transaction types
	signers := []ethtypes.Signer{
		ethtypes.LatestSignerForChainID(tx.ChainId()),
		ethtypes.NewEIP155Signer(tx.ChainId()),
		ethtypes.NewLondonSigner(tx.ChainId()),
	}

	for _, signer := range signers {
		if from, err := ethtypes.Sender(signer, tx); err == nil {
			return from
		}
	}

	// If all signers fail, return zero address
	return common.Address{}
}

func getToAddress(tx *ethtypes.Transaction) string {
	if tx.To() == nil {
		return "" // Contract creation
	}
	return tx.To().Hex()
}

// getBaseFeePerGas extracts base fee from EIP-1559 blocks
func getBaseFeePerGas(block *ethtypes.Block) string {
	if block.BaseFee() == nil {
		return "" // Not an EIP-1559 block
	}
	return block.BaseFee().String()
}

// getMaxFeePerGas extracts max fee per gas from EIP-1559 transactions
func getMaxFeePerGas(tx *ethtypes.Transaction) string {
	if tx.Type() == ethtypes.DynamicFeeTxType {
		return tx.GasFeeCap().String()
	}
	return ""
}

// getMaxPriorityFeePerGas extracts max priority fee per gas from EIP-1559 transactions
func getMaxPriorityFeePerGas(tx *ethtypes.Transaction) string {
	if tx.Type() == ethtypes.DynamicFeeTxType {
		return tx.GasTipCap().String()
	}
	return ""
}

// getChainId extracts chain ID from transaction
func getChainId(tx *ethtypes.Transaction) string {
	if tx.ChainId() == nil {
		return ""
	}
	return tx.ChainId().String()
}

// Close closes the connections
func (c *GRPCEthereumClient) Close() error {
	var err error
	if c.grpcConn != nil {
		err = c.grpcConn.Close()
	}
	if c.httpClient != nil {
		c.httpClient.Close()
	}
	return err
}
