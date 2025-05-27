use std::error::Error;
use ethabi::{Contract, Event, EventParam, ParamType, RawLog};
use ethereum_types::H256;
use serde_json::json;

// Import the module under test
#[path = "lib.rs"]
mod lib;
use lib::{Input, Output, decode_topic};

#[cfg(test)]
mod tests {
    use super::*;

    // Helper function to create a simple ABI with one event
    fn create_test_abi() -> Vec<u8> {
        let json_abi = json!([{
            "anonymous": false,
            "inputs": [
                {
                    "indexed": true,
                    "name": "from",
                    "type": "address"
                },
                {
                    "indexed": true,
                    "name": "to",
                    "type": "address"
                },
                {
                    "indexed": false,
                    "name": "value",
                    "type": "uint256"
                }
            ],
            "name": "Transfer",
            "type": "event"
        }]);
        
        serde_json::to_vec(&json_abi).unwrap()
    }

    #[test]
    fn test_decode_topic_with_valid_event_hash() -> Result<(), Box<dyn Error>> {
        // Create a test ABI
        let abi = create_test_abi();
        
        // Transfer event hash
        let topic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef";
        
        // Decode the topic
        let result = decode_topic(topic, &abi)?;
        
        // The result should not be empty as we should find the event
        assert!(!result.is_empty());
        
        Ok(())
    }

    #[test]
    fn test_decode_topic_with_invalid_event_hash() -> Result<(), Box<dyn Error>> {
        // Create a test ABI
        let abi = create_test_abi();
        
        // Invalid event hash
        let topic = "0x0000000000000000000000000000000000000000000000000000000000000000";
        
        // Decode the topic
        let result = decode_topic(topic, &abi)?;
        
        // For invalid topics, it should return the original topic
        assert_eq!(result, topic);
        
        Ok(())
    }

    #[test]
    fn test_decode_topic_with_empty_abi() -> Result<(), Box<dyn Error>> {
        // Empty ABI
        let abi = serde_json::to_vec(&json!([])).unwrap();
        
        // Topic
        let topic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef";
        
        // Decode the topic
        let result = decode_topic(topic, &abi)?;
        
        // Should return the original topic since no events in ABI
        assert_eq!(result, topic);
        
        Ok(())
    }

    #[test]
    fn test_decode_topic_with_malformed_topic() {
        // Create a test ABI
        let abi = create_test_abi();
        
        // Malformed topic (not hex)
        let topic = "not-a-hex-string";
        
        // Decode should return an error
        let result = decode_topic(topic, &abi);
        assert!(result.is_err());
    }
}
