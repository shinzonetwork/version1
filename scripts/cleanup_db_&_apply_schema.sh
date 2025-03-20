#!/bin/bash

# Stop DefraDB
pkill defradb

# Drop existing collections
rm -rf ~/.defradb/data

# Start DefraDB
~/go/bin/defradb start &

# Apply new schema
./scripts/apply_schema.sh
