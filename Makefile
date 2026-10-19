BINARY  := terraform-provider-varnish
VERSION ?= dev
GOBIN   := $(shell go env GOPATH)/bin

default: build

.PHONY: build
build:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .

# install builds the provider into GOBIN and prints the dev_overrides block to
# add to ~/.terraformrc so `terraform plan/apply` uses this local build.
.PHONY: install
install:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o $(GOBIN)/$(BINARY) .
	@echo ""
	@echo "Add this to ~/.terraformrc (then skip 'terraform init'):"
	@echo ""
	@echo "provider_installation {"
	@echo "  dev_overrides { \"varnish/varnish\" = \"$(GOBIN)\" }"
	@echo "  direct {}"
	@echo "}"

.PHONY: test
test:
	CGO_ENABLED=1 go test -race ./...

# testacc runs acceptance tests against a live API. Requires:
#   VARNISH_ENDPOINT, VARNISH_ACCESS_TOKEN (and optionally VARNISH_INSECURE=true)
.PHONY: testacc
testacc:
	TF_ACC=1 CGO_ENABLED=1 go test -race -v -timeout 120m ./internal/provider/...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: vet
vet:
	go vet ./...

# pinned so docs/ is byte-identical whoever regenerates it
TFPLUGINDOCS_VERSION := v0.25.0

.PHONY: docs
docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) generate --provider-name varnish

.PHONY: lint
lint: vet
	@unformatted=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd (run 'make fmt'):"; echo "$$unformatted"; exit 1; \
	fi
