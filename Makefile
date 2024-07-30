.DEFAULT_GOAL := build
.PHONY:gen fmt vet build run
gen:
		go generate .
fmt: gen
		go fmt .
vet: fmt
		go vet .
build: vet
		go build .
run: build
		./goserv
