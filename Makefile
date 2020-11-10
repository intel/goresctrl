GO_CMD := go

Q := @

.PHONY: all ci-lint gofmt-verify test verify

all: test

verify: gofmt-verify ci-lint

gofmt-verify:
	@out=`gofmt -l -d $$(find . -name '*.go')`; \
	if [ -n "$$out" ]; then \
	    echo "$$out"; \
	    exit 1; \
	fi

ci-lint:
	golangci-lint run

test:
	$(Q)$(GO_CMD) test -v -coverprofile=coverage.txt ./pkg/...
