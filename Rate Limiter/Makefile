GOOS ?= linux
GOARCH ?= amd64
BINARY = rate-limiter
MAIN = ./cmd/rate-limiter

.PHONY: build build-all docker clean test vet

build:
	go build -o $(BINARY)-$(GOOS)-$(GOARCH) $(MAIN)

build-all:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY)-linux-amd64 $(MAIN)
	GOOS=linux GOARCH=arm64 go build -o $(BINARY)-linux-arm64 $(MAIN)
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY)-darwin-amd64 $(MAIN)
	GOOS=darwin GOARCH=arm64 go build -o $(BINARY)-darwin-arm64 $(MAIN)
	GOOS=windows GOARCH=amd64 go build -o $(BINARY)-windows-amd64.exe $(MAIN)

docker:
	docker build -t rate-limiter:latest -f cmd/rate-limiter/Dockerfile .

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)-linux-amd64 $(BINARY)-linux-arm64 $(BINARY)-darwin-amd64 $(BINARY)-darwin-arm64 $(BINARY)-windows-amd64.exe $(BINARY)
