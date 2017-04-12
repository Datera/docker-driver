# Datera Docker Driver Image: Dockerfile for the Datera Docker Volume Driver
# Based on ubuntu:latest, runs the Datera Docker Volume Driver in a container

FROM golang:alpine

MAINTAINER Matt Smith <mss@datera.io>

# Setup Go, OpenISCSI and Make
RUN apk add --update \
    alpine-sdk \
    open-iscsi \
    e2fsprogs \
    mkinitfs
RUN mkdir docker-driver/
COPY . docker-driver/

RUN cd docker-driver && make clean && make

RUN apk del alpine-sdk -y

# Setup Volume Internal Mount Location
RUN mkdir -p /var/lib/docker-volumes/_datera && \
    mkdir -p /run/docker/plugins && \
    touch /run/docker/plugins/datera.sock \
    /usr/sbin/iscsid

ENTRYPOINT [ "/bin/sh", "-c", "/usr/sbin/iscsid; /go/docker-driver/dddbin" ]
