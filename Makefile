GOPATH=$(shell pwd)
BINNAME=dddbin

all:
	git clone http://github.com/Datera/go-sdk || true
	mv go-sdk/src/dsdk src/dsdk
	rm -rf -- go-sdk
	env GOPATH=${GOPATH} go get ddd
	env GOPATH=${GOPATH} go build -o ${BINNAME} ddd
	env GOPATH=${GOPATH} go vet ddd

fast:
	env GOPATH=${GOPATH} go get ddd
	env GOPATH=${GOPATH} go build -o ${BINNAME} ddd
	env GOPATH=${GOPATH} go vet ddd

clean:
	rm -f -- dddbin
	rm -f -- ddd.log
	rm -f -- dsdk.log
	rm -f -- datera-config-template.txt
	rm -rf -- bin
	rm -rf -- pkg
	rm -rf -- src/github.com
	rm -rf -- src/golang.com
	rm -rf -- src/golang.org
	rm -rf -- src/dsdk

test:
	env GOPATH=${GOPATH} go get ddd
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/mock
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/assert
	env GOPATH=${GOPATH} go build -o ${BINNAME} ddd
	env GOPATH=${GOPATH} go vet ddd
	env GOPATH=${GOPATH} go test -v ddd

fmt:
	env GOPATH=${GOPATH} go fmt ddd
