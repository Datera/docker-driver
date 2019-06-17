# Docker volume plugin for Datera Storage backend

This plugin uses Datera storage backend as distributed data storage for containers.

## Easy Installation (Docker v1.13+ required)

Before enabling the plugin, create the UDC configuration file on each node
```bash
$ sudo touch /etc/datera/datera-config.json
```
This is a JSON file with the following structure:
```json
{
      "mgmt_ip": "1.1.1.1",
      "username": "admin",
      "password": "password",
      "tenant": "/root",
      "api_version": "2.2",
      "ldap": ""
}
```
NOTE: The specified tenant MUST be accessible by the user account provided.

Install the iscsi-recv binary on all nodes
```bash
$ ./ddct install -u k8s_csi_iscsi
```
See http://github.com/Datera/ddct for instructions on how to download and
install ddct

Run this on each node that should use the Datera volume driver
```bash
$ sudo docker plugin install dateraiodev/docker-driver
```
Update the config file with the relevant information for the cluster then
run the following:
```bash
$ sudo docker plugin enable dateraiodev/docker-driver
```

### Usage
WHEN USING THE PLUGIN INSTALLATION METHOD YOU MUST REFER TO THE DRIVER BY
THE FORM "repository/image" NOT JUST "image"

Create a volume
```bash
$ sudo docker volume create --name my-vol --driver dateraiodev/docker-driver --opt size=5
```

Start your docker containers with the option `--volume-driver=dateraiodev/docker-driver` and use the first part of `--volume` to specify the remote volume that you want to connect to:
```bash
$ sudo docker run --volume-driver dateraiodev/docker-driver --volume datastore:/data alpine touch /data/hello
```

## The Other Way (DEPRECATED, required for Mesos installations)

### Installation

Download the latest release of the docker-driver from https://github.com/Datera/docker-driver/releases

Unzip the binary
```bash
unzip dddbin.zip
```

Install udev rules on each docker/mesos node
```bash
sudo ./scripts/install_udev_rules.py
```

### Start driver

This plugin doesn't create volumes in your Datera cluster yet, so you'll have to create them yourself first.

1 - Create the config file
```bash
$ sudo touch /root/.datera-config-file
```
This is a JSON file with the following structure:
```json
{
    "datera-cluster": "1.1.1.1",
    "username": "my-user",
    "password": "my-pass",
    "debug": false,
    "ssl": true,
    "tenant": "/root",
    "os-user": "root"
}
```
Fill out the cluster info in the config file

2 - Start the plugin using this command:
```bash
$ sudo ./dddbin
```
PLEASE NOTE: If installing on a Mesos node, the config variable
`"framework": "dcos-mesos"` or `"framework": "dcos-docker"` must be set

3a - Create a volume
```bash
$ sudo docker volume create --name my-vol --driver datera --opt size=5
```

3b - Start your docker containers with the option `--volume-driver=dateraiodev/docker-driver` and use the first part of `--volume` to specify the remote volume that you want to connect to:
```bash
$ sudo docker run --volume-driver dateraiodev/docker-driver --volume datastore:/data alpine touch /data/hello
```


# DCOS/MESOSPHERE Instructions

## CAVEATS
Currently DCOS and Mesos are very early in their external persistent volume support.
Because of this, their volume lifecycle is simpler than other ecosystems.  This means
only a subset of the Datera product functionality is available through DCOS and Mesos.
It also means there are a few wonky behaviors when using the external volume support for
DCOS.  You can read more about that here: https://dcos.io/docs/1.10/storage/external-storage/#potential-pitfalls

Download the latest release of the docker-driver from https://github.com/Datera/docker-driver/releases

Unzip the binary
```bash
unzip dddbin.zip
```

### Create config file on each node
```bash
$ sudo touch datera-config-file.txt
```
This is a JSON file with the following structure:
```json
{
    "datera-cluster": "1.1.1.1",
    "username": "my-user",
    "password": "my-pass",
    "debug": false,
    "ssl": true,
    "tenant": "/root",
    "os-user": "root"
}
```
### Copy config file to all relevant Mesos Agent nodes
```bash
scp -i ~/your_ssh_key datera-config-file.txt user@agent-node:/some/location/dddbin
```

### Start the driver with the config file
#### For Mesos Container nodes
```bash
sudo ./dddbin -config datera-config-template.txt
```

#### For Docker Container nodes
```bash
./dddbin -config datera-config-template.txt
```
The following json config keys are available to use for Docker container nodes
```text
{
    "datera-cluster": "1.1.1.1",                    # Datera Cluster Mgmt IP
    "username": "my-user",                          # Datera Account Username
    "password": "my-pass",                          # Datera Account Password
    "tenant": "/root",                              # Datera tenant ID
    "os-user": "root",                              # Name of local user to run under
    "ssl": true|false,                              # Use SSL for requests
    "framework": "bare"|"dcos-mesos"|"dcos-docker"  # Framework being used
    "volume": {
        "size": 16,
        "replica": 3,
        "template": null,
        "fstype": "ext4",
        "maxiops": null,
        "maxbw": null,
        "placement": "hybrid",
        "persistence": "manual",
        "clone-src": null
    }
}
```
PLEASE NOTE: Values provided under "volume" are for use by the dcos-docker
containerizer only and will hold true for all containers created on the system

### Create a service with Datera storage
#### Simple Mesos container setup
```json
{
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
          "name": "datera-mesos-test-volume",
          "provider": "dvdi",
          "options": {
            "dvdi/driver": "datera",
            }
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
```

The easiest way to generate this JSON config is to go to the DCOS UI
and create a new container with an external volume.  Then switch
"dvdi/driver": "rexray" --> "dvdi/driver": "datera"

The default size for a volume created without providing a "dvdi/size"
parameter is 16GB

#### More Complex Mesos Container
All 'dvdi/xxxxx' options must be double-quoted strings
```json
{
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
          "name": "datera-mesos-test-volume",
          "provider": "dvdi",
          "options": {
            "dvdi/driver": "datera",
            "dvdi/size": "33",
            "dvdi/replica": "3",
            "dvdi/maxIops": "100",
            "dvdi/maxBW": "200",
            "dvdi/placementMode": "hybrid",
            "dvdi/fsType": "ext4",
            "dvdi/cloneSrc": "some-app-instance"
            }
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
```
PLEASE NOTE: "containerPath" cannot start with a "/", this will break the Mesos
agent and cause the container spawn to fail

#### For Docker containers
You cannot specify any Datera specific information in this JSON blob due to a
limitation in the way DCOS interacts with Mesos and Docker. The relevant
options must be specified during driver instantiation time via the config
variables shown in an earlier section.
```json
{
  "id": "test-datera-docker",
  "instances": 1,
  "cpus": 0.1,
  "mem": 32,
  "cmd": "/bin/cat /dev/urandom > mesos-test/test.img",
  "container": {
    "type": "DOCKER",
    "docker": {
      "image": "alpine:3.1",
      "network": "HOST",
      "forcePullImage": true
    },
    "volumes": [
      {
        "containerPath": "/data/test-volume",
        "external": {
          "name": "datera-docker-volume",
          "provider": "dvdi",
          "options": { "dvdi/driver": "datera" }
        },
        "mode": "RW"
      }
    ]
  },
  "upgradeStrategy": {
    "minimumHealthCapacity": 0,
    "maximumOverCapacity": 0
  },
}
```
