# Datera Docker Driver Image: Dockerfile for the Datera Docker Volume Driver
# Based on ubuntu:latest, runs the Datera Docker Volume Driver in a container

FROM alpine

MAINTAINER Matt Smith <mss@datera.io>

RUN apk add --update \
    e2fsprogs \
    mkinitfs

ADD ddd /bin/
ADD iscsi-send /bin/
ADD iscsiadm /bin/

# Setup Volume Internal Mount Location
RUN mkdir -p /var/lib/docker-volumes/_datera && \
    mkdir -p /run/docker/plugins && \
    touch /run/docker/plugins/datera.sock

CMD [ "/bin/sh", "-c", "/bin/ddd 2>&1 | tee /var/log/driver.log" ]
