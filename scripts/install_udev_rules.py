#!/usr/bin/env python

from __future__ import unicode_literals, print_function, division

import os
import shutil
import subprocess

UDEV_LOC = '/etc/udev/rules.d/'
ASSETS_DIR = os.path.join(
    os.path.dirname(os.path.abspath(__file__)), 'assets')
RULES_NAME = '99-iscsi-luns.rules'
RULES_START_LOC = os.path.join(ASSETS_DIR, RULES_NAME)
RULES_END_LOC = os.path.join(UDEV_LOC, RULES_NAME)
FETCH_NAME = 'fetch_device_serial_no.sh'
FETCH_START_LOC = os.path.join(ASSETS_DIR, FETCH_NAME)
FETCH_END_LOC = os.path.join('/sbin/', FETCH_NAME)


def main():
    print('Copying file from {} to {}'.format(RULES_START_LOC, RULES_END_LOC))
    shutil.copyfile(RULES_START_LOC, RULES_END_LOC)
    print('Copying file from {} to {}'.format(FETCH_START_LOC, FETCH_END_LOC))
    shutil.copyfile(FETCH_START_LOC, FETCH_END_LOC)

    print('Reloading udev rules')
    try:
        subprocess.check_call(['udevadm', 'control', '--reload'])
    except subprocess.CalledProcessError as e:
        print('Error reloading udev rules')
        print(e)
        return 1
    print('Successfully reloaded udev rules')
    return 0

if __name__ == '__main__':
    exit(main())
