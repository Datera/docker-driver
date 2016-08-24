#!/bin/sh
# Script used to fetched the Unit serial number of the given device /dev/sd* or /dev/dm-*
# This script must be present at location.
/usr/bin/sg_inq /dev/$1 | /bin/grep ' Unit serial number:' | /usr/bin/awk '{print $4}'
