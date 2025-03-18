.PHONY: deps playground build defra:playground

deps:
	go mod download

playground:
	GOFLAGS="-tags=playground" go mod download

defra:playground:
	cd $(GOPATH)/src/github.com/sourcenetwork/defradb && \
	GOFLAGS="-tags=playground" go install ./cmd/defradb

build:
	go build -o bin/indexer cmd/indexer/main.go
