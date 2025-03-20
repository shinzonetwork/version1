#!/bin/bash
set -e

# Read the schema file
SCHEMA_FILE="/Users/duncanbrown/Developer/shinzo/version1/schema/schema.sdl"
if [ ! -f "$SCHEMA_FILE" ]; then
    echo "Schema file not found: $SCHEMA_FILE"
    exit 1
fi

# Read and escape the schema content
SCHEMA=$(cat "$SCHEMA_FILE" | sed 's/"/\\"/g' | tr '\n' ' ')

# Create the GraphQL mutation
MUTATION="mutation { createCollection(schema: \"$SCHEMA\") }"

# Send the mutation to DefraDB
echo "Initializing schema in DefraDB..."
RESPONSE=$(curl -s -X POST http://127.0.0.1:9181/api/v0/graphql \
  -H "Content-Type: application/json" \
  -d "{\"query\": \"$MUTATION\"}")

# Check for errors in the response
if echo "$RESPONSE" | grep -q "error"; then
    echo "Error initializing schema:"
    echo "$RESPONSE" | jq '.'
    exit 1
else
    echo "Schema initialized successfully!"
    echo "$RESPONSE" | jq '.'
fi
