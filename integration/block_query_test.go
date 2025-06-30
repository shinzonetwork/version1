//go:build integration
// +build integration

package integration

import (
	"path/filepath"
	"testing"
)

const queryFile = "queries/blocks.graphql"

var queryPath string

func init() {
	// Initialize queryPath once for all tests
	queryPath = filepath.Join(getProjectRoot(nil), queryFile)
}

// Helper to get the latest block number
func getLatestBlockNumber(t *testing.T) int {
	query, err := loadGraphQLQuery(queryPath, "GetHighestBlockNumber")
	if err != nil {
		t.Fatalf("Failed to load query: %v", err)
	}
	result := postGraphQLQuery(t, query, nil)
	blockList, ok := result["data"].(map[string]interface{})["Block"].([]interface{})
	if !ok || len(blockList) == 0 {
		t.Fatalf("No blocks returned: %v", result)
	}
	num, ok := blockList[0].(map[string]interface{})["number"]
	if !ok {
		t.Fatalf("Block missing number field: %v", blockList[0])
	}
	n, ok := num.(float64)
	if !ok {
		t.Fatalf("Block number is not a number: %v", num)
	}
	return int(n)
}

func TestGetHighestBlockNumber(t *testing.T) {
	_ = getLatestBlockNumber(t) // Just check we can get it
}

func TestGetLatestBlocks(t *testing.T) {
	query, err := loadGraphQLQuery(queryPath, "GetLatestBlocks")
	if err != nil {
		t.Fatalf("Failed to load query: %v", err)
	}
	result := postGraphQLQuery(t, query, nil)
	blockList, ok := result["data"].(map[string]interface{})["Block"].([]interface{})
	if !ok {
		t.Fatalf("No Block field or wrong type in response: %v", result)
	}
	if len(blockList) == 0 {
		t.Fatalf("No blocks returned")
	}
	for _, b := range blockList {
		block := b.(map[string]interface{})
		if _, ok := block["hash"]; !ok {
			t.Errorf("Block missing hash field")
		}
		if _, ok := block["number"]; !ok {
			t.Errorf("Block missing number field")
		}
	}
}

func TestGetBlockWithTransactions(t *testing.T) {
	blockNumber := getLatestBlockNumber(t)
	query, err := loadGraphQLQuery(queryPath, "GetBlockWithTransactions")
	if err != nil {
		t.Fatalf("Failed to load query: %v", err)
	}
	variables := map[string]interface{}{"blockNumber": blockNumber}
	result := postGraphQLQuery(t, query, variables)
	blockList, ok := result["data"].(map[string]interface{})["Block"].([]interface{})
	if !ok || len(blockList) == 0 {
		t.Fatalf("No block with number %v found; cannot test transactions.", blockNumber)
	}
	block := blockList[0].(map[string]interface{})
	if _, ok := block["transactions"]; !ok {
		t.Errorf("Block missing transactions field")
	}
}
