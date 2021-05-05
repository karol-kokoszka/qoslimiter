GOARCH ?= $(shell go env GOHOSTARCH 2>/dev/null)
GOOS ?= $(shell go env GOOS 2>/dev/null)
TEST_TIMEOUT := 10m

deps: ### Download depdendencies
	@echo Download dependencies
	@GOSUMDB=off go mod download

test: ### Run tests
	@echo "Running tests"
	go clean -testcache
	CGO_ENABLED=0 go test -cover -timeout ${TEST_TIMEOUT} ./pkg/...
