GOOS ?= linux # GOOS: target OS for build (linux default, overridable: GOOS=darwin make build)
GOARCH ?= amd64 # GOARCH: target architecture for build (amd64 default, overridable: GOARCH=arm64 make build)
BINARY = rate-limiter # BINARY: output binary name prefix
MAIN = ./cmd/rate-limiter # MAIN: path to main package entry point

.PHONY: build build-all docker clean test vet # .PHONY: declare non-file targets

# build: compile binary for current GOOS/GOARCH
build:
	go build -o $(BINARY)-$(GOOS)-$(GOARCH) $(MAIN)

# build-all: compile binary for all target platforms (linux amd64/arm64, darwin amd64/arm64, windows amd64)
build-all:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY)-linux-amd64 $(MAIN)
	GOOS=linux GOARCH=arm64 go build -o $(BINARY)-linux-arm64 $(MAIN)
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY)-darwin-amd64 $(MAIN)
	GOOS=darwin GOARCH=arm64 go build -o $(BINARY)-darwin-arm64 $(MAIN)
	GOOS=windows GOARCH=amd64 go build -o $(BINARY)-windows-amd64.exe $(MAIN)

# docker: build Docker image from Dockerfile in cmd/rate-limiter/
docker:
	docker build -t rate-limiter:latest -f cmd/rate-limiter/Dockerfile .

# test: run all Go tests recursively (./... = all packages)
test:
	go test ./...

# vet: run go vet for static analysis (undefined behavior, suspicious constructs)
vet:
	go vet ./...

# clean: remove all built binaries from previous build-all
clean:
	rm -f $(BINARY)-linux-amd64 $(BINARY)-linux-arm64 $(BINARY)-darwin-amd64 $(BINARY)-darwin-arm64 $(BINARY)-windows-amd64.exe $(BINARY)

# release: build all platform binaries and create GitHub Release (requires VERSION env var)
release: build-all
	gh release create $(VERSION) \
		--title "$(VERSION)" \
		--notes "Release $(VERSION)" \
		$(wildcard rate-limiter-*)
