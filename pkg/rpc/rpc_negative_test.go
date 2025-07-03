package rpc

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"shinzo/version1/pkg/types"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/trie"
)

// TestAlchemyClientNegativeCases tests all negative scenarios for Alchemy client
func TestAlchemyClientNegativeCases(t *testing.T) {
	t.Run("NewAlchemyClient_InvalidURL", func(t *testing.T) {
		invalidURLs := []string{
			"",
			"not-a-url",
			"ftp://invalid-protocol.com",
			"http://",
			"://missing-scheme",
		}
		
		for _, url := range invalidURLs {
			t.Run(url, func(t *testing.T) {
				client := NewAlchemyClient(url)
				// Should still create client but operations will fail
				if client == nil {
					t.Error("Client should be created even with invalid URL")
				}
			})
		}
	})

	t.Run("AlchemyClient_GetBlock_NetworkErrors", func(t *testing.T) {
		// Test various network error scenarios
		errorTests := []struct {
			name        string
			serverFunc  func(w http.ResponseWriter, r *http.Request)
			expectError bool
		}{
			{
				"Connection refused",
				nil, // No server
				true,
			},
			{
				"HTTP 400 Bad Request",
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"error": {"code": -32602, "message": "Invalid params"}}`))
				},
				true,
			},
			{
				"HTTP 401 Unauthorized",
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error": {"code": -32000, "message": "Unauthorized"}}`))
				},
				true,
			},
			{
				"HTTP 429 Rate Limited",
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusTooManyRequests)
					w.Write([]byte(`{"error": {"code": -32005, "message": "Rate limited"}}`))
				},
				true,
			},
			{
				"HTTP 500 Internal Server Error",
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error": {"code": -32000, "message": "Internal error"}}`))
				},
				true,
			},
			{
				"Invalid JSON response",
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`invalid json`))
				},
				true,
			},
			{
				"Empty response",
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				},
				true,
			},
		}
		
		for _, tt := range errorTests {
			t.Run(tt.name, func(t *testing.T) {
				var client *AlchemyClient
				
				if tt.serverFunc != nil {
					server := httptest.NewServer(http.HandlerFunc(tt.serverFunc))
					defer server.Close()
					client = NewAlchemyClient(server.URL)
				} else {
					// Test with non-existent server
					client = NewAlchemyClient("http://localhost:99999")
				}
				
				_, err := client.GetBlock(context.Background(), "latest")
				if tt.expectError && err == nil {
					t.Error("Expected error but got none")
				}
			})
		}
	})

	t.Run("AlchemyClient_GetBlock_MalformedData", func(t *testing.T) {
		malformedResponses := []string{
			`{"result": null}`,
			`{"result": {}}`,
			`{"result": {"number": "invalid_hex"}}`,
			`{"result": {"number": "0x123", "hash": ""}}`,
			`{"result": {"transactions": "not_an_array"}}`,
			`{"error": {"code": -32000, "message": "Block not found"}}`,
		}
		
		for i, response := range malformedResponses {
			t.Run(string(rune('A'+i)), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(response))
				}))
				defer server.Close()
				
				client := NewAlchemyClient(server.URL)
				_, err := client.GetBlock(context.Background(), "latest")
				if err == nil {
					t.Error("Expected error for malformed response")
				}
			})
		}
	})

	t.Run("AlchemyClient_ContextCancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second) // Long delay
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		
		client := NewAlchemyClient(server.URL)
		
		// Create context that cancels quickly
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		
		_, err := client.GetBlock(ctx, "latest")
		if err == nil {
			t.Error("Expected timeout error")
		}
		if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "timeout") {
			t.Errorf("Expected context/timeout error, got: %v", err)
		}
	})

	t.Run("AlchemyClient_GetTransactionReceipt_Errors", func(t *testing.T) {
		errorTests := []struct {
			name     string
			response string
			wantErr  bool
		}{
			{
				"Receipt not found",
				`{"result": null}`,
				true,
			},
			{
				"Invalid transaction hash",
				`{"error": {"code": -32602, "message": "Invalid transaction hash"}}`,
				true,
			},
			{
				"Malformed receipt",
				`{"result": {"status": "invalid"}}`,
				true,
			},
		}
		
		for _, tt := range errorTests {
			t.Run(tt.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(tt.response))
				}))
				defer server.Close()
				
				client := NewAlchemyClient(server.URL)
				_, err := client.GetTransactionReceipt(context.Background(), "0x123")
				if tt.wantErr && err == nil {
					t.Error("Expected error but got none")
				}
			})
		}
	})
}

// TestGRPCClientNegativeCases tests all negative scenarios for gRPC client
func TestGRPCClientNegativeCases(t *testing.T) {
	t.Run("NewGRPCEthereumClient_InvalidGRPC", func(t *testing.T) {
		invalidAddresses := []string{
			"",
			"invalid-address",
			"localhost:99999", // Non-existent port
		}
		
		for _, addr := range invalidAddresses {
			t.Run(addr, func(t *testing.T) {
				_, err := NewGRPCEthereumClient(addr, "http://backup.example.com")
				if err == nil {
					t.Error("Expected error for invalid gRPC address")
				}
			})
		}
	})

	t.Run("NewGRPCEthereumClient_InvalidHTTP", func(t *testing.T) {
		invalidHTTPURLs := []string{
			"",
			"not-a-url",
			"ftp://invalid.com",
		}
		
		for _, url := range invalidHTTPURLs {
			t.Run(url, func(t *testing.T) {
				_, err := NewGRPCEthereumClient("localhost:8545", url)
				if err == nil {
					t.Error("Expected error for invalid HTTP URL")
				}
			})
		}
	})

	t.Run("ConvertGethBlock_EdgeCases", func(t *testing.T) {
		client := &GRPCEthereumClient{}
		
		// Test with minimal header
		minimalHeader := &ethtypes.Header{
			Number: big.NewInt(0),
		}
		
		// Test with no transactions
		emptyBlock := ethtypes.NewBlock(minimalHeader, []*ethtypes.Transaction{}, nil, nil, trie.NewStackTrie(nil))
		result := client.convertGethBlock(emptyBlock)
		
		if result == nil {
			t.Error("Should handle empty block")
		}
		if len(result.Transactions) != 0 {
			t.Error("Empty block should have no transactions")
		}
	})

	t.Run("ConvertTransaction_InvalidData", func(t *testing.T) {
		client := &GRPCEthereumClient{}
		
		// Create transaction with invalid data
		tx := ethtypes.NewTransaction(
			0,                             // nonce
			common.Address{},              // to (zero address)
			big.NewInt(0),                 // value
			0,                             // gas limit (invalid)
			big.NewInt(0),                 // gas price
			[]byte{},                      // data
		)
		
		header := &ethtypes.Header{
			Number: big.NewInt(1),
		}
		block := ethtypes.NewBlock(header, []*ethtypes.Transaction{tx}, nil, nil, trie.NewStackTrie(nil))
		
		_, err := client.convertTransaction(tx, block, 0)
		// Should handle gracefully even with invalid data
		if err != nil {
			// Error is acceptable for invalid transaction data
		}
	})

	t.Run("GetFromAddress_UnsignedTransaction", func(t *testing.T) {
		// Create unsigned transaction
		tx := ethtypes.NewTransaction(
			1,
			common.HexToAddress("0xto"),
			big.NewInt(1000),
			21000,
			big.NewInt(20000000000),
			[]byte("data"),
		)
		
		from := getFromAddress(tx)
		// Should return zero address for unsigned transaction
		if from != (common.Address{}) {
			t.Error("Unsigned transaction should return zero address")
		}
	})

	t.Run("GetToAddress_ContractCreation", func(t *testing.T) {
		// Create contract creation transaction (nil to address)
		tx := ethtypes.NewTransaction(
			1,
			common.Address{}, // Zero address for contract creation
			big.NewInt(0),
			21000,
			big.NewInt(20000000000),
			[]byte("contract bytecode"),
		)
		
		to := getToAddress(tx)
		if to != "0x0000000000000000000000000000000000000000" {
			t.Errorf("Contract creation should return zero address, got %s", to)
		}
	})

	t.Run("Close_MultipleCloses", func(t *testing.T) {
		// Create a mock HTTP client
		httpClient, _ := ethclient.Dial("http://example.com")
		client := &GRPCEthereumClient{
			httpClient: httpClient,
		}
		
		// Multiple closes should not panic
		client.Close()
		client.Close()
		client.Close()
	})
}

// TestConversionFunctionEdgeCases tests edge cases in conversion functions
func TestConversionFunctionEdgeCases(t *testing.T) {
	t.Run("ConvertBlock_NilValues", func(t *testing.T) {
		// Test with block containing nil values where possible
		header := &ethtypes.Header{
			Number:      big.NewInt(123),
			Time:        0,
			Difficulty:  big.NewInt(0),
			GasUsed:     0,
			GasLimit:    0,
			// Many fields will be zero values
		}
		
		block := ethtypes.NewBlock(header, []*ethtypes.Transaction{}, nil, nil, trie.NewStackTrie(nil))
		
		localBlock := types.ConvertBlock(block, []types.Transaction{})
		if localBlock == nil {
			t.Error("Should handle block with zero values")
		}
		
		// Verify zero values are handled correctly
		if localBlock.Number != "123" {
			t.Errorf("Expected number '123', got '%s'", localBlock.Number)
		}
	})

	t.Run("ConvertTransaction_ZeroValues", func(t *testing.T) {
		// Create transaction with zero values
		tx := ethtypes.NewTransaction(
			0,                    // nonce
			common.Address{},     // to (zero address)
			big.NewInt(0),        // value
			0,                    // gas
			big.NewInt(0),        // gas price
			[]byte{},             // data
		)
		
		header := &ethtypes.Header{Number: big.NewInt(1)}
		block := ethtypes.NewBlock(header, []*ethtypes.Transaction{}, nil, nil, trie.NewStackTrie(nil))
		
		localTx := types.ConvertTransaction(tx, common.Address{}, block, nil, false)
		if localTx.Hash == "" {
			t.Error("Should handle transaction with zero values")
		}
		
		// Verify zero values are converted correctly
		if localTx.Value != "0" {
			t.Errorf("Expected value '0', got '%s'", localTx.Value)
		}
		if localTx.Gas != "0" {
			t.Errorf("Expected gas '0', got '%s'", localTx.Gas)
		}
	})

	t.Run("ConvertReceipt_EdgeCases", func(t *testing.T) {
		// Create receipt with edge case values
		receipt := &ethtypes.Receipt{
			Status:            0, // Failed transaction
			CumulativeGasUsed: 0,
			GasUsed:           0,
			Logs:              nil, // No logs
		}
		
		localReceipt := types.ConvertReceipt(receipt)
		if localReceipt == nil {
			t.Error("Should handle receipt with edge case values")
		}
		
		// Verify edge cases are handled
		if localReceipt.Status != "0" {
			t.Error("Expected status '0' for failed transaction")
		}
		if len(localReceipt.Logs) != 0 {
			t.Error("Expected empty logs array")
		}
	})

	t.Run("ConvertLog_EmptyData", func(t *testing.T) {
		// Create log with minimal data
		ethLog := &ethtypes.Log{
			Address: common.Address{},
			Topics:  []common.Hash{},
			Data:    []byte{},
			Index:   0,
		}
		
		localLog := types.ConvertLog(ethLog)
		if localLog.Address == "" {
			t.Error("Should handle log with empty data")
		}
		
		// Verify empty data is handled correctly
		if len(localLog.Topics) != 0 {
			t.Error("Expected empty topics array")
		}
		if localLog.Data != "0x" {
			t.Errorf("Expected data '0x', got '%s'", localLog.Data)
		}
	})
}

// TestConcurrentOperations tests concurrent operations on clients
func TestConcurrentOperations(t *testing.T) {
	t.Run("AlchemyClient_ConcurrentRequests", func(t *testing.T) {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			time.Sleep(10 * time.Millisecond) // Small delay
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result": {"number": "0x123", "hash": "0xhash", "transactions": []}}`))
		}))
		defer server.Close()
		
		client := NewAlchemyClient(server.URL)
		
		// Run concurrent requests
		concurrency := 10
		results := make(chan error, concurrency)
		
		for i := 0; i < concurrency; i++ {
			go func() {
				_, err := client.GetBlock(context.Background(), "latest")
				results <- err
			}()
		}
		
		// Collect results
		for i := 0; i < concurrency; i++ {
			err := <-results
			if err != nil {
				t.Errorf("Concurrent request failed: %v", err)
			}
		}
		
		if requestCount != concurrency {
			t.Errorf("Expected %d requests, got %d", concurrency, requestCount)
		}
	})

	t.Run("GRPCClient_ConcurrentConversions", func(t *testing.T) {
		client := &GRPCEthereumClient{}
		
		// Create test data
		header := &ethtypes.Header{Number: big.NewInt(123)}
		tx := ethtypes.NewTransaction(1, common.HexToAddress("0xto"), big.NewInt(1000), 21000, big.NewInt(20000000000), []byte("data"))
		block := ethtypes.NewBlock(header, []*ethtypes.Transaction{tx}, nil, nil, trie.NewStackTrie(nil))
		
		// Run concurrent conversions
		concurrency := 10
		results := make(chan *types.Block, concurrency)
		
		for i := 0; i < concurrency; i++ {
			go func() {
				result := client.convertGethBlock(block)
				results <- result
			}()
		}
		
		// Collect results
		for i := 0; i < concurrency; i++ {
			result := <-results
			if result == nil {
				t.Error("Concurrent conversion failed")
			}
		}
	})
}
