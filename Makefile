BIN := tuitai
PKG := ./cmd/tuitai
LDFLAGS := -s -w

.PHONY: build test vet fmt cross clean run

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

run:
	go run $(PKG) gui

cross:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/tuitai-windows-amd64.exe $(PKG)
	GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/tuitai-linux-amd64 $(PKG)
	GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/tuitai-darwin-arm64 $(PKG)

clean:
	rm -rf dist $(BIN) $(BIN).exe
