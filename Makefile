GOPATH=$(shell pwd)

all:
	go get driver
	env GOPATH=${GOPATH} go build -o datera-driver driver

clean:
	rm -f driver
	rm -rf -- bin
	rm -rf -- pkg
	rm -rf -- src/github.com
	rm -rf -- src/golang.com

test:
	go get driver
	go get github.com/stretchr/testify/mock
	go get github.com/stretchr/testify/assert
	go test -v driver
