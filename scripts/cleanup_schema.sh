#!/bin/bash

# Stop DefraDB
killall defradb

# Drop existing collections
rm -rf ~/.defradb/data
