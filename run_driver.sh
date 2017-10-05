#!/bin/sh

echo "Starting Containerized iSCSI-D"
/usr/sbin/iscsid &
echo "Starting Datera Volume Driver"
/go/docker-driver/dddbin > datera-ddd.log 2>&1 &
sleep 2
echo "Tailing the logfile"
tail -f datera-ddd.log | /go/docker-driver/scripts/logviewer -mo
