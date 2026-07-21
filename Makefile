BINARY := clubhouse
GOBIN  := $(shell go env GOPATH)/bin

install:
	go install ./cmd/clubhouse
	@echo ""
	@echo "✅ installed to $(GOBIN)/$(BINARY)"
	@command -v $(BINARY) >/dev/null 2>&1 && echo "   'clubhouse' is on your PATH — you're set." || ( \
		echo "   add it to PATH so 'clubhouse' works anywhere:" ; \
		echo "     fish:      fish_add_path $(GOBIN)" ; \
		echo "     bash/zsh:  echo 'export PATH=\"\$$PATH:$(GOBIN)\"' >> ~/.zshrc" )

build:
	go build -o $(BINARY) ./cmd/clubhouse

test:
	go test ./...

.PHONY: install build test
