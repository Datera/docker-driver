GOPATH=$(shell pwd)

all:
	env GOPATH=${GOPATH} go get driver
	env GOPATH=${GOPATH} go build -o datera-driver driver

clean:
	rm -f -- datera-driver
	rm -f -- datera_docker_driver.log
	rm -rf -- bin
	rm -rf -- pkg
	rm -rf -- src/github.com
	rm -rf -- src/golang.com

test:
	env GOPATH=${GOPATH} go get driver
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/mock
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/assert
	env GOPATH=${GOPATH} go test -v driver

fmt:
	env GOPATH=${GOPATH} go fmt driver
	env GOPATH=${GOPATH} go fmt datera
