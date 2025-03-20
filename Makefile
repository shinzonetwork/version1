.PHONY: deps playground defra-playground build github

deps:
	go mod download

playground:
	GOFLAGS="-tags=playground" go mod download

defra-playground:
	cd $(GOPATH)/src/github.com/sourcenetwork/defradb && \
	GOFLAGS="-tags=playground" go install ./cmd/defradb

build:
	go build -o bin/indexer cmd/indexer/main.go

start: build
	./bin/indexer > indexer.log 2>&1 &

github:
	git add .
	git commit -m "Update dependencies"
	git push origin ${BRANCH}