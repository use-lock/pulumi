VERSION ?= $(shell node -p "require('./pulumi-plugin.json').version")
BIN := bin/pulumi-resource-lock

SDK_DIR := sdk/nodejs

.PHONY: build check schema gen-sdk build-sdk install-local

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/pulumi-resource-lock

check:
	go vet ./...
	go test -race ./...
	test -z "$$(gofmt -l .)"

schema: build
	pulumi package get-schema ./$(BIN) > schema.json

install-local: build
	pulumi plugin install resource lock $(VERSION) --file ./bin

gen-sdk: build
	rm -rf $(SDK_DIR)
	pulumi package gen-sdk ./$(BIN) --language nodejs --out sdk
	node patch-sdk.js
	cd $(SDK_DIR) && npm install --package-lock-only --ignore-scripts --no-audit --no-fund

build-sdk:
	cd $(SDK_DIR) && npm ci --no-audit --no-fund && npm run build
	cp $(SDK_DIR)/package.json README.md LICENSE $(SDK_DIR)/bin/
	cd $(SDK_DIR)/bin && node -e "const fs=require('fs');const p=JSON.parse(fs.readFileSync('package.json'));p.version='$(VERSION)';p.pulumi.version='$(VERSION)';delete p.scripts.prepare;delete p.devDependencies;fs.writeFileSync('package.json',JSON.stringify(p,null,2)+'\n')"
