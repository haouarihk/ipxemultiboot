VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath
PLATFORMS ?= linux/amd64 linux/arm64

.PHONY: all web build ipxe test lint dev-backend dev-web release docker clean qemu-test

all: web build

## web: build the React frontend into web/dist (embedded into the binary)
web:
	cd web && pnpm install --frozen-lockfile && pnpm build

## build: build the server binary into bin/ipxemultiboot
build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/ipxemultiboot ./cmd/ipxemultiboot

## ipxe: rebuild the embedded iPXE binaries from source (needs gcc, make, liblzma)
ipxe:
	scripts/build-ipxe.sh

## test: run Go and frontend checks
test:
	go vet ./...
	go test ./...
	cd web && pnpm typecheck

## lint: check formatting
lint:
	@test -z "$$(gofmt -l cmd internal web)" || (gofmt -l cmd internal web; exit 1)

## dev-backend: run the server with local folders (ports 67/69 need root)
dev-backend: build
	sudo IPXEMULTIBOOT_DATA_DIR=./data IPXEMULTIBOOT_ISO_DIR=./isos ./bin/ipxemultiboot -v

## dev-web: run the Vite dev server with hot reload, proxying the API to :8080
dev-web:
	cd web && pnpm dev

## qemu-test: boot VMs in every firmware/DHCP mode combination (no root needed)
qemu-test: build
	scripts/regress.sh

## release: cross-compile release archives into dist/
release: web
	rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; name=ipxemultiboot_$(VERSION:v%=%)_$${os}_$${arch}; \
		echo "building $$name"; \
		mkdir -p dist/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/$$name/ipxemultiboot ./cmd/ipxemultiboot || exit 1; \
		cp LICENSE README.md packaging/systemd/ipxemultiboot.service dist/$$name/ || exit 1; \
		tar -C dist/$$name -czf dist/$$name.tar.gz . ; \
		rm -rf dist/$$name; \
	done
	cd dist && sha256sum *.tar.gz > checksums.txt

## docker: build the container image
docker:
	docker build --build-arg VERSION=$(VERSION) -t ipxemultiboot:$(VERSION) .

clean:
	rm -rf bin dist web/dist/assets web/dist/index.html web/dist/favicon.svg
