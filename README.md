# Shinzo Network Blockchain Indexer

A high-performance blockchain indexing solution built with Source Network, DefraDB, and LensVM.

## Architecture

- **GoLang**: Indexing engine for storing block data at source
- **DefraDB**: P2P datastore for blockchain data storage and querying

## Features

- Real-time blockchain data indexing
- GraphQL API for querying indexed data
- Support for blocks, transactions, logs, and events
- Bi-directional relationships between blockchain entities
- Deterministic document IDs
- Graceful shutdown handling
- Concurrent indexing with duplicate block protection
- Structured logging with clear error reporting

## Prerequisites

- Go 1.20+
- [DefraDB](https://github.com/sourcenetwork/defradb)
- [Source Network CLI](https://docs.sourcenetwork.io/cli)
- [Alchemy API Key](https://www.alchemy.com/docs)

## Prerequisit setup

- Install [DefraDB](https://github.com/sourcenetwork/defradb)
- Navigate to defradb
- Add a .nvmrc file with the node `v23.10.0`


## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/shinzonetwork/version1.git
   cd version1
   ```

2. Install Go dependencies:
   ```bash
   go mod download
   ```

3. Create environment variables in `.env`:
   ```bash
    DEFRA_KEYRING_SECRET=<DefraDB_SECRET> # DEFRA KEY RING PASSWORD
    ALCHEMY_API_KEY=<Alchemy_API_KEY> # Alchemy API key
    ALCHEMY_NETWORK=<Alchemy_NETWORK> # RPC network 
    VERSION=<VERSION> # verisoning 
    DEFRA_LOGGING_DEVELOPMENT=<DEFRA_LOGGING_DEVELOPMENT> # logging switch
    DEFRA_DEVELOPMENT=<DEFRA_DEVELOPMENT>
    DEFRA_P2P_ENABLED=<DEFRA_P2P_ENABLED> # p2p enable true required for prod
    RPC_URL=<RPC_URL> # RPC HTTP URL
    DEFRADB_URL=<DEFRADB_URL> # DefraDB HTTP URL
   ```

## Configuration

1. Configure DefraDB schema:
   - GraphQL schema files are located in `schema/`
   - Main schema defines relationships between blocks, transactions, logs, and events
   - Each entity has its own schema file in `schema/types/blockchain/`

2. Update `config/config.yaml` with your settings:
   ```yaml
   alchemy:
     api_key: ${ALCHEMY_API_KEY}
   defra:
     url: ${DEFRA_URL}
   ```

## How to Run

`make bootstrap DEFRA_PATH=/path/to/defradb`
or, to open the playground as well, use
`make playground DEFRA_PATH=/path/to/defradb`

To avoid passing the `DEFRA_PATH=/path/to/defradb` portion of the command, set `DEFRA_PATH` as an environment variable.

## Data Model

### Entities and Relationships
- **Block**
  - Primary key: `hash` (unique index)
  - Secondary index: `number`
  - Has many transactions (`block_transactions`)
- **Transaction**
  - Primary key: `hash` (unique index)
  - Secondary indices: `blockHash`, `blockNumber`
  - Belongs to block (`block_transactions`)
  - Has many logs (`transaction_logs`)
- **Log**
  - Indices: `blockNumber`
  - Belongs to block and transaction
  - Has many events (`log_events`)
- **Event**
  - Indices: `transactionHash`, `blockNumber`
  - Belongs to log (`log_events`)

### Relationship Definitions

DefraDB relationships use the `@relation(name: "relationship_name")` syntax. Example:

```graphql
type Block {
  transactions: [Transaction] @relation(name: "block_transactions")
}

type Transaction {
  block: Block @relation(name: "block_transactions")
}
```

### Logging Strategy

The indexer implements a structured logging strategy:
- INFO level: Important state changes and block processing
- ERROR level: Critical issues requiring attention
- Block number context in error messages
- Clear distinction between normal shutdown and errors
- Success logging for block processing progress

### Querying Data

Access indexed data through DefraDB's GraphQL API at `http://localhost:9181/api/v0/graphql`

Example query:
```graphql
{
  Block(filter: { number: { _eq: 18100003 } }) {
    hash
    transactions {
      hash
      logs {
        logIndex
        data
        events {
          eventName
          parameters
        }
      }
    }
  }
}
```

## Documentation Links

- [DefraDB Documentation](https://github.com/sourcenetwork/defradb)
- [Source Network Documentation](https://docs.sourcenetwork.io)
- [Alchemy API Documentation](https://docs.alchemy.com/reference/api-overview)

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
