# Use this repository's go.work even when GOWORK points at the go-kanna-wide one.
export GOWORK := $(CURDIR)/go.work

BUILD_DIR = bin

.PHONY: test
test:
	go test ./... -race -coverprofile=coverage.out -covermode=atomic

.PHONY: lint
lint:
	@for dir in $$(go list -m -f '{{.Dir}}'); do \
		echo ">>> lint $$dir"; \
		(cd $$dir && golangci-lint run) || exit 1; \
	done

.PHONY: lint-fix
lint-fix:
	@for dir in $$(go list -m -f '{{.Dir}}'); do \
		echo ">>> lint-fix $$dir"; \
		(cd $$dir && golangci-lint run --fix) || exit 1; \
	done

.PHONY: build
build:
	go build -o $(BUILD_DIR)/kanna-validate ./cmd/kanna-validate

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

.PHONY: example
example:
	cd example && go generate ./... && go vet ./... && go run .

# BENCHFLAGS tunes the run, such as BENCHFLAGS=-count=6 for benchstat.
.PHONY: bench
bench:
	cd bench && go generate ./... && go test -bench . $(BENCHFLAGS) ./...
