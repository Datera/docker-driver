#!/usr/bin/env python

from __future__ import print_function, unicode_literals, absolute_import

import argparse
import io
import json
import subprocess
import shlex
import sys

GITHUB_ZIP = ("https://github.com/Datera/docker-driver/releases/download/"
              "{}/dddbin.zip")
BINARY = "dddbin"
ZIP = "dddbin.zip"
DEFAULT_CONFIG = "/root/.datera-config-file"

SUCCESS = 0
ARG_ERROR = 1
CONF_ERROR = 2


def run_cmd(cmd, shell=False):
    print("$ {}".format(cmd))
    rcmd = shlex.split(cmd) if not shell else cmd
    result = subprocess.check_output(rcmd, shell=shell)
    print(result)
    return result


def install(t, version, cfile, envs):
    if envs:
        envs = envs.replace(",", " ")
    print("Downloading Mesos driver")
    run_cmd("curl -O {}".format(GITHUB_ZIP.format(version)))
    run_cmd("unzip {}".format(ZIP))

    print("Starting Mesos driver")
    cmd = ("nohup env DATERA_FRAMEWORK={fwk} {envs} {file} -config "
           "{config} > datera-dcos.log 2>&1 &")
    fwk = "dcos-mesos"
    if t == "docker":
        fwk = "dcos-docker"
    cmd = cmd.format(fwk=fwk, file=BINARY, config=cfile, envs=envs)
    run_cmd(cmd)


def check_config_file(cfile):
    try:
        with io.open(cfile) as f:
            try:
                json.load(f)
                return True
            except ValueError:
                print("Config cfile: {} is invalid".format(cfile))
                return False
    except IOError:
        print("Config cfile: {} does not exist".format(cfile))
        return False


def main(args):
    cfile = DEFAULT_CONFIG
    if args.config_file:
        cfile = args.config_file
    if not check_config_file(cfile):
        return CONF_ERROR
    install(args.type, args.driver_version, cfile, args.envs)
    return SUCCESS

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("type", help="Either 'docker' or 'mesos'")
    parser.add_argument("-d", "--driver_version")
    parser.add_argument("-f", "--config-file")
    parser.add_argument("-e", "--envs", default="",
                        help="Environment variables to add to the driver, "
                             "comma delimited list.  eg: --envs "
                             "ONE=val1,TWO=val2")
    parser.add_argument("-c", "--clean")
    args = parser.parse_args()

    sys.exit(main(args))
