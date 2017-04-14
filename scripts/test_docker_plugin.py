#!/usr/bin/env python

from __future__ import unicode_literals, print_function, division

import argparse
import json
import re
import shlex
import subprocess
import sys
import time
import uuid

UUID4_STR_RE = ("[a-f0-9]{8}-?[a-f0-9]{4}-?4[a-f0-9]{3}-?[89ab]"
                "[a-f0-9]{3}-?[a-f0-9]{12}")
UUID4_RE = re.compile(UUID4_STR_RE)
PLUGIN_NAME = "dateraio/datera"
CONFIG_FILE = "/root/.datera-config-file"
CONFIG_FILE_CONTENTS = """
{{
    "datera-cluster": "{cluster}",
    "username": "{username}",
    "password": "{password}",
    "debug": false,
    "ssl": true,
    "tenant": "/root",
    "os-user": "root"
}}
"""

_args = None


#########
# Tests #
#########

def test_volume_creation_removal():
    vol_size = 5
    vol_replica = 1
    name = create_vol(size=vol_size, replica=vol_replica)
    time.sleep(1)
    vol = json.loads(excmd("docker volume inspect {}".format(name)))[0]
    assert vol['Driver'] == "{}:latest".format(PLUGIN_NAME)
    assert vol['Name'] == name
    assert vol['Options']['replica'] == str(vol_replica)
    assert vol['Options']['size'] == str(vol_size)
    assert vol['Scope'] == "global"
    excmd("docker volume rm {}".format(name))


def test_volume_mount_unmount():
    vol_name = create_vol()
    name = create_container(vol_name)
    result = excmd("docker exec {} sh -c 'cd /mnt/dvol && df -h .'".format(
        name))
    # Easy check to see if we mounted the volume correctly
    assert "by-uuid" in result
    uid = UUID4_RE.search(result).group(0)
    # Check that block device is present
    assert uid in excmd("ls /dev/disk/by-uuid/")
    excmd("docker rm -f {}".format(name))
    # Check that block device in not present after container removal
    assert uid not in excmd("ls /dev/disk/by-uuid/")


#################
# Library Funcs #
#################

def dprint(*args, **kwargs):
    if _args.verbose:
        print(*args, **kwargs)


def excmd(cmd):
    dprint("Running command: {}".format(cmd))
    result = subprocess.check_output(shlex.split(cmd))
    dprint("Result:")
    dprint(result)
    return result


def setup_plugin(cluster_ip, cluster_login, cluster_password):
    p = subprocess.Popen(
        shlex.split("docker plugin install {}".format(PLUGIN_NAME)),
        stdin=subprocess.PIPE)
    p.communicate("y")
    with open(CONFIG_FILE, 'w+') as f:
        f.write(CONFIG_FILE_CONTENTS.format(
            cluster=cluster_ip,
            username=cluster_login,
            password=cluster_password))
    if not json.loads(
            excmd("docker plugin inspect {}".format(
                PLUGIN_NAME)))[0]['Enabled']:
        excmd("docker plugin enable {}".format(PLUGIN_NAME))
    if not json.loads(
            excmd("docker plugin inspect {}".format(
                PLUGIN_NAME)))[0]['Enabled']:
        raise EnvironmentError("Plugin not enabled")


def create_vol(name=None, size=5, replica=1):
    if not name:
        name = str(uuid.uuid4())
    excmd("docker volume create --name {} "
          "--driver {} --opt size={} --opt replica={}".format(
              name, PLUGIN_NAME, size, replica))
    return name


def create_container(vol):
    excmd("docker pull alpine")
    result = excmd("docker run --rm --detach -v {}:/mnt/dvol alpine "
                   "sleep 60".format(vol))
    return result


def main(args):
    setup_plugin(args.cluster_ip, args.cluster_login, args.cluster_password)
    test_volume_creation_removal()
    test_volume_mount_unmount()

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('cluster_ip')
    parser.add_argument('cluster_login')
    parser.add_argument('cluster_password')
    parser.add_argument('-v', '--verbose', action='store_true')
    _args = parser.parse_args()
    sys.exit(main(_args))
