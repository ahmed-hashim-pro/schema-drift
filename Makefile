# Thin wrappers over go, plus the targets that regenerate what the README shows.
GO ?= go

.PHONY: build test check docs demo clean

build:
	CGO_ENABLED=0 $(GO) build -o schema-drift ./cmd/schema-drift

test:
	$(GO) vet ./...
	$(GO) test ./...

check: build
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	$(GO) vet ./...
	$(GO) test -race ./...

# docs/*.txt are the CLI's real output; the README quotes them verbatim.
docs: build
	scripts/docs.sh

# Re-record docs/demo.gif. Needs Docker: VHS, plus git for the gate demo.
# The binaries are static so they run in that image.
demo: build
	CGO_ENABLED=0 $(GO) build -o orders-consumer ./examples/consumer
	printf 'FROM ghcr.io/charmbracelet/vhs:v0.10.0\nRUN apt-get update -q --allow-releaseinfo-change && apt-get install -qy --no-install-recommends git && rm -rf /var/lib/apt/lists/*\n' | \
	  docker build -q -t schema-drift-vhs - > /dev/null
	docker run --rm -u $$(id -u):$$(id -g) -e HOME=/tmp -v $(CURDIR):/vhs schema-drift-vhs docs/demo.tape

clean:
	rm -f schema-drift orders-consumer
