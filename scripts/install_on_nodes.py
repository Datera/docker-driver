#!/usr/bin/env python

from __future__ import print_function, unicode_literals, division

import argparse
import os
import sys

import paramiko
import prettytable

INITIAL_SSH_TIMEOUT = 600
FOLDER = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
INSTALL_SCRIPT = "install_docker_driver.py"
INSTALL_SCRIPT_PATH = os.path.join(FOLDER, 'scripts', INSTALL_SCRIPT)

# Python2.7 compat
try:
    str = unicode
except NameError:
    pass


def pretty_table(headers, row_list):
    table = prettytable.PrettyTable()
    table.field_names = headers
    for row in row_list:
        table.add_row(row)
    return table


def get_ssh(ip, username, password=None, keyfile=None):
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(
        paramiko.AutoAddPolicy())
    if keyfile:
        # Used for Ubuntu Cloud server images without passwords
        k = paramiko.RSAKey.from_private_key_file(keyfile)
        ssh.connect(
            hostname=ip,
            username=username,
            banner_timeout=INITIAL_SSH_TIMEOUT,
            pkey=k)
    else:
        # Normal username/password usage
        ssh.connect(
            hostname=ip,
            username=username,
            password=password,
            banner_timeout=INITIAL_SSH_TIMEOUT)
    return ssh


def exec_command(s, command, fail_ok=False):
    ip = s._host_keys._entries[0].hostnames[0]
    print("Executing command: {} on VM: {}".format(command, ip))
    _, stdout, stderr = s.exec_command(command)
    exit_status = stdout.channel.recv_exit_status()
    result = None
    if int(exit_status) == 0:
        result = stdout.read()
    elif fail_ok:
        result = stderr.read()
    else:
        raise EnvironmentError(
            "Nonzero return code: {} stderr: {}".format(
                exit_status,
                stderr.read()))
    print("Result:", str(result, 'utf-8'))
    return result


def parse_node(arg):
    username, ip, password, keyfile = '', '', '', ''
    try:
        username, ip, password, keyfile = arg.strip(',').split(',')
    except ValueError:
        username, ip, password = arg.strip(',').split(',')

    if all((password, keyfile)):
        print("Error parsing argument: {}".format(arg))
        print("Must provide EITHER password, or keyfile location,"
              "but both found")
        sys.exit(1)
    elif not any((password, keyfile)):
        print("Error parsing argument: {}".format(arg))
        print("Must provide EITHER password, or keyfile location,"
              "but neither found")
        sys.exit(1)
    return {"username": username,
            "ip": ip,
            "password": password,
            "keyfile": keyfile}


def install_node(cluster_ip, node_dict, cluster_user="admin",
                 cluster_pass="password", tenant="root"):
    print("Connecting to node: {}".format(node_dict["ip"]))
    ssh = get_ssh(node_dict["ip"],
                  node_dict["username"],
                  node_dict["password"],
                  os.path.expanduser(node_dict["keyfile"]))
    print("Copying installer script to node: {}".format(node_dict["ip"]))
    sftp = ssh.open_sftp()
    sftp.put(INSTALL_SCRIPT_PATH, INSTALL_SCRIPT)
    exec_command(ssh, "chmod +x {}".format(INSTALL_SCRIPT))
    exec_command(ssh, "sudo apt-get install curl")
    exec_command(ssh, "./{} {} --dc-username {} --dc-password {} "
                      "--dc-tenant {}".format(
                          INSTALL_SCRIPT, cluster_ip, cluster_user,
                          cluster_pass, tenant))


def main(args):
    nodes = []
    for arg in args.node:
        nodes.append(parse_node(arg))

    print("\nSetting up the following nodes")
    print("==============================")
    print("Datera Cluster: {}".format(args.datera_cluster_ip))
    print(pretty_table(["Username", "IP", "Password", "Keyfile"],
          [[node["username"], node["ip"], node["password"], node["keyfile"]]
           for node in nodes]))
    for node in nodes:
        install_node(args.datera_cluster_ip, node)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        formatter_class=argparse.RawTextHelpFormatter)
    parser.add_argument(
        "datera_cluster_ip")
    parser.add_argument(
        "node",
        nargs="+",
        help="Node Info in the following space-delimited format:\n\n"
             "username,ip_address,password(optional),keyfile(optional)\n\n"
             "Where EITHER 'password' OR 'keyfile' is provided\n\n Eg:\n"
             "testuser,1.1.1.1,testpass,, or "
             "testuser,1.1.1.1,/keyfile/loc")
    args = parser.parse_args()
    sys.exit(main(args))
