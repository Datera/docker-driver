#!/usr/bin/env python

from __future__ import unicode_literals, print_function, division

import argparse
import io
import json
import requests
import sys
import uuid

BASE = "http://{}/service/marathon/v2/apps"


BASIC_APP = {
  "id": "test-datera-2",
  "instances": 1,
  "cpus": 0.1,
  "mem": 32,
  "cmd": "/bin/cat /dev/urandom > mesos-test/test.img",
  "container": {
    "type": "MESOS",
    "volumes": [
      {
        "containerPath": "mesos-test",
        "external": {
          "name": "mesos-test-volume-2",
          "provider": "dvdi",
          "options": {
            "dvdi/driver": "datera"}
        },
        "mode": "RW"
      }
    ]
  },
  "upgradeStrategy": {
    "minimumHealthCapacity": 0,
    "maximumOverCapacity": 0
  }
}


def get_app(app_name):
    url = "/".join((BASE, app_name))
    resp = requests.get(url)
    if resp.status_code == 200:
        return resp.json()
    raise ValueError("Invalid App at url: {}".format(url))


def list_apps(simple=False):
    apps = []
    resp = requests.get(BASE)
    for app in resp.json()['apps']:
        if simple:
            apps.append(app['id'])
            continue
        newapp = get_app(app['id'])
        try:
            apps.append((app['id'], newapp['app']['tasks'][0]['state']))
        except (KeyError, IndexError):
            apps.append((app['id'], None))
    return apps


def post_app(arglist, app_name, volume_name, json_file):
    if not volume_name:
        raise ValueError("Must specify volume_name")
    headers = {'Content-Type': 'application/json',
               'Accept': 'application/json'}
    if json_file:
        with io.open(json_file) as f:
            # j = json.load(f)
            return requests.post(BASE, data=f.read(), headers=headers).json()
    if not app_name:
        app_name = str(uuid.uuid4())
    arglist = arglist.split(',')
    j = BASIC_APP.copy()
    for arg in arglist:
        k, v = arg.split("=")
        k = "dvdi/" + k
        j["container"]["volumes"][0]["external"]["options"][k] = v
    j['id'] = app_name
    # j["container"]["volumes"][0]["containerPath"] = volume_name
    # j["container"]["volumes"][0]["hostPath"] = "/mnt/" + volume_name
    j["container"]["volumes"][0]["external"]["name"] = volume_name
    print(json.dumps(j, indent=4))
    return requests.post(BASE, data=json.dumps(j), headers=headers).json()


def delete_app(app_name):
    url = "/".join((BASE, app_name))
    resp = requests.delete(url)
    return resp.json()


def clear_apps():
    for app_id in list_apps(simple=True):
        print(delete_app(app_id))


def main(args):
    global BASE
    BASE = BASE.format(args.dcos_ip)
    if args.post_app:
        print(
            json.dumps(post_app(
                args.post_app, args.app_name, args.volume_name,
                args.json_file),
                      indent=4))
        return 0
    elif args.delete_app:
        r = raw_input("Are you sure you want to delete: {}?, [Y/n]\n".format(
                args.delete_app))
        if r.strip() == "Y":
            print(delete_app(args.delete_app))
            return 0
        else:
            print("Aborting")
            return 1
    elif args.app_name:
        print(json.dumps(get_app(args.app_name), indent=4))
        return 0
    elif args.list_apps:
        print("\n".join(map("\t\t".join, list_apps())))
        return 0
    elif args.clear_all_apps:
        r = raw_input("Are you sure you want to delete all the apps?, [Y/n]\n")
        if r.strip() == "Y":
            clear_apps()
            return 0
        else:
            print("Aborting")
            return 1
    else:
        print("Not a valid argument combo")
        return 1

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("dcos_ip")
    parser.add_argument('-c', '--clear-all-apps', action='store_true')
    parser.add_argument('-d', '--delete-app')
    parser.add_argument('-n', '--volume-name')
    parser.add_argument('-j', '--json-file')
    parser.add_argument('-p', '--post-app',
                        help="Additional arguments can be specified via comma "
                             "delimited list of: 'arg1=val1,arg2=val2'")
    parser.add_argument('-a', '--app-name')
    parser.add_argument('-l', '--list-apps', action='store_true')
    parser.add_argument('-v', '--verbose', action='store_true')
    _args = parser.parse_args()
    sys.exit(main(_args))
