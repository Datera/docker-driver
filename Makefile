GOPATH=$(shell pwd)
BINNAME=dddbin
IMGNAME=datera

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

plugin: # must be run with "sudo"
	docker build -t ${IMGNAME} .
	mkdir -p rootfs
	docker export `docker create datera true` | sudo tar -x -C rootfs
	docker rm -vf `docker ps -a | grep datera | head -n 1 | awk '{print $$1}'`
	docker rmi ${IMGNAME}
	docker plugin create dateraio/${IMGNAME} .

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
	rm -rf -- rootfs

test:
	env GOPATH=${GOPATH} go get ddd
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/mock
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/assert
	env GOPATH=${GOPATH} go build -o ${BINNAME} ddd
	env GOPATH=${GOPATH} go vet ddd
	env GOPATH=${GOPATH} go test -v ddd

fmt:
	env GOPATH=${GOPATH} go fmt ddd
