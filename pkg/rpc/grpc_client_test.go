package rpc

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

func TestNewGRPCEthereumClient_HTTPOnly(t *testing.T) {
	// Start a mock Ethereum JSON-RPC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock response for eth_chainId
		response := `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
	defer server.Close()

	// Convert HTTP URL to WS URL format expected by ethclient
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)

	client, err := NewGRPCEthereumClient("", wsURL)
	if err != nil {
		t.Fatalf("NewGRPCEthereumClient failed: %v", err)
	}
	defer client.Close()

	if client.httpClient == nil {
		t.Error("HTTP client should not be nil")
	}

	if client.grpcConn != nil {
		t.Error("gRPC connection should be nil when no gRPC address provided")
	}

	if client.nodeURL != wsURL {
		t.Errorf("Expected nodeURL %s, got %s", wsURL, client.nodeURL)
	}
}

func TestNewGRPCEthereumClient_InvalidHTTP(t *testing.T) {
	_, err := NewGRPCEthereumClient("", "invalid-url")
	if err == nil {
		t.Error("Expected error for invalid HTTP URL, got nil")
	}
}

func TestGRPCEthereumClient_GetNetworkID_MockClient(t *testing.T) {
	// Create a mock HTTP server for testing
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{"jsonrpc":"2.0","id":1,"result":"0x1"}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
	defer server.Close()

	// We can't easily mock ethclient.Client, so we'll test the client creation only
	client := &GRPCEthereumClient{
		nodeURL: server.URL,
	}

	// This would typically require a real Ethereum node or complex mocking
	// For now, we'll test that the function doesn't panic when httpClient is nil
	_, err := client.GetNetworkID(context.Background())
	if err == nil {
		t.Error("Expected error when httpClient is nil")
	}
}

func TestConvertGethBlock(t *testing.T) {
	// Create a mock Geth block
	header := &ethtypes.Header{
		Number:      big.NewInt(1234567),
		ParentHash:  common.HexToHash("0xparent"),
		Root:        common.HexToHash("0xroot"),
		TxHash:      common.HexToHash("0xtxhash"),
		ReceiptHash: common.HexToHash("0xreceipthash"),
		UncleHash:   common.HexToHash("0xunclehash"),
		Coinbase:    common.HexToAddress("0xcoinbase"),
		Difficulty:  big.NewInt(1000000),
		GasLimit:    8000000,
		GasUsed:     4000000,
		Time:        1600000000,
		Nonce:       ethtypes.BlockNonce{1, 2, 3, 4, 5, 6, 7, 8},
		Extra:       []byte("extra data"),
	}

	// Create transactions
	tx1 := ethtypes.NewTransaction(
		1,
		common.HexToAddress("0xto"),
		big.NewInt(1000),
		21000,
		big.NewInt(20000000000),
		[]byte("data"),
	)

	gethBlock := ethtypes.NewBlock(header, []*ethtypes.Transaction{tx1}, nil, nil, trie.NewStackTrie(nil))

	client := &GRPCEthereumClient{}
	localBlock := client.convertGethBlock(gethBlock)

	if localBlock == nil {
		t.Fatal("Converted block should not be nil")
	}

	if localBlock.Hash != gethBlock.Hash().Hex() {
		t.Errorf("Expected hash %s, got %s", gethBlock.Hash().Hex(), localBlock.Hash)
	}

	if localBlock.Number != gethBlock.Number().String() {
		t.Errorf("Expected number %s, got %s", gethBlock.Number().String(), localBlock.Number)
	}

	if len(localBlock.Transactions) != 1 {
		t.Errorf("Expected 1 transaction, got %d", len(localBlock.Transactions))
	}

	// Test transaction conversion within block
	tx := localBlock.Transactions[0]
	if tx.Hash != tx1.Hash().Hex() {
		t.Errorf("Expected tx hash %s, got %s", tx1.Hash().Hex(), tx.Hash)
	}
}

func TestConvertTransaction(t *testing.T) {
	// Create a mock Geth transaction
	tx := ethtypes.NewTransaction(
		1,                                           // nonce
		common.HexToAddress("0xto"),                // to
		big.NewInt(1000),                           // value
		21000,                                      // gas
		big.NewInt(20000000000),                   // gas price
		[]byte("test data"),                        // data
	)

	// Create a mock block
	header := &ethtypes.Header{
		Number: big.NewInt(1234567),
	}
	gethBlock := ethtypes.NewBlock(header, []*ethtypes.Transaction{}, nil, nil, trie.NewStackTrie(nil))

	client := &GRPCEthereumClient{}
	localTx, err := client.convertTransaction(tx, gethBlock, 0)

	if err != nil {
		t.Fatalf("convertTransaction failed: %v", err)
	}

	if localTx.Hash != tx.Hash().Hex() {
		t.Errorf("Expected hash %s, got %s", tx.Hash().Hex(), localTx.Hash)
	}

	if localTx.BlockNumber != gethBlock.Number().String() {
		t.Errorf("Expected block number %s, got %s", gethBlock.Number().String(), localTx.BlockNumber)
	}

	if localTx.To != tx.To().Hex() {
		t.Errorf("Expected to %s, got %s", tx.To().Hex(), localTx.To)
	}

	if localTx.Value != tx.Value().String() {
		t.Errorf("Expected value %s, got %s", tx.Value().String(), localTx.Value)
	}
}

func TestConvertTransaction_ContractCreation(t *testing.T) {
	// Create a contract creation transaction (to = nil)
	tx := ethtypes.NewContractCreation(
		1,                          // nonce
		big.NewInt(0),             // value
		21000,                     // gas
		big.NewInt(20000000000),   // gas price
		[]byte("contract bytecode"), // data
	)

	header := &ethtypes.Header{
		Number: big.NewInt(1234567),
	}
	gethBlock := ethtypes.NewBlock(header, []*ethtypes.Transaction{}, nil, nil, trie.NewStackTrie(nil))

	client := &GRPCEthereumClient{}
	localTx, err := client.convertTransaction(tx, gethBlock, 0)

	if err != nil {
		t.Fatalf("convertTransaction failed: %v", err)
	}

	// For contract creation, To should be empty
	if localTx.To != "" {
		t.Errorf("Expected empty To for contract creation, got %s", localTx.To)
	}
}

func TestGetFromAddress(t *testing.T) {
	// This is a complex test because it requires proper signature setup
	// For now, we'll test that the function doesn't panic
	tx := ethtypes.NewTransaction(
		1,
		common.HexToAddress("0xto"),
		big.NewInt(1000),
		21000,
		big.NewInt(20000000000),
		[]byte("data"),
	)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("getFromAddress should not panic: %v", r)
		}
	}()

	// This will likely fail because the transaction isn't properly signed
	// but it shouldn't panic
	address := getFromAddress(tx)
	
	// The address might be the zero address due to invalid signature
	if address == (common.Address{}) {
		t.Log("Got zero address, which is expected for unsigned transaction")
	}
}

func TestGetToAddress(t *testing.T) {
	// Test with regular transaction
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	tx := ethtypes.NewTransaction(
		1,
		to,
		big.NewInt(1000),
		21000,
		big.NewInt(20000000000),
		[]byte("data"),
	)

	result := getToAddress(tx)
	expected := to.Hex()

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test with contract creation (to = nil)
	contractTx := ethtypes.NewContractCreation(
		1,
		big.NewInt(0),
		21000,
		big.NewInt(20000000000),
		[]byte("contract bytecode"),
	)

	result = getToAddress(contractTx)
	if result != "" {
		t.Errorf("Expected empty string for contract creation, got %s", result)
	}
}

func TestClose(t *testing.T) {
	client := &GRPCEthereumClient{}

	// Test closing with no connections
	err := client.Close()
	if err != nil {
		t.Errorf("Close should not error with nil connections: %v", err)
	}

	// Test with mock connections would require complex setup
	// The current implementation should handle nil connections gracefully
}

func TestGRPCEthereumClient_NilBlock(t *testing.T) {
	client := &GRPCEthereumClient{}
	
	// Test convertGethBlock with nil block
	result := client.convertGethBlock(nil)
	if result != nil {
		t.Error("convertGethBlock should return nil for nil input")
	}
}
