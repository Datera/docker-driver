GOPATH=$(shell pwd)
DIRNAME=ddd
BINNAME=dddbin
IMGNAME=docker-driver
REPONAME=dateraiodev

all:
	env GOPATH=${GOPATH} go get ${DIRNAME}
	env GOPATH=${GOPATH} go build -ldflags '-extldflags "-static"' -o ${BINNAME} ${DIRNAME}
	env GOPATH=${GOPATH} go vet ${DIRNAME}

linux:
	rm -f -- dddbin
	docker build -t ${IMGNAME} .
	docker cp $(shell docker run -d -it --entrypoint "true" ${IMGNAME}):/go/docker-driver/dddbin .

fast:
	env GOPATH=${GOPATH} go get ${DIRNAME}
	env GOPATH=${GOPATH} go build -o ${BINNAME} ${DIRNAME}
	env GOPATH=${GOPATH} go vet ${DIRNAME}

plugin: # must be run with "sudo"
	docker build -t ${IMGNAME} .
	mkdir -p rootfs
	docker export `docker create ${IMGNAME} true` | sudo tar -x -C rootfs
	docker rm -vf `docker ps -a | grep ${IMGNAME} | head -n 1 | awk '{print $$1}'`
	docker rmi ${IMGNAME}
	docker plugin create ${REPONAME}/${IMGNAME} .

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
	env GOPATH=${GOPATH} go get ${DIRNAME}
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/mock
	env GOPATH=${GOPATH} go get github.com/stretchr/testify/assert
	env GOPATH=${GOPATH} go build -o ${BINNAME} ${DIRNAME}
	env GOPATH=${GOPATH} go vet ${DIRNAME}
	env GOPATH=${GOPATH} go test -v ${DIRNAME}

fmt:
	env GOPATH=${GOPATH} go fmt ${DIRNAME}
