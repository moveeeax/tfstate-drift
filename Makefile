BINARY := tfstate-drift
PKG := ./...

.PHONY: all build test race vet fmt fmt-check smoke clean

all: fmt-check vet race build

build:
	go build -o $(BINARY) .

test:
	go test $(PKG)

race:
	go test -race $(PKG)

vet:
	go vet $(PKG)

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "needs gofmt:"; echo "$$unformatted"; exit 1; fi

smoke: build
	@set +e; ./$(BINARY) scan --plan-json examples/drift-plan.json --format tree; \
	echo "exit: $$?"

clean:
	rm -f $(BINARY) out.json coverage.out
