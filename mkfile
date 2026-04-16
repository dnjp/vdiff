all:V:
	go build

install:V:
	go install

test:V:
	go test ./...

plan9check:V:
	# NOTE: 9fans.net/go@v0.0.7 has a build bug in draw/drawfcall/mux_plan9.go
	# that prevents a clean GOOS=plan9 compile of the full binary. Our own code
	# is Plan 9 compatible. Track: https://github.com/9fans/go/issues/141
	GOOS=plan9 GOARCH=amd64 go build

lint:V:
	gofumpt -l -w .
	staticcheck ./...
	go vet ./...

clean:V:
	go clean ./...
