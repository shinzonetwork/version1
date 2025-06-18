package defra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"shinzo/version1/pkg/types"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

type BlockHandler struct {
	defraURL string
	client   *http.Client
}

type FatalError interface {
	err() string
}

func NewBlockHandler(host string, port int) *BlockHandler {
	return &BlockHandler{
		defraURL: fmt.Sprintf("http://%s:%d/api/v0/graphql", host, port),
		client:   &http.Client{},
	}
}

func (h *BlockHandler) ConvertHexToInt(s string, sugar *zap.SugaredLogger) int64 {
	block16 := s[2:]
	blockInt, err := strconv.ParseInt(block16, 16, 64)
	if blockInt != 0 {
		return blockInt
	}
	sugar.Fatalf("Failed to ParseInt(", err, ")")
	return 0

}

func (h *BlockHandler) CreateBlock(ctx context.Context, block *types.Block, sugar *zap.SugaredLogger) string {
	// Convert string number to int
	blockInt, err := strconv.ParseInt(block.Number, 0, 64)
	if err != nil {
		sugar.Fatalf("failed to parse block number: %w", err)
	}

	blockData := map[string]interface{}{
		"hash":             block.Hash,
		"number":           blockInt,
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
	sugar.Debug("Posting blockdata to collection endpoint: ", blockData)
	return h.PostToCollection(ctx, "Block", blockData, sugar)
}

func (h *BlockHandler) CreateTransaction(ctx context.Context, tx *types.Transaction, block_id string, sugar *zap.SugaredLogger) string {
	blockInt, err := strconv.ParseInt(tx.BlockNumber, 0, 64)
	if err != nil {
		sugar.Fatalf("failed to parse block number: ", err)
	}

	txData := map[string]interface{}{
		"hash":             tx.Hash,
		"blockHash":        tx.BlockHash,
		"blockNumber":      blockInt,
		"from":             tx.From,
		"to":               tx.To,
		"value":            tx.Value,
		"gasUsed":          tx.Gas,
		"gasPrice":         tx.GasPrice,
		"inputData":        tx.Input,
		"nonce":            tx.Nonce,
		"transactionIndex": tx.TransactionIndex,
	}
	sugar.Debug("Creating transaction: ", txData)
	return h.PostToCollection(ctx, "Transaction", txData, sugar)
}

func (h *BlockHandler) CreateLog(ctx context.Context, log *types.Log, block_id, tx_Id string, sugar *zap.SugaredLogger) string {
	blockInt, err := strconv.ParseInt(log.BlockNumber, 0, 64)
	if err != nil {
		sugar.Fatalf("failed to parse block number: ", err)
	}

	logData := map[string]interface{}{
		"address":          log.Address,
		"topics":           log.Topics,
		"data":             log.Data,
		"blockNumber":      blockInt,
		"transactionHash":  log.TransactionHash,
		"transactionIndex": log.TransactionIndex,
		"blockHash":        log.BlockHash,
		"logIndex":         log.LogIndex,
		"removed":          fmt.Sprintf("%v", log.Removed), // Convert bool to string
		"transaction_id":   tx_Id,
		"block_id":         block_id,
	}
	return h.PostToCollection(ctx, "Log", logData, sugar)
}

func (h *BlockHandler) CreateEvent(ctx context.Context, event *types.Event, log_id string, sugar *zap.SugaredLogger) string {
	blockInt, err := strconv.ParseInt(event.BlockNumber, 0, 64)
	if err != nil {
		sugar.Errorf("failed to parse block number: %w", err)
		return ""
	}

	eventData := map[string]interface{}{
		"contractAddress":  event.ContractAddress,
		"eventName":        event.EventName,
		"parameters":       event.Parameters,
		"transactionHash":  event.TransactionHash,
		"blockHash":        event.BlockHash,
		"blockNumber":      blockInt,
		"transactionIndex": event.TransactionIndex,
		"logIndex":         event.LogIndex,
		"log_id":           log_id,
	}
	return h.PostToCollection(ctx, "Event", eventData, sugar)
}

func (h *BlockHandler) UpdateTransactionRelationships(ctx context.Context, blockId string, txHash string, sugar *zap.SugaredLogger) string {

	// Update transaction with block relationship
	mutation := types.Request{Query: fmt.Sprintf(`mutation {
		update_Transaction(filter: {hash: {_eq: %q}}, input: {block: %q}) {
			_docID
		}
	}`, txHash, blockId)}

	docId := h.SendToGraphql(ctx, mutation, sugar)
	if docId == nil {
		sugar.Errorf("failed to update transaction relationships: ", mutation)
		return ""
	}

	return string(docId)
}

// shinzo stuct
// alchemy client interface
// call start and measure what i am storing in defra
// mock alchemy block data { alter diff fields to create diff scenarios}

func (h *BlockHandler) UpdateLogRelationships(ctx context.Context, blockId string, txId string, txHash string, logIndex string, sugar *zap.SugaredLogger) string {

	// Update log with block and transaction relationships
	mutation := types.Request{Query: fmt.Sprintf(`mutation {
		update_Log(filter: {logIndex: {_eq: %q}, transactionHash: {_eq: %q}}, input: {
			block: %q,
			transaction: %q
		}) {
			_docID
		}
	}`, logIndex, txHash, blockId, txId)}

	docId := h.SendToGraphql(ctx, mutation, sugar)
	if docId == nil {
		sugar.Warn("log relationship update failure")
		return ""
	}
	return string(docId)
}

func (h *BlockHandler) UpdateEventRelationships(ctx context.Context, logDocId string, txHash string, logIndex string, sugar *zap.SugaredLogger) string {
	// Update event with log relationship
	mutation := types.Request{Query: fmt.Sprintf(`mutation {
		update_Event(filter: {logIndex: {_eq: %q}, transactionHash:{_eq:%q}}, input: {
		log: %q
		}) {
			_docID
		}
	}`, logIndex, txHash, logDocId)}

	docId := h.SendToGraphql(ctx, mutation, sugar)
	if docId == nil {
		sugar.Warn("log relationship update failure")
		return ""
	}
	return string(docId)
}

func (h *BlockHandler) PostToCollection(ctx context.Context, collection string, data map[string]interface{}, sugar *zap.SugaredLogger) string {
	// Convert data to GraphQL input format
	var inputFields []string
	for key, value := range data {
		switch v := value.(type) {
		case string:
			inputFields = append(inputFields, fmt.Sprintf("%s: %q", key, v))
		case bool:
			inputFields = append(inputFields, fmt.Sprintf("%s: %v", key, v))
		case int, int64:
			inputFields = append(inputFields, fmt.Sprintf("%s: %d", key, v))
		case []string:
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				sugar.Fatalf("failed to marshal field ", key, "err: ", err)
				return ""
			}
			inputFields = append(inputFields, fmt.Sprintf("%s: %s", key, string(jsonBytes)))
		default:
			inputFields = append(inputFields, fmt.Sprintf("%s: %q", key, fmt.Sprint(v)))
		}
	}

	// Create mutation
	mutation := types.Request{
		Type: "POST",
		Query: fmt.Sprintf(`mutation {
		create_%s(input: { %s }) {
			_docID
		}
	}`, collection, strings.Join(inputFields, ", "))}

	// Send mutation
	resp := h.SendToGraphql(ctx, mutation, sugar)
	if resp == nil {
		sugar.Error("Received nil response from GraphQL")
		return ""
	}

	// Parse response
	var response types.Response
	if err := json.Unmarshal(resp, &response); err != nil {
		sugar.Errorf("failed to decode response: %v", err)
		sugar.Debug("Raw response: ", string(resp))
		return ""
	}

	// Get document ID
	createField := fmt.Sprintf("create_%s", collection)
	items, ok := response.Data[createField]
	if !ok {
		sugar.Errorf("create_", collection, " field not found in response")
		sugar.Debug("Response data: ", response.Data)
		return ""
	}
	if len(items) == 0 {
		sugar.Warnf("no document ID returned for create_", collection)
		return ""
	}
	return items[0].DocID
}

// Graph golang client check in defra

func (h *BlockHandler) SendToGraphql(ctx context.Context, req types.Request, sugar *zap.SugaredLogger) []byte {

	type RequestJSON struct {
		Query string `json:"query"`
	}

	// Create request body
	body := RequestJSON{req.Query}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		sugar.Errorf("failed to marshal request body: ", err)
	}

	// Debug: Print the mutation
	sugar.Debug("Sending mutation: ", req.Query, "\n")

	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, req.Type, h.defraURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		sugar.Errorf("failed to create request: ", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Send request
	resp, err := h.client.Do(httpReq)
	if err != nil {
		sugar.Errorf("failed to send request: %v", err)
		return nil // Prevent panic if resp is nil
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		sugar.Errorf("Read response error: ", err) // todo turn to error interface
	}
	// Debug: Print the response
	sugar.Debug("DefraDB Response: ", string(respBody), "\n")
	return respBody
}

// GetHighestBlockNumber returns the highest block number stored in DefraDB
func (h *BlockHandler) GetHighestBlockNumber(ctx context.Context, sugar *zap.SugaredLogger) int64 {
	query := types.Request{
		Type: "POST",
		Query: `query {
		Block(order: {number: DESC}, limit: 1) {
			number
		}	
	}`}

	resp := h.SendToGraphql(ctx, query, sugar)
	if resp == nil {
		sugar.Errorf("failed to query block numbers error: ", resp)
	}

	var result struct {
		Data struct {
			Block []struct {
				Number int64 `json:"number"`
			} `json:"Block"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		sugar.Errorf("failed to decode response: ", err)
	}

	if len(result.Data.Block) == 0 {
		return 0 // Return 0 if no blocks exist
	}

	// Return the highest block number + 1
	return result.Data.Block[0].Number + 1
}
