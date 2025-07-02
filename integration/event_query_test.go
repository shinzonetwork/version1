//go:build integration
// +build integration

package integration

import (
	"path/filepath"
	"testing"
)

var eventsQueryPath string

func init() {
	eventsQueryPath = filepath.Join(getProjectRoot(nil), "queries/events.graphql")
}

func getArbitraryBlock(t *testing.T) map[string]interface{} {
	blockQueryPath := filepath.Join(getProjectRoot(nil), "queries/blocks.graphql")
	blockNumber := getLatestBlockNumber(t) - 25 // Alchemy doesn't always have events for the latest blocks
	if blockNumber < 0 {
		t.Fatalf("Block number underflow: %d", blockNumber)
	}
	variables := map[string]interface{}{"blockNumber": blockNumber}
	result := MakeQuery(t, blockQueryPath, "GetBlockWithTransactions", variables)
	blockList, ok := result["data"].(map[string]interface{})["Block"].([]interface{})
	if !ok || len(blockList) == 0 {
		t.Fatalf("No block with number %v found; cannot test events.", blockNumber)
	}
	block := blockList[0].(map[string]interface{})
	return block
}

func TestGetEventsForBlock(t *testing.T) {
	block := getArbitraryBlock(t)
	blockHash, ok := block["hash"].(string)
	if !ok || len(blockHash) == 0 {
		t.Fatalf("Block hash missing or empty in block: %v", block)
	}
	eventsQueryPath := filepath.Join(getProjectRoot(nil), "queries/events.graphql")
	result := MakeQuery(t, eventsQueryPath, "GetEventsForBlock", map[string]interface{}{"blockHash": blockHash})
	eventList, ok := result["data"].(map[string]interface{})["Event"].([]interface{})
	if !ok {
		t.Errorf("No events returned for blockHash %v: %v", blockHash, result)
		return
	}
	for _, e := range eventList {
		event, ok := e.(map[string]interface{})
		if !ok {
			t.Errorf("Event is not a map: %v", e)
			continue
		}
		if event["contractAddress"] == nil || event["eventName"] == nil {
			t.Errorf("Event missing contractAddress or eventName: %v", event)
		}
	}
}

func TestGetEventsByEventName(t *testing.T) {
	block := getArbitraryBlock(t)
	blockHash, ok := block["hash"].(string)
	if !ok || len(blockHash) == 0 {
		t.Fatalf("Block hash missing or empty in block: %v", block)
	}
	result := MakeQuery(t, eventsQueryPath, "GetEventsForBlock", map[string]interface{}{"blockHash": blockHash})
	eventList, ok := result["data"].(map[string]interface{})["Event"].([]interface{})
	if !ok || len(eventList) == 0 {
		t.Skipf("No events found for blockHash %v, skipping event name test", blockHash)
		return
	}
	firstEvent, ok := eventList[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Event is not a map: %v", eventList[0])
	}
	eventName, ok := firstEvent["eventName"].(string)
	if !ok || len(eventName) == 0 {
		t.Fatalf("Event name missing or empty: %v", firstEvent)
	}
	result = MakeQuery(t, eventsQueryPath, "GetEventsByEventName", map[string]interface{}{"name": eventName})
	nameEventList, ok := result["data"].(map[string]interface{})["Event"].([]interface{})
	if !ok || len(nameEventList) == 0 {
		t.Errorf("No events returned for eventName %v: %v", eventName, result)
		return
	}
	found := false
	for _, e := range nameEventList {
		event, ok := e.(map[string]interface{})
		if !ok {
			t.Errorf("Event is not a map: %v", e)
			continue
		}
		if event["eventName"] == eventName {
			found = true
		} else {
			t.Errorf("Event does not match eventName %v: %v", eventName, event)
		}
	}
	if !found {
		t.Errorf("No event matched the eventName %v", eventName)
	}
} 