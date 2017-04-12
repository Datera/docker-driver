#!/usr/bin/env python

from __future__ import print_function, unicode_literals, absolute_import

import argparse
import subprocess
import shlex
import sys

IMAGE = "dateraio/docker-driver"
BINARY = "dddbin"


def run_cmd(cmd, shell=False):
    print("$ {}".format(cmd))
    rcmd = shlex.split(cmd) if not shell else cmd
    result = subprocess.check_output(rcmd, shell=shell)
    print(result)
    return result


def main(args):
    run_cmd("sudo docker pull {}".format(IMAGE))
    run_cmd("sudo docker run {}".format(IMAGE))
    container_id = run_cmd(
        "bash -c \"sudo docker ps -a | grep {} "
        "| awk '{{print $1}}' | head -n 1\"".format(
            IMAGE)).strip()
    run_cmd("sudo docker cp '{0}:/docker-driver/{1}' {1}".format(
        container_id, BINARY))
    run_cmd(
        "sudo ./dddbin -datera-cluster {dc_ip} -username {dc_username} "
        "-password {dc_password} -tenant {dc_tenant} > /dev/null "
        "2>&1 &".format(
            dc_ip=args.dc_ip,
            dc_username=args.dc_username,
            dc_password=args.dc_password,
            dc_tenant=args.dc_tenant),
        shell=True)

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("dc_ip")
    parser.add_argument("--dc-username", default="admin")
    parser.add_argument("--dc-password", default="password")
    parser.add_argument("--dc-tenant", default="root")
    args = parser.parse_args()

    sys.exit(main(args))
