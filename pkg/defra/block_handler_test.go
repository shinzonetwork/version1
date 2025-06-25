package defra

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"shinzo/version1/pkg/testutils"
	"shinzo/version1/pkg/types"
	"strings"
	"testing"

	"net/http/httptest"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// createBlockHandlerWithMocksConfig creates a mock server and returns it along with a BlockHandler configured to use it, using a custom MockServerConfig.
func createBlockHandlerWithMocksConfig(config testutils.MockServerConfig) (*httptest.Server, *BlockHandler) {
	server := testutils.CreateMockServer(config)
	handler := &BlockHandler{
		defraURL: server.URL,
		client:   &http.Client{},
	}
	return server, handler
}

// createBlockHandlerWithMocks creates a mock server and returns it along with a BlockHandler configured to use it (simple version).
func createBlockHandlerWithMocks(response string) (*httptest.Server, *BlockHandler) {
	return createBlockHandlerWithMocksConfig(testutils.DefaultMockServerConfig(response))
}

func TestNewBlockHandler(t *testing.T) {
	host := "localhost"
	port := 9181

	handler := NewBlockHandler(host, port)

	if handler == nil {
		t.Fatal("NewBlockHandler should not return nil")
	}

	expectedURL := "http://localhost:9181/api/v0/graphql"
	if handler.defraURL != expectedURL {
		t.Errorf("Expected defraURL %s, got %s", expectedURL, handler.defraURL)
	}

	if handler.client == nil {
		t.Error("HTTP client should not be nil")
	}
}

func TestConvertHexToInt(t *testing.T) {
	// Create a test logger
	logger := zap.NewNop().Sugar()
	handler := NewBlockHandler("localhost", 9181)

	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"Simple hex", "0x1", 1},
		{"Larger hex", "0xff", 255},
		{"Zero", "0x0", 0},
		{"Large number", "0x1000", 4096},
		{"Block number", "0x1234", 4660},
		{"All characters, lowercase", "0x1234567890abcdef", 1311768467294899695},
		{"All characters, uppercase", "0x1234567890ABCDEF", 1311768467294899695},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.ConvertHexToInt(tt.input, logger)
			if result != tt.expected {
				t.Errorf("ConvertHexToInt(%s) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConvertHexToInt_UnhappyPaths(t *testing.T) {
	// Create a test logger
	logger, buffer := newTestLogger()
	handler := NewBlockHandler("localhost", 9181)

	tests := []struct {
		name        string
		input       string
		expectedLog string
	}{
		{"Empty string", "", "Empty hex string provided"},
		{"Invalid hex", "invalid hex", "Failed to parse hex string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.ConvertHexToInt(tt.input, logger)
			if result != 0 {
				t.Errorf("ConvertHexToInt(%s) = %d, want %d", tt.input, result, 0)
			}
			logs := buffer.String()
			if !strings.Contains(logs, tt.expectedLog) {
				t.Errorf("Expected log to contain error message '%s', got: %s", tt.expectedLog, logs)
			}
		})
	}
}

func newTestLogger() (*zap.SugaredLogger, *bytes.Buffer) {
	buf := new(bytes.Buffer)
	encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	core := zapcore.NewCore(encoder, zapcore.AddSync(buf), zapcore.DebugLevel)
	logger := zap.New(core)
	return logger.Sugar(), buf
}

func TestCreateBlock_MockServer(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Block", "test-block-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	block := &types.Block{
		Hash:         "0x1234567890abcdef",
		Number:       "12345",
		Timestamp:    "1600000000",
		ParentHash:   "0xabcdef1234567890",
		Difficulty:   "1000000",
		GasUsed:      "4000000",
		GasLimit:     "8000000",
		Nonce:        "123456789",
		Miner:        "0xminer",
		Size:         "1024",
		StateRoot:    "0xstateroot",
		Sha3Uncles:   "0xsha3uncles",
		ReceiptsRoot: "0xreceiptsroot",
		ExtraData:    "extra",
	}

	docID := handler.CreateBlock(context.Background(), block, logger)

	if docID != "test-block-doc-id" {
		t.Errorf("Expected docID 'test-block-doc-id', got '%s'", docID)
	}
}

func TestCreateBlock_InvalidBlock(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Block", "test-block-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()

	block := &types.Block{
		Hash:         "0x1234567890abcdef",
		Number:       "invalid block number",
		Timestamp:    "1600000000",
		ParentHash:   "0xabcdef1234567890",
		Difficulty:   "1000000",
		GasUsed:      "4000000",
		GasLimit:     "8000000",
		Nonce:        "123456789",
		Miner:        "0xminer",
		Size:         "1024",
		StateRoot:    "0xstateroot",
		Sha3Uncles:   "0xsha3uncles",
		ReceiptsRoot: "0xreceiptsroot",
		ExtraData:    "extra",
	}

	docID := handler.CreateBlock(context.Background(), block, logger)

	if docID != "" {
		t.Error("Expected an error; should've received null response")
	}

	expected := "failed to parse block number"
	logs := buffer.String()
	if !strings.Contains(logs, expected) {
		t.Errorf("Got unexpected error message: %s | expected should contain: %s", logs, expected)
	}
}

func TestCreateBlock_InvalidJSON(t *testing.T) {
	response := "not a json"
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()
	block := &types.Block{Hash: "0x1", Number: "1"}
	result := handler.CreateBlock(context.Background(), block, logger)
	if result != "" {
		t.Errorf("Expected empty string for invalid JSON, got '%s'", result)
	}
}

func TestCreateBlock_MissingField(t *testing.T) {
	response := `{"data": {}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()
	block := &types.Block{Hash: "0x1", Number: "1"}
	result := handler.CreateBlock(context.Background(), block, logger)
	if result != "" {
		t.Errorf("Expected empty string for missing field, got '%s'", result)
	}
}

func TestCreateBlock_EmptyField(t *testing.T) {
	response := `{"data": {"create_Block": []}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()
	block := &types.Block{Hash: "0x1", Number: "1"}
	result := handler.CreateBlock(context.Background(), block, logger)
	if result != "" {
		t.Errorf("Expected empty string for empty field, got '%s'", result)
	}
}

func TestCreateTransaction_MockServer(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Transaction", "test-tx-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	tx := &types.Transaction{
		Hash:             "0xtxhash",
		BlockHash:        "0xblockhash",
		BlockNumber:      "12345",
		From:             "0xfrom",
		To:               "0xto",
		Value:            "1000",
		Gas:              "21000",
		GasPrice:         "20000000000",
		Input:            "0xinput",
		Nonce:            "1",
		TransactionIndex: "0",
		Status:           true,
	}

	blockID := "test-block-id"
	docID := handler.CreateTransaction(context.Background(), tx, blockID, logger)

	if docID != "test-tx-doc-id" {
		t.Errorf("Expected docID 'test-tx-doc-id', got '%s'", docID)
	}
}

func TestCreateTransaction_InvalidBlockNumber(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Transaction", "test-tx-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()

	tx := &types.Transaction{
		Hash:             "0xtxhash",
		BlockHash:        "0xblockhash",
		BlockNumber:      "invalid block number",
		From:             "0xfrom",
		To:               "0xto",
		Value:            "1000",
		Gas:              "21000",
		GasPrice:         "20000000000",
		Input:            "0xinput",
		Nonce:            "1",
		TransactionIndex: "0",
		Status:           true,
	}

	blockID := "test-block-id"
	docID := handler.CreateTransaction(context.Background(), tx, blockID, logger)

	if docID != "" {
		t.Error("Expected an error; should've received null response")
	}

	expected := "failed to parse block number"
	logs := buffer.String()
	if !strings.Contains(logs, expected) {
		t.Errorf("Got unexpected error message: %s | expected should contain: %s", logs, expected)
	}
}

func TestCreateLog_MockServer(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Log", "test-log-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	log := &types.Log{
		Address:          "0xcontract",
		Topics:           []string{"0xtopic1", "0xtopic2"},
		Data:             "0xlogdata",
		BlockNumber:      "12345",
		TransactionHash:  "0xtxhash",
		TransactionIndex: "0",
		BlockHash:        "0xblockhash",
		LogIndex:         "0",
		Removed:          false,
	}

	blockID := "test-block-id"
	txID := "test-tx-id"

	docID := handler.CreateLog(context.Background(), log, blockID, txID, logger)

	if docID != "test-log-doc-id" {
		t.Errorf("Expected docID 'test-log-doc-id', got '%s'", docID)
	}
}

func TestCreateLog_InvalidBlockNumber(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Log", "test-log-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()

	logEntry := &types.Log{
		Address:          "0xcontract",
		Topics:           []string{"0xtopic1", "0xtopic2"},
		Data:             "0xlogdata",
		BlockNumber:      "invalid block number",
		TransactionHash:  "0xtxhash",
		TransactionIndex: "0",
		BlockHash:        "0xblockhash",
		LogIndex:         "0",
		Removed:          false,
	}

	blockID := "test-block-id"
	txID := "test-tx-id"

	docID := handler.CreateLog(context.Background(), logEntry, blockID, txID, logger)

	if docID != "" {
		t.Error("Expected an error; should've received null response")
	}

	expected := "failed to parse block number"
	logs := buffer.String()
	if !strings.Contains(logs, expected) {
		t.Errorf("Got unexpected error message: %s | expected should contain: %s", logs, expected)
	}
}

func TestCreateEvent_MockServer(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Event", "test-event-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	event := &types.Event{
		ContractAddress:  "0xcontract",
		EventName:        "Transfer",
		Parameters:       "0xeventdata",
		TransactionHash:  "0xtxhash",
		BlockHash:        "0xblockhash",
		BlockNumber:      "12345",
		TransactionIndex: "0",
		LogIndex:         "0",
	}

	logID := "test-log-id"

	docID := handler.CreateEvent(context.Background(), event, logID, logger)

	if docID != "test-event-doc-id" {
		t.Errorf("Expected docID 'test-event-doc-id', got '%s'", docID)
	}
}

func TestCreateEvent_InvalidBlockNumber(t *testing.T) {
	response := testutils.CreateGraphQLCreateResponse("Event", "test-event-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()

	event := &types.Event{
		ContractAddress:  "0xcontract",
		EventName:        "Transfer",
		Parameters:       "0xeventdata",
		TransactionHash:  "0xtxhash",
		BlockHash:        "0xblockhash",
		BlockNumber:      "invalid block number",
		TransactionIndex: "0",
		LogIndex:         "0",
	}

	logID := "test-log-id"

	docID := handler.CreateEvent(context.Background(), event, logID, logger)

	if docID != "" {
		t.Error("Expected an error; should've received null response")
	}

	expected := "failed to parse block number"
	logs := buffer.String()
	if !strings.Contains(logs, expected) {
		t.Errorf("Got unexpected error message: %s | expected should contain: %s", logs, expected)
	}
}

func TestUpdateTransactionRelationships_MockServerSuccess(t *testing.T) {
	response := testutils.CreateGraphQLUpdateResponse("Transaction", "updated-tx-doc-id")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	blockID := "test-block-id"
	txHash := "0xtxhash"

	docID := handler.UpdateTransactionRelationships(context.Background(), blockID, txHash, logger)

	if docID != "updated-tx-doc-id" {
		t.Errorf("Expected docID 'updated-tx-doc-id', got '%s'", docID)
	}
}

func TestUpdateTransactionRelationships_InvalidJSON(t *testing.T) {
	response := "not a json"
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()
	result := handler.UpdateTransactionRelationships(context.Background(), "blockId", "txHash", logger)
	if result != "" {
		t.Errorf("Expected empty string for invalid JSON, got '%s'", result)
	}
}

func TestUpdateTransactionRelationships_MissingField(t *testing.T) {
	response := `{"data": {}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()
	result := handler.UpdateTransactionRelationships(context.Background(), "blockId", "txHash", logger)
	if result != "" {
		t.Errorf("Expected empty string for missing field, got '%s'", result)
	}
}

func TestUpdateTransactionRelationships_EmptyField(t *testing.T) {
	response := `{"data": {"update_Transaction": []}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()
	result := handler.UpdateTransactionRelationships(context.Background(), "blockId", "txHash", logger)
	if result != "" {
		t.Errorf("Expected empty string for empty field, got '%s'", result)
	}
}

func TestUpdateTransactionRelationships_NilResponse(t *testing.T) {
	server, handler := createBlockHandlerWithMocks(`{"data": {}}`)
	server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateTransactionRelationships(context.Background(), "blockId", "txHash", logger)
	if result != "" {
		t.Error("Expected empty string for nil response")
	}
	if !strings.Contains(buffer.String(), "failed to update transaction relationships") {
		t.Errorf("Expected log to mention failed to update transaction relationships, got: %s", buffer.String())
	}
}

func TestUpdateLogRelationships_MockServerSuccess(t *testing.T) {
	response := `{"data": {"update_Log": [{"_docID": "log-doc-id"}]}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, _ := newTestLogger()
	result := handler.UpdateLogRelationships(context.Background(), "blockId", "txId", "txHash", "logIndex", logger)
	if result != "log-doc-id" {
		t.Errorf("Expected 'log-doc-id', got '%s'", result)
	}
}

func TestUpdateLogRelationships_InvalidJSON(t *testing.T) {
	response := "not a json"
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateLogRelationships(context.Background(), "blockId", "txId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for invalid JSON")
	}
	if !strings.Contains(buffer.String(), "failed to decode response") {
		t.Errorf("Expected log to contain decode error, got: %s", buffer.String())
	}
}

func TestUpdateLogRelationships_MissingField(t *testing.T) {
	response := `{"data": {}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateLogRelationships(context.Background(), "blockId", "txId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for missing field")
	}
	if !strings.Contains(buffer.String(), "update_Log field not found in response") {
		t.Errorf("Expected log to mention missing field, got: %s", buffer.String())
	}
}

func TestUpdateLogRelationships_EmptyField(t *testing.T) {
	response := `{"data": {"update_Log": []}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateLogRelationships(context.Background(), "blockId", "txId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for empty field")
	}
	if !strings.Contains(buffer.String(), "no document ID returned for update_Log") {
		t.Errorf("Expected log to mention no document ID, got: %s", buffer.String())
	}
}

func TestUpdateLogRelationships_NilResponse(t *testing.T) {
	server, handler := createBlockHandlerWithMocks(`{"data": {}}`)
	server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateLogRelationships(context.Background(), "blockId", "txId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for nil response")
	}
	if !strings.Contains(buffer.String(), "log relationship update failure") {
		t.Errorf("Expected log to mention relationship update failure, got: %s", buffer.String())
	}
}

func TestUpdateEventRelationships_MockServerSuccess(t *testing.T) {
	response := `{"data": {"update_Event": [{"_docID": "event-doc-id"}]}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, _ := newTestLogger()
	result := handler.UpdateEventRelationships(context.Background(), "logDocId", "txHash", "logIndex", logger)
	if result != "event-doc-id" {
		t.Errorf("Expected 'event-doc-id', got '%s'", result)
	}
}

func TestUpdateEventRelationships_InvalidJSON(t *testing.T) {
	response := "not a json"
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateEventRelationships(context.Background(), "logDocId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for invalid JSON")
	}
	if !strings.Contains(buffer.String(), "failed to decode response") {
		t.Errorf("Expected log to contain decode error, got: %s", buffer.String())
	}
}

func TestUpdateEventRelationships_MissingField(t *testing.T) {
	response := `{"data": {}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateEventRelationships(context.Background(), "logDocId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for missing field")
	}
	if !strings.Contains(buffer.String(), "update_Event field not found in response") {
		t.Errorf("Expected log to mention missing field, got: %s", buffer.String())
	}
}

func TestUpdateEventRelationships_EmptyField(t *testing.T) {
	response := `{"data": {"update_Event": []}}`
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateEventRelationships(context.Background(), "logDocId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for empty field")
	}
	if !strings.Contains(buffer.String(), "no document ID returned for update_Event") {
		t.Errorf("Expected log to mention no document ID, got: %s", buffer.String())
	}
}

func TestUpdateEventRelationships_NilResponse(t *testing.T) {
	server, handler := createBlockHandlerWithMocks(`{"data": {}}`)
	server.Close()

	logger, buffer := newTestLogger()
	result := handler.UpdateEventRelationships(context.Background(), "logDocId", "txHash", "logIndex", logger)
	if result != "" {
		t.Error("Expected empty string for nil response")
	}
	if !strings.Contains(buffer.String(), "event relationship update failure") {
		t.Errorf("Expected log to mention relationship update failure, got: %s", buffer.String())
	}
}

func TestPostToCollection_Success(t *testing.T) {
	config := testutils.MockServerConfig{
		ResponseBody: testutils.CreateGraphQLCreateResponse("TestCollection", "test-doc-id"),
		StatusCode:   http.StatusOK,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		ValidateRequest: func(r *http.Request) error {
			if r.Method != "POST" {
				return fmt.Errorf("Expected POST request, got %s", r.Method)
			}
			contentType := r.Header.Get("Content-Type")
			if contentType != "application/json" {
				return fmt.Errorf("Expected Content-Type application/json, got %s", contentType)
			}
			return nil
		},
	}
	server, handler := createBlockHandlerWithMocksConfig(config)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	data := map[string]interface{}{
		"string":      "value1",
		"number":      123,
		"bool":        true,
		"stringArray": []string{"dog", "cat", "bearded dragon"},
		"somethingElse": map[string]interface{}{
			"foo": "bar",
			"baz": 42,
		},
	}
	docID := handler.PostToCollection(context.Background(), "TestCollection", data, logger)

	if docID != "test-doc-id" {
		t.Errorf("Expected docID 'test-doc-id', got '%s'", docID)
	}
}

func TestPostToCollection_ServerError(t *testing.T) {
	server := testutils.CreateErrorServer(http.StatusInternalServerError, "Internal Server Error")
	defer server.Close()

	handler := &BlockHandler{
		defraURL: server.URL,
		client:   &http.Client{},
	}

	logger := zap.NewNop().Sugar()

	data := map[string]interface{}{
		"field1": "value1",
	}
	docID := handler.PostToCollection(context.Background(), "TestCollection", data, logger)

	if docID != "" {
		t.Errorf("Expected empty docID on error, got '%s'", docID)
	}
}

func TestPostToCollection_NilResponse(t *testing.T) {
	server, handler := createBlockHandlerWithMocks(`{"data": {}}`)
	server.Close() // Simulate network error, SendToGraphql returns nil

	logger, buffer := newTestLogger()
	data := map[string]interface{}{
		"field1": "value1",
	}
	result := handler.PostToCollection(context.Background(), "TestCollection", data, logger)
	if result != "" {
		t.Errorf("Expected empty string for nil response, got '%s'", result)
	}
	if !strings.Contains(buffer.String(), "Received nil response from GraphQL") {
		t.Errorf("Expected log to mention nil response from GraphQL, got: %s", buffer.String())
	}
}

func TestSendToGraphql_Success(t *testing.T) {
	expectedQuery := "query { test }"
	var receivedQuery string

	config := testutils.MockServerConfig{
		ResponseBody: `{"data": {"test": "result"}}`,
		StatusCode:   http.StatusOK,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		ValidateRequest: func(r *http.Request) error {
			body := make([]byte, r.ContentLength)
			r.Body.Read(body)
			receivedQuery = string(body)
			return nil
		},
	}
	server, handler := createBlockHandlerWithMocksConfig(config)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	request := types.Request{
		Query: expectedQuery,
	}

	result := handler.SendToGraphql(context.Background(), request, logger)

	if result == nil {
		t.Fatal("Result should not be nil")
	}

	if !strings.Contains(receivedQuery, expectedQuery) {
		t.Errorf("Request body should contain query '%s', got '%s'", expectedQuery, receivedQuery)
	}
}

func TestSendToGraphql_NetworkError(t *testing.T) {
	// Create a server and close it before making the request
	server, handler := createBlockHandlerWithMocks(`{"data": {}}`)
	server.Close()

	logger := zap.NewNop().Sugar()
	request := types.Request{Query: "query { test }", Type: "POST"}
	result := handler.SendToGraphql(context.Background(), request, logger)
	if result != nil && string(result) != "" {
		t.Errorf("Expected nil or empty result for network error, got '%s'", string(result))
	}
}

func TestGetHighestBlockNumber_MockServer(t *testing.T) {
	response := testutils.CreateGraphQLQueryResponse("Block", `[
		{
			"number": 12345
		}
	]`)
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	blockNumber := handler.GetHighestBlockNumber(context.Background(), logger)

	if blockNumber != 12345 {
		t.Errorf("Expected block number 12345, got %d", blockNumber)
	}
}

func TestGetHighestBlockNumber_EmptyResponse(t *testing.T) {
	response := testutils.CreateGraphQLQueryResponse("Block", "[]")
	server, handler := createBlockHandlerWithMocks(response)
	defer server.Close()

	logger := zap.NewNop().Sugar()

	blockNumber := handler.GetHighestBlockNumber(context.Background(), logger)

	if blockNumber != 0 {
		t.Errorf("Expected block number 0 for empty response, got %d", blockNumber)
	}
}

func TestGetHighestBlockNumber_NilResponse(t *testing.T) {
	server, handler := createBlockHandlerWithMocks(`{"data": {}}`)
	server.Close() // Simulate network error, SendToGraphql returns nil

	logger, buffer := newTestLogger()
	result := handler.GetHighestBlockNumber(context.Background(), logger)
	if result != 0 {
		t.Errorf("Expected 0 for nil response, got %d", result)
	}
	if !strings.Contains(buffer.String(), "failed to query block numbers error") {
		t.Errorf("Expected log to mention failed to query block numbers error, got: %s", buffer.String())
	}
}
