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
