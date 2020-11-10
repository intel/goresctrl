GO_CMD := go

Q := @

.PHONY: all test

all: test

test:
	$(Q)$(GO_CMD) test -v ./pkg/...
