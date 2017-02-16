GOPATH=$(shell pwd)

all:
	git clone ssh://git@gits.daterainc.com:/go-sdk || true
	mv go-sdk/src/dsdk src/dsdk
	rm -rf -- go-sdk
	env GOPATH=${GOPATH} go get ddd
	env GOPATH=${GOPATH} go build -o ddd ddd
	env GOPATH=${GOPATH} go vet ddd

fast:
	env GOPATH=${GOPATH} go get ddd
	env GOPATH=${GOPATH} go build -o ddd ddd
	env GOPATH=${GOPATH} go vet ddd

clean:
	rm -f -- datera-driver
	rm -f -- datera_docker_driver.log
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
	env GOPATH=${GOPATH} go test -v ddd

fmt:
	env GOPATH=${GOPATH} go fmt ddd
