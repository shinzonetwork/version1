package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"shinzo/version1/pkg/types"
)

type AlchemyClient struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

func NewAlchemyClient(apiKey string) *AlchemyClient {
	return &AlchemyClient{
		client:  &http.Client{},
		baseURL: "https://eth-mainnet.alchemyapi.io/v2",
		apiKey:  apiKey,
	}
}

func (c *AlchemyClient) GetBlock(ctx context.Context, blockNumber string) (*types.Block, error) {
	payload := fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"method": "eth_getBlockByNumber",
		"params": ["%s", true],
		"id": 1
	}`, blockNumber)
	resp, err := c.post(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result *types.Block `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Result, nil
}

func (c *AlchemyClient) GetTransactionReceipt(ctx context.Context, txHash string) (*types.TransactionReceipt, error) {
	payload := fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"method": "eth_getTransactionReceipt",
		"params": ["%s"],
		"id": 1
	}`, txHash)

	resp, err := c.post(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result *types.TransactionReceipt `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Result, nil
}

func (c *AlchemyClient) post(ctx context.Context, payload string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("%s/%s", c.baseURL, c.apiKey),
		strings.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}
