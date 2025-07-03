.PHONY: deps env build start clean defradb gitpush test testrpc coverage bootstrap playground stop

DEFRA_PATH ?=

deps:
	go mod download

env:
	export $(cat .env)

build:
	go build -o bin/block_poster cmd/block_poster/main.go

start:
	./bin/block_poster > logs/log.txt 1>&2   

defradb:
	sh scripts/apply_schema.sh

clean:
	rm -rf bin/ && rm -r logs/logfile && touch logs/logfile

gitpush: 
	git add . && git commit -m "${COMMIT_MESSAGE}" && git push origin ${BRANCH_NAME}

geth-start:
	cd $GETH_DIR && geth --http --authrpc.jwtsecret=$HOME/.ethereum/jwt.hex --datadir=$HOME/.ethereum

prysm-start:
	cd $PRYSM_DIR && ./prysm.sh beacon-chain \
  --execution-endpoint=http://localhost:8551 \
  --jwt-secret=$HOME/.ethereum/jwt.hex \
  --checkpoint-sync-url=https://mainnet.checkpoint-sync.ethpandaops.io \
  --suggested-fee-recipient=0x8E4902d854e6A7eaF44A98D6f1E600413C99Ce07

test:
	go test ./... -v

testrpc:
	go test ./pkg/rpc -v

coverage:
	go test -coverprofile=coverage.out ./... || true
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html
	rm coverage.out

bootstrap:
	@if [ -z "$(DEFRA_PATH)" ]; then \
		echo "ERROR: You must pass DEFRA_PATH. Usage:"; \
		echo "  make bootstrap DEFRA_PATH=../path/to/defradb"; \
		exit 1; \
	fi
	@scripts/bootstrap.sh "$(DEFRA_PATH)" "$(PLAYGROUND)"

playground:
	@if [ -z "$(DEFRA_PATH)" ]; then \
		echo "ERROR: You must pass DEFRA_PATH. Usage:"; \
		echo "  make playground DEFRA_PATH=../path/to/defradb"; \
		exit 1; \
	fi
	@$(MAKE) bootstrap PLAYGROUND=1 DEFRA_PATH="$(DEFRA_PATH)"

stop:
	@echo "===> Stopping defradb if running..."
	@DEFRA_ROOTDIR="$(shell pwd)/.defra"; \
	DEFRA_PIDS=$$(ps aux | grep '[d]efradb start --rootdir ' | grep "$$DEFRA_ROOTDIR" | awk '{print $$2}'); \
	if [ -n "$$DEFRA_PIDS" ]; then \
	  echo "Killing defradb PIDs: $$DEFRA_PIDS"; \
	  echo "$$DEFRA_PIDS" | xargs -r kill -9 2>/dev/null; \
	  echo "Stopped all defradb processes using $$DEFRA_ROOTDIR"; \
	else \
	  echo "No defradb processes found for $$DEFRA_ROOTDIR"; \
	fi; \
	rm -f .defra/defradb.pid;
	@echo "===> Stopping block_poster if running..."
	@BLOCK_PIDS=$$(ps aux | grep '[b]lock_poster' | awk '{print $$2}'); \
	if [ -n "$$BLOCK_PIDS" ]; then \
	  echo "Killing block_poster PIDs: $$BLOCK_PIDS"; \
	  echo "$$BLOCK_PIDS" | xargs -r kill -9 2>/dev/null; \
	  echo "Stopped all block_poster processes"; \
	else \
	  echo "No block_poster processes found"; \
	fi; \
	rm -f .defra/block_poster.pid;

