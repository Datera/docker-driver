IMGNAME=docker-driver
REPONAME=dateraiodev

TAR ?= $(shell if hash gtar 2>/dev/null; then echo "gtar"; else echo "tar"; fi)
VERSION ?= $(shell grep -E "DriverVersion\s+=" ../../pkg/driver/driver.go | awk '{print $$3}' | sed 's/"//g')
GITHASH ?= $(shell git describe --match nEvErMatch --always --abbrev=10 --dirty)
GOSDK_V ?= $(shell go mod graph | grep "github.com/Datera/docker-driver github.com/Datera/go-sdk" | awk -F '@' '{print $$2}')
NAME=ddd

compile: iscsi-rpc
	@echo "==> Building the Datera Docker Driver Version ${VERSION}"
	@echo "==> docker-driver version ${VERSION}-${GITHASH}"
	@echo "==> go-sdk version ${GOSDK_V}"
	@env go get -d ./...
	@env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags 'osusergo netgo static_build' -o ${NAME} -ldflags "-X 'github.com/Datera/datera-csi/pkg/driver.Version=${VERSION}' -X 'github.com/Datera/datera-csi/pkg/driver.SdkVersion=${GOSDK_V}' -X 'github.com/Datera/datera-csi/pkg/driver.Githash=${GITHASH}'" github.com/Datera/datera-csi/cmd/dat-csi-plugin
	@env go vet ./...

iscsi-rpc:
	@echo "==> Building iscsi-rpc"
	@env go get -d ./...
	@env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags 'osusergo netgo static_build' -o iscsi-send github.com/Datera/datera-csi/cmd/iscsi-send
	@env go vet ./...
	@env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags 'osusergo netgo static_build' -o iscsi-recv github.com/Datera/datera-csi/cmd/iscsi-recv
	@env go vet ./...

plugin: compile
	docker build -t ${IMGNAME} .
	mkdir -p rootfs
	docker export `docker create ${IMGNAME} true` | ${TAR} -x -C rootfs 2>/dev/null
	docker rm -vf `docker ps -a | grep ${IMGNAME} | head -n 1 | awk '{print $$1}'`
	docker rmi ${IMGNAME} -f
	docker plugin create ${REPONAME}/${IMGNAME} .

clean:
	rm -f -- ddd
	rm -f -- iscsi-recv
	rm -f -- iscsi-send
	rm -rf -- rootfs

test:
	go get ${DIRNAME}
	go get github.com/stretchr/testify/mock
	go get github.com/stretchr/testify/assert
	go build -o ${BINNAME} ${DIRNAME}
	go vet ${DIRNAME}
	go test -v ${DIRNAME}

fmt:
	go fmt ${DIRNAME}
