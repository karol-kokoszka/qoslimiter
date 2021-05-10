GOARCH ?= $(shell go env GOHOSTARCH 2>/dev/null)
GOOS ?= $(shell go env GOOS 2>/dev/null)
TEST_TIMEOUT := 10m
GOPATH := $(shell go env GOPATH)
DOCKER_IMG	:= $$(grep FROM Dockerfile | head -n 1 | awk '{print $$2 }')

deps: ### Download depdendencies
	@echo Download dependencies
	@GOSUMDB=off go mod download

test: ### Run tests
	@echo "Running tests"
	go clean -testcache
	CGO_ENABLED=0 go test -cover -timeout ${TEST_TIMEOUT} ./pkg/...

docker-build:
	docker build -t qoslistener .

docker-test: ### Run test inside docker
	docker run --rm -it -e CGO_ENABLED=0 -v $(CURDIR):/go/src/github.com/karol-kokoszka/qoslimiter -w /go/src/github.com/karol-kokoszka/qoslimiter qoslistener make test