VERSION := 0.1.0
TAG := v$(VERSION)

EXTENSION_NAME := gh-actionarmor

BIN_DIR := $(CURDIR)/bin

# build binary must be located in the root of the project to test it with the local gh
BUILD_BIN := $(EXTENSION_NAME)


STATICCHECK := $(BIN_DIR)/staticcheck
$(STATICCHECK):
	mkdir -p $(BIN_DIR)
	GOBIN=$(BIN_DIR) go install honnef.co/go/tools/cmd/staticcheck@latest

TESTIFYILINT := $(BIN_DIR)/testifylint
$(TESTIFYILINT):
	mkdir -p $(BIN_DIR)
	GOBIN=$(BIN_DIR) go install github.com/Antonboom/testifylint@latest


.PHONY: tag
tag:
	git tag $(TAG) -m "Release $(TAG)"

.PHONY: push-tag
push-tag:
	git push origin $(TAG)

.PHONY: build
build:
	mkdir -p $(BIN_DIR)
	go build -o $(BUILD_BIN) main.go

.PHONY: fmt
fmt: $(TESTIFYILINT)
	gofmt -w -s .
	$(TESTIFYILINT) -fix ./...

.PHONY: check
check: $(STATICCHECK) $(TESTIFYILINT)
	$(STATICCHECK) ./...
	go vet ./...
	$(TESTIFYILINT) ./...

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(BUILD_BIN)

.PHONY: test
test:
	go test -race -v ./...

.PHONY: uninstall
uninstall:
	-gh extension remove $(EXTENSION_NAME)

.PHONY: install
install: build uninstall
	gh extension install .
	gh extension list

.PHONY: help
help: install
	gh actionarmor --help

.PHONY: test-run
test-run: install
	gh actionarmor --log-level=debug .

.PHONY: test-run-no-cache
test-run-no-cache: install
	gh actionarmor --log-level=debug --no-cache . 

debug:
	go test -run Execute
