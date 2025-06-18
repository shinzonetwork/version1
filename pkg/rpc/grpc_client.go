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
	
	// Use a block that's 2-3 blocks behind to avoid transaction type issues
	targetBlockNum := new(big.Int).Sub(latestHeader.Number, big.NewInt(2))
	
	// Retry mechanism for transaction type errors
	var gethBlock *ethtypes.Block
	
	for retries := 0; retries < 3; retries++ {
		gethBlock, err = c.httpClient.BlockByNumber(ctx, targetBlockNum)
		if err != nil {
			if retries < 2 && (err.Error() == "transaction type not supported" || 
			    err.Error() == "invalid transaction type") {
				log.Printf("Retry %d: Transaction type error, trying again...", retries+1)
				// Try an even older block
				targetBlockNum = new(big.Int).Sub(targetBlockNum, big.NewInt(1))
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

// convertGethBlock converts go-ethereum Block to our custom Block type
func (c *GRPCEthereumClient) convertGethBlock(gethBlock *ethtypes.Block) *types.Block {
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

	// Convert the block
	return &types.Block{
		Hash:             gethBlock.Hash().Hex(),
		Number:           fmt.Sprintf("%d", gethBlock.NumberU64()),
		Timestamp:        fmt.Sprintf("%d", gethBlock.Time()),
		ParentHash:       gethBlock.ParentHash().Hex(),
		Difficulty:       gethBlock.Difficulty().String(),
		GasUsed:          fmt.Sprintf("%d", gethBlock.GasUsed()),
		GasLimit:         fmt.Sprintf("%d", gethBlock.GasLimit()),
		Nonce:            fmt.Sprintf("%d", gethBlock.Nonce()),
		Miner:            gethBlock.Coinbase().Hex(),
		Size:             fmt.Sprintf("%d", gethBlock.Size()),
		StateRoot:        gethBlock.Root().Hex(),
		Sha3Uncles:       gethBlock.UncleHash().Hex(),
		TransactionsRoot: gethBlock.TxHash().Hex(),
		ReceiptsRoot:     gethBlock.ReceiptHash().Hex(),
		ExtraData:        common.Bytes2Hex(gethBlock.Extra()),
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
	
	localTx := types.Transaction{
		Hash:             tx.Hash().Hex(),
		BlockHash:        gethBlock.Hash().Hex(),
		BlockNumber:      fmt.Sprintf("%d", gethBlock.NumberU64()),
		From:             fromAddr.Hex(),
		To:               toAddr,
		Value:            tx.Value().String(),
		Gas:              fmt.Sprintf("%d", tx.Gas()),
		GasPrice:         gasPrice.String(),
		Input:            common.Bytes2Hex(tx.Data()),
		Nonce:            fmt.Sprintf("%d", tx.Nonce()),
		TransactionIndex: fmt.Sprintf("%d", index),
		Status:           true, // Default to true, will be updated from receipt
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
