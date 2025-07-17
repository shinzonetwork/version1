package rpc

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"shinzo/version1/pkg/logger"
	"shinzo/version1/pkg/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// BlockRequestOptions defines options for block requests
type BlockRequestOptions struct {
	HydratedTransactions bool          // Whether to include full transaction objects or just hashes
	Timeout              time.Duration // Request timeout
}

// EthereumClient wraps both JSON-RPC and fallback HTTP client
type EthereumClient struct {
	jsonRPCClient *rpc.Client
	httpClient    *ethclient.Client
	nodeURL       string
	jsonRPCAddr   string
}

// NewEthereumClient creates a new JSON-RPC Ethereum client with HTTP fallback
func NewEthereumClient(jsonRPCAddr, httpNodeURL string) (*EthereumClient, error) {
	client := &EthereumClient{
		jsonRPCAddr: jsonRPCAddr,
		nodeURL:     httpNodeURL,
	}

	// Try to establish JSON-RPC connection
	if jsonRPCAddr != "" {
		rpcClient, err := rpc.Dial(jsonRPCAddr)
		if err != nil {
			fmt.Printf("Failed to connect to JSON-RPC, will use HTTP fallback: %v", err)
		} else {
			client.jsonRPCClient = rpcClient
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

	if client.jsonRPCClient == nil && client.httpClient == nil {
		return nil, fmt.Errorf("no valid connection established")
	}

	return client, nil
}

// GetLatestBlock fetches the latest block
func (c *EthereumClient) GetLatestBlock(ctx context.Context) (*types.Block, error) {
	// For now, use HTTP client (you can implement JSON-RPC here when needed)
	if c.httpClient == nil {
		return nil, fmt.Errorf("no HTTP client available")
	}

	// Get the latest block number first
	latestHeader, err := c.httpClient.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest header: %w", err)
	}

	var gethBlock *ethtypes.Block

	for retries := 0; retries < 3; retries++ {
		gethBlock, err = c.httpClient.BlockByNumber(ctx, latestHeader.Number)
		if err != nil {
			if retries < 2 && (err.Error() == "transaction type not supported" ||
				err.Error() == "invalid transaction type") {
				logger.Sugar.Warnf("Retry %d: Transaction type error, trying again...", retries+1)
				// Try a block that's 1 block behind
				latestHeader.Number = big.NewInt(1).Sub(latestHeader.Number, big.NewInt(1))
				continue
			}
			return nil, fmt.Errorf("failed to get latest block: %w", err)
		}
		break
	}

	return c.convertGethBlock(gethBlock), nil
}

// GetBlockByNumber fetches a block by number using HTTP client
func (c *EthereumClient) GetBlockByNumber(ctx context.Context, blockNumber *big.Int) (*types.Block, error) {
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
func (c *EthereumClient) GetNetworkID(ctx context.Context) (*big.Int, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("no HTTP client available")
	}

	return c.httpClient.NetworkID(ctx)
}

// GetTransactionReceipt fetches a transaction receipt by hash
func (c *EthereumClient) GetTransactionReceipt(ctx context.Context, txHash string) (*types.TransactionReceipt, error) {
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
func (c *EthereumClient) convertGethReceipt(receipt *ethtypes.Receipt) *types.TransactionReceipt {
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
		CumulativeGasUsed: fmt.Sprintf("%d", receipt.CumulativeGasUsed),
		GasUsed:           fmt.Sprintf("%d", receipt.GasUsed),
		ContractAddress:   getContractAddress(receipt),
		Logs:              logs,
		Status:            getReceiptStatus(receipt),
	}
}

// convertGethLog converts go-ethereum log to our custom log type
func (c *EthereumClient) convertGethLog(log *ethtypes.Log) types.Log {
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
		TransactionIndex: int(log.TxIndex),
		BlockHash:        log.BlockHash.Hex(),
		LogIndex:         int(log.Index),
		Removed:          log.Removed,
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
func (c *EthereumClient) convertGethBlock(gethBlock *ethtypes.Block) *types.Block {
	if gethBlock == nil {
		return nil
	}

	// Convert transactions
	transactions := make([]types.Transaction, 0, len(gethBlock.Transactions()))

	for i, tx := range gethBlock.Transactions() {
		// Skip transaction conversion if it fails (continue with others)
		logger.Sugar.Info("Transaction", tx)
		localTx, err := c.convertTransaction(tx, gethBlock, i)
		if err != nil {
			logger.Sugar.Warnf("Warning: Failed to convert transaction %s: %v", tx.Hash().Hex(), err)
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
		Nonce:            int(gethBlock.Nonce()),
		Miner:            gethBlock.Coinbase().Hex(),
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
func (c *EthereumClient) convertTransaction(tx *ethtypes.Transaction, gethBlock *ethtypes.Block, index int) (types.Transaction, error) {
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
		Nonce:                int(tx.Nonce()),
		TransactionIndex:     index,
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

// GetBlockByNumberJSONRPC fetches a block by number using JSON-RPC directly
func (c *EthereumClient) GetBlockByNumberJSONRPC(ctx context.Context, blockNumber *big.Int, hydratedTxs bool) (*types.Block, error) {
	if c.jsonRPCClient == nil {
		return nil, fmt.Errorf("no JSON-RPC client available")
	}

	// Convert block number to hex string for JSON-RPC call
	var blockNumberHex string
	if blockNumber == nil {
		blockNumberHex = "latest"
	} else {
		blockNumberHex = fmt.Sprintf("0x%x", blockNumber)
	}

	// Prepare JSON-RPC request parameters
	params := []interface{}{blockNumberHex, hydratedTxs}

	// Make the JSON-RPC call
	var result interface{}
	err := c.jsonRPCClient.CallContext(ctx, &result, "eth_getBlockByNumber", params...)
	if err != nil {
		return nil, fmt.Errorf("failed to call eth_getBlockByNumber: %w", err)
	}

	// Handle null result (block not found)
	if result == nil {
		return nil, fmt.Errorf("block %v not found", blockNumber)
	}

	// Convert the result to our Block type
	block, err := c.convertJSONRPCBlock(result)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON-RPC block: %w", err)
	}

	return block, nil
}

// GetBlockByNumberJSONRPCWithOptions fetches a block with additional options
func (c *EthereumClient) GetBlockByNumberJSONRPCWithOptions(ctx context.Context, blockNumber *big.Int, options BlockRequestOptions) (*types.Block, error) {
	if c.jsonRPCClient == nil {
		return nil, fmt.Errorf("no JSON-RPC client available")
	}

	// Convert block number to hex string for JSON-RPC call
	var blockNumberHex string
	if blockNumber == nil {
		blockNumberHex = "latest"
	} else {
		blockNumberHex = fmt.Sprintf("0x%x", blockNumber)
	}

	// Prepare JSON-RPC request parameters
	params := []interface{}{blockNumberHex, options.HydratedTransactions}

	// Make the JSON-RPC call with timeout if specified
	var result interface{}
	var err error
	if options.Timeout > 0 {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, options.Timeout)
		defer cancel()
		err = c.jsonRPCClient.CallContext(ctxWithTimeout, &result, "eth_getBlockByNumber", params...)
	} else {
		err = c.jsonRPCClient.CallContext(ctx, &result, "eth_getBlockByNumber", params...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to call eth_getBlockByNumber: %w", err)
	}

	// Handle null result (block not found)
	if result == nil {
		return nil, fmt.Errorf("block %v not found", blockNumber)
	}

	// Convert the result to our Block type
	block, err := c.convertJSONRPCBlock(result)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON-RPC block: %w", err)
	}

	return block, nil
}

// convertJSONRPCBlock converts JSON-RPC block response to our Block type
func (c *EthereumClient) convertJSONRPCBlock(result interface{}) (*types.Block, error) {
	// Convert interface{} to map[string]interface{}
	blockMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid block format")
	}

	// Helper function to safely get string values
	getString := func(key string) string {
		if val, exists := blockMap[key]; exists && val != nil {
			return fmt.Sprintf("%v", val)
		}
		return ""
	}

	// Helper function to safely get and convert hex values
	getHexInt := func(key string) int {
		if val, exists := blockMap[key]; exists && val != nil {
			if hexStr, ok := val.(string); ok {
				if i, err := strconv.ParseInt(hexStr, 0, 64); err == nil {
					return int(i)
				}
			}
		}
		return 0
	}

	// Convert transactions
	var transactions []types.Transaction
	if txs, exists := blockMap["transactions"]; exists && txs != nil {
		switch txList := txs.(type) {
		case []interface{}:
			transactions = make([]types.Transaction, 0, len(txList))
			for i, tx := range txList {
				if txMap, ok := tx.(map[string]interface{}); ok {
					// Full transaction object
					if convertedTx, err := c.convertJSONRPCTransaction(txMap, i); err == nil {
						transactions = append(transactions, convertedTx)
					} else {
						logger.Sugar.Warnf("Failed to convert transaction %d: %v", i, err)
					}
				} else if txHash, ok := tx.(string); ok {
					// Transaction hash only - create minimal transaction
					transactions = append(transactions, types.Transaction{
						Hash:             txHash,
						BlockHash:        getString("hash"),
						BlockNumber:      getString("number"),
						TransactionIndex: i,
					})
				}
			}
		}
	}

	// Convert uncles
	var uncles []string
	if uncleList, exists := blockMap["uncles"]; exists && uncleList != nil {
		if uncleArray, ok := uncleList.([]interface{}); ok {
			uncles = make([]string, 0, len(uncleArray))
			for _, uncle := range uncleArray {
				if uncleStr, ok := uncle.(string); ok {
					uncles = append(uncles, uncleStr)
				}
			}
		}
	}

	// Create the block
	block := &types.Block{
		Hash:             getString("hash"),
		Number:           getString("number"),
		Timestamp:        getString("timestamp"),
		ParentHash:       getString("parentHash"),
		Difficulty:       getString("difficulty"),
		TotalDifficulty:  getString("totalDifficulty"),
		GasUsed:          getString("gasUsed"),
		GasLimit:         getString("gasLimit"),
		BaseFeePerGas:    getString("baseFeePerGas"),
		Nonce:            getHexInt("nonce"),
		Miner:            getString("miner"),
		Size:             getString("size"),
		StateRoot:        getString("stateRoot"),
		Sha3Uncles:       getString("sha3Uncles"),
		TransactionsRoot: getString("transactionsRoot"),
		ReceiptsRoot:     getString("receiptsRoot"),
		LogsBloom:        getString("logsBloom"),
		ExtraData:        getString("extraData"),
		MixHash:          getString("mixHash"),
		Uncles:           uncles,
		Transactions:     transactions,
	}

	return block, nil
}

// convertJSONRPCTransaction converts JSON-RPC transaction to our Transaction type
func (c *EthereumClient) convertJSONRPCTransaction(txMap map[string]interface{}, index int) (types.Transaction, error) {
	// Helper function to safely get string values
	getString := func(key string) string {
		if val, exists := txMap[key]; exists && val != nil {
			return fmt.Sprintf("%v", val)
		}
		return ""
	}

	// Helper function to safely get and convert hex values
	getHexInt := func(key string) int {
		if val, exists := txMap[key]; exists && val != nil {
			if hexStr, ok := val.(string); ok {
				if i, err := strconv.ParseInt(hexStr, 0, 64); err == nil {
					return int(i)
				}
			}
		}
		return 0
	}

	// Convert access list if present
	var accessList []types.AccessListEntry
	if al, exists := txMap["accessList"]; exists && al != nil {
		if alArray, ok := al.([]interface{}); ok {
			accessList = make([]types.AccessListEntry, 0, len(alArray))
			for _, entry := range alArray {
				if entryMap, ok := entry.(map[string]interface{}); ok {
					var storageKeys []string
					if keys, exists := entryMap["storageKeys"]; exists {
						if keyArray, ok := keys.([]interface{}); ok {
							storageKeys = make([]string, 0, len(keyArray))
							for _, key := range keyArray {
								if keyStr, ok := key.(string); ok {
									storageKeys = append(storageKeys, keyStr)
								}
							}
						}
					}
					accessList = append(accessList, types.AccessListEntry{
						Address:     getString("address"),
						StorageKeys: storageKeys,
					})
				}
			}
		}
	}

	// Create the transaction
	tx := types.Transaction{
		Hash:                 getString("hash"),
		BlockHash:            getString("blockHash"),
		BlockNumber:          getString("blockNumber"),
		From:                 getString("from"),
		To:                   getString("to"),
		Value:                getString("value"),
		Gas:                  getString("gas"),
		GasPrice:             getString("gasPrice"),
		MaxFeePerGas:         getString("maxFeePerGas"),
		MaxPriorityFeePerGas: getString("maxPriorityFeePerGas"),
		Input:                getString("input"),
		Nonce:                getHexInt("nonce"),
		TransactionIndex:     index,
		Type:                 getString("type"),
		ChainId:              getString("chainId"),
		AccessList:           accessList,
		V:                    getString("v"),
		R:                    getString("r"),
		S:                    getString("s"),
		Status:               true, // Default to true, updated from receipt
	}

	return tx, nil
}

// Close closes the connections
func (c *EthereumClient) Close() error {
	var err error
	if c.jsonRPCClient != nil {
		c.jsonRPCClient.Close()
	}
	if c.httpClient != nil {
		c.httpClient.Close()
	}
	return err
}
