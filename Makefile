GO_CMD := go
CMDS := $(shell ls cmd)

Q := @

.PHONY: all
all: test build

.PHONY: build
build: $(foreach cmd,$(CMDS),bin/$(cmd))

FORCE:

bin/%: FORCE
	$(GO_CMD) build -o $@ ./cmd/$(notdir $@)

.PHONY: verify
verify: gofmt-verify ci-lint

.PHONY: gofmt-verify
gofmt-verify:
	@out=`gofmt -w -l -d $$(find . -name '*.go')`; \
	if [ -n "$$out" ]; then \
	    echo "$$out"; \
	    exit 1; \
	fi

.PHONY: ci-lint
ci-lint:
	golangci-lint run

.PHONY: test
test:
	$(Q)$(GO_CMD) test -v -coverprofile=coverage.txt ./pkg/...
