package defra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type BlockHandler struct {
	defraURL string
	client   *http.Client
}

func NewBlockHandler(host string, port int) *BlockHandler {
	return &BlockHandler{
		defraURL: fmt.Sprintf("http://%s:%d/api/v0/graphql", host, port),
		client:   &http.Client{},
	}
}

type Block struct {
	Hash             string        `json:"hash"`
	Number           string        `json:"number"`
	Timestamp        string        `json:"timestamp"`
	ParentHash       string        `json:"parentHash"`
	Difficulty       string        `json:"difficulty"`
	GasUsed          string        `json:"gasUsed"`
	GasLimit         string        `json:"gasLimit"`
	Nonce            string        `json:"nonce"`
	Miner            string        `json:"miner"`
	Size             string        `json:"size"`
	StateRoot        string        `json:"stateRoot"`
	Sha3Uncles       string        `json:"sha3Uncles"`
	TransactionsRoot string        `json:"transactionsRoot"`
	ReceiptsRoot     string        `json:"receiptsRoot"`
	ExtraData        string        `json:"extraData"`
	Transactions     []Transaction `json:"transactions,omitempty"`
}

type Transaction struct {
	Hash             string `json:"hash"`
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	From             string `json:"from"`
	To               string `json:"to"`
	Value            string `json:"value"`
	Gas              string `json:"gas"`
	GasPrice         string `json:"gasPrice"`
	Input            string `json:"input"`
	Nonce            string `json:"nonce"`
	TransactionIndex string `json:"transactionIndex"`
	Status           bool   `json:"status"`
	Logs             []Log  `json:"logs,omitempty"`
}

type Log struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	BlockHash        string   `json:"blockHash"`
	LogIndex         string   `json:"logIndex"`
	Removed          bool     `json:"removed"`
	Events           []Event  `json:"events,omitempty"`
}

type Event struct {
	ContractAddress string `json:"contractAddress"`
	EventName       string `json:"eventName"`
	Parameters      string `json:"parameters"`
	TransactionHash string `json:"transactionHash"`
	BlockHash       string `json:"blockHash"`
	BlockNumber     string `json:"blockNumber"`
	TransactionIndex string `json:"transactionIndex"`
	LogIndex        string `json:"logIndex"`
}

type Response struct {
	Data map[string][]struct {
		DocID string `json:"_docID"`
	} `json:"data"`
}

// PostBlock posts a block and its nested objects to DefraDB
func (h *BlockHandler) PostBlock(ctx context.Context, block *Block) (string, error) {
	// Post block first
	blockID, err := h.createBlock(ctx, block)
	if err != nil {
		return "", fmt.Errorf("failed to create block: %w", err)
	}

	// Process transactions
	for _, tx := range block.Transactions {
		_, err := h.createTransaction(ctx, &tx)
		if err != nil {
			return "", fmt.Errorf("failed to create transaction: %w", err)
		}

		// Link transaction to block
		if err := h.updateTransactionRelationships(ctx, block.Hash, tx.Hash); err != nil {
			return "", fmt.Errorf("failed to update transaction relationships: %w", err)
		}

		// Process logs
		for _, log := range tx.Logs {
			_, err := h.createLog(ctx, &log)
			if err != nil {
				return "", fmt.Errorf("failed to create log: %w", err)
			}

			// Link log to transaction and block
			if err := h.updateLogRelationships(ctx, block.Hash, tx.Hash, log.LogIndex); err != nil {
				return "", fmt.Errorf("failed to update log relationships: %w", err)
			}

			// Process events
			for _, event := range log.Events {
				_, err := h.createEvent(ctx, &event)
				if err != nil {
					return "", fmt.Errorf("failed to create event: %w", err)
				}

				// Link event to log
				if err := h.updateEventRelationships(ctx, log.LogIndex, event.LogIndex); err != nil {
					return "", fmt.Errorf("failed to update event relationships: %w", err)
				}
			}
		}
	}

	return blockID, nil
}

func (h *BlockHandler) createBlock(ctx context.Context, block *Block) (string, error) {
	blockData := map[string]interface{}{
		"hash":             block.Hash,
		"number":           block.Number,
		"timestamp":        block.Timestamp,
		"parentHash":       block.ParentHash,
		"difficulty":       block.Difficulty,
		"gasUsed":          block.GasUsed,
		"gasLimit":         block.GasLimit,
		"nonce":            block.Nonce,
		"miner":            block.Miner,
		"size":             block.Size,
		"stateRoot":        block.StateRoot,
		"sha3Uncles":       block.Sha3Uncles,
		"transactionsRoot": block.TransactionsRoot,
		"receiptsRoot":     block.ReceiptsRoot,
		"extraData":        block.ExtraData,
	}

	return h.postToCollection(ctx, "Block", blockData)
}

func (h *BlockHandler) createTransaction(ctx context.Context, tx *Transaction) (string, error) {
	txData := map[string]interface{}{
		"hash":             tx.Hash,
		"blockHash":        tx.BlockHash,
		"blockNumber":      tx.BlockNumber,
		"from":             tx.From,
		"to":               tx.To,
		"value":            tx.Value,
		"gasUsed":          tx.Gas,           // Map Gas to gasUsed
		"gasPrice":         tx.GasPrice,
		"inputData":        tx.Input,         // Map Input to inputData
		"nonce":            tx.Nonce,
		"transactionIndex": tx.TransactionIndex,
	}

	return h.postToCollection(ctx, "Transaction", txData)
}

func (h *BlockHandler) createLog(ctx context.Context, log *Log) (string, error) {
	logData := map[string]interface{}{
		"address":          log.Address,
		"topics":           log.Topics,
		"data":             log.Data,
		"blockNumber":      log.BlockNumber,
		"transactionHash":  log.TransactionHash,
		"transactionIndex": log.TransactionIndex,
		"blockHash":        log.BlockHash,
		"logIndex":         log.LogIndex,
		"removed":          fmt.Sprintf("%v", log.Removed), // Convert bool to string
	}

	return h.postToCollection(ctx, "Log", logData)
}

func (h *BlockHandler) createEvent(ctx context.Context, event *Event) (string, error) {
	eventData := map[string]interface{}{
		"contractAddress":  event.ContractAddress,
		"eventName":        event.EventName,
		"parameters":       event.Parameters,
		"transactionHash":  event.TransactionHash,
		"blockHash":        event.BlockHash,
		"blockNumber":      event.BlockNumber,
		"transactionIndex": event.TransactionIndex,
		"logIndex":         event.LogIndex,
	}

	return h.postToCollection(ctx, "Event", eventData)
}

func (h *BlockHandler) updateTransactionRelationships(ctx context.Context, blockHash, txHash string) error {
	// Get block ID
	query := fmt.Sprintf(`query {
		Block(filter: {hash: {_eq: %q}}) {
			_docID
		}
	}`, blockHash)

	resp, err := h.postGraphQL(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to get block ID: %w", err)
	}

	var blockResp struct {
		Data struct {
			Block []struct {
				DocID string `json:"_docID"`
			}
		}
	}
	if err := json.Unmarshal(resp, &blockResp); err != nil {
		return fmt.Errorf("failed to decode block response: %w", err)
	}

	if len(blockResp.Data.Block) == 0 {
		return fmt.Errorf("block not found")
	}

	// Update transaction with block relationship
	mutation := fmt.Sprintf(`mutation {
		update_Transaction(filter: {hash: {_eq: %q}}, input: {block: %q}) {
			_docID
		}
	}`, txHash, blockResp.Data.Block[0].DocID)

	_, err = h.postGraphQL(ctx, mutation)
	if err != nil {
		return fmt.Errorf("failed to update transaction relationships: %w", err)
	}

	return nil
}

func (h *BlockHandler) updateLogRelationships(ctx context.Context, blockHash, txHash, logIndex string) error {
	// Get block and transaction IDs
	query := fmt.Sprintf(`query {
		Block(filter: {hash: {_eq: %q}}) {
			_docID
		}
		Transaction(filter: {hash: {_eq: %q}}) {
			_docID
		}
	}`, blockHash, txHash)

	resp, err := h.postGraphQL(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to get IDs: %w", err)
	}

	var idResp struct {
		Data struct {
			Block []struct {
				DocID string `json:"_docID"`
			}
			Transaction []struct {
				DocID string `json:"_docID"`
			}
		}
	}
	if err := json.Unmarshal(resp, &idResp); err != nil {
		return fmt.Errorf("failed to decode ID response: %w", err)
	}

	if len(idResp.Data.Block) == 0 || len(idResp.Data.Transaction) == 0 {
		return fmt.Errorf("block or transaction not found")
	}

	// Update log with block and transaction relationships
	mutation := fmt.Sprintf(`mutation {
		update_Log(filter: {logIndex: {_eq: %q}}, input: {
			block: %q,
			transaction: %q
		}) {
			_docID
		}
	}`, logIndex, idResp.Data.Block[0].DocID, idResp.Data.Transaction[0].DocID)

	_, err = h.postGraphQL(ctx, mutation)
	if err != nil {
		return fmt.Errorf("failed to update log relationships: %w", err)
	}

	return nil
}

func (h *BlockHandler) updateEventRelationships(ctx context.Context, logIndex, eventLogIndex string) error {
	// Get log ID
	query := fmt.Sprintf(`query {
		Log(filter: {logIndex: {_eq: %q}}) {
			_docID
		}
	}`, logIndex)

	resp, err := h.postGraphQL(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to get log ID: %w", err)
	}

	var logResp struct {
		Data struct {
			Log []struct {
				DocID string `json:"_docID"`
			}
		}
	}
	if err := json.Unmarshal(resp, &logResp); err != nil {
		return fmt.Errorf("failed to decode log response: %w", err)
	}

	if len(logResp.Data.Log) == 0 {
		return fmt.Errorf("log not found")
	}

	// Update event with log relationship
	mutation := fmt.Sprintf(`mutation {
		update_Event(filter: {logIndex: {_eq: %q}}, input: {log: %q}) {
			_docID
		}
	}`, eventLogIndex, logResp.Data.Log[0].DocID)

	_, err = h.postGraphQL(ctx, mutation)
	if err != nil {
		return fmt.Errorf("failed to update event relationships: %w", err)
	}

	return nil
}

func (h *BlockHandler) postToCollection(ctx context.Context, collection string, data map[string]interface{}) (string, error) {
	// Convert data to GraphQL input format
	var inputFields []string
	for key, value := range data {
		switch v := value.(type) {
		case string:
			inputFields = append(inputFields, fmt.Sprintf("%s: %q", key, v))
		case bool:
			inputFields = append(inputFields, fmt.Sprintf("%s: %v", key, v))
		case []string:
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				return "", fmt.Errorf("failed to marshal field %s: %w", key, err)
			}
			inputFields = append(inputFields, fmt.Sprintf("%s: %s", key, string(jsonBytes)))
		default:
			inputFields = append(inputFields, fmt.Sprintf("%s: %q", key, fmt.Sprint(v)))
		}
	}
	log.Printf("Input fields: %s\n", strings.Join(inputFields, ", "))
	log.Printf("Collection: %s\n", collection)
	log.Printf("Mutation: %s\n", fmt.Sprintf(`mutation {
		create_%s(input: { %s }) {
			_docID
		}
	}`, collection, strings.Join(inputFields, ", ")))
	log.Printf("Http: %s\n", h.defraURL)
	// Create mutation
	mutation := fmt.Sprintf(`mutation {
		create_%s(input: { %s }) {
			_docID
		}
	}`, collection, strings.Join(inputFields, ", "))

	// Debug: Print the mutation
	fmt.Printf("Sending mutation: %s\n", mutation)

	// Send mutation
	resp, err := h.postGraphQL(ctx, mutation)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", collection, err)
	}

	// Parse response
	var response Response
	if err := json.Unmarshal(resp, &response); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Get document ID
	createField := fmt.Sprintf("create_%s", collection)
	items, ok := response.Data[createField]
	if !ok || len(items) == 0 {
		return "", fmt.Errorf("no document ID returned")
	}

	return items[0].DocID, nil
}

func (h *BlockHandler) postGraphQL(ctx context.Context, mutation string) ([]byte, error) {
	// Create request body
	body := map[string]string{
		"query": mutation,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Debug: Print the mutation
	fmt.Printf("Sending mutation: %s\n", mutation)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", h.defraURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Debug: Print the response
	fmt.Printf("DefraDB Response: %s\n", string(respBody))

	return respBody, nil
}
