# Docker volume plugin for Datera Storage backend

This plugin uses Datera storage backend as distributed data storage for containers.

There are two ways to use this plugin

## Easy Installation (Docker v1.13+ required)

Run this on each node that should use the Datera volume driver
```
$ sudo docker install dateraio/datera
```
Before enabling the plugin, create the configuration file
```
$ sudo touch /root/.datera-config-file
```
This is a JSON file with the following structure:
```
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
Update the config file with the relevant information for the cluster then
run the following:
```
$ sudo docker plugin enable dateraiodev/docker-driver
```

Install udev rules on each docker node (from the scripts directory)
```
sudo ./install_udev_rules.py
```

### Usage
WHEN USING THE PLUGIN INSTALLATION METHOD YOU MUST REFER TO THE DRIVER BY
THE FORM "repository/image" NOT JUST "image"

Create a volume
```
$ sudo docker volume create --name my-vol --driver dateraiodev/docker-driver --opt size=5
```

Start your docker containers with the option `--volume-driver=dateraiodev/docker-driver` and use the first part of `--volume` to specify the remote volume that you want to connect to:
```
$ sudo docker run --volume-driver dateraiodev/docker-driver --volume datastore:/data alpine touch /data/hello
```

## The Hard Way (building from source, not recommended)

### Building
```
$ make
```

### Running Unit Tests

```
$ make test
```

### Installation

{Update with binary location when we have one}

Install udev rules on each docker/mesos node
```
sudo ./scripts/install_udev_rules.py
```

### Starting the newly built driver

This plugin doesn't create volumes in your Datera cluster yet, so you'll have to create them yourself first.

1 - Create the config file
```
$ sudo touch /root/.datera-config-file
```
This is a JSON file with the following structure:
```
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
```
$ sudo ./dddbin
```

3a - Create a volume
```
$ sudo docker volume create --name my-vol --driver datera --opt size=5
```

3b - Start your docker containers with the option `--volume-driver=dateraiodev/docker-driver` and use the first part of `--volume` to specify the remote volume that you want to connect to:
```
$ sudo docker run --volume-driver dateraiodev/docker-driver --volume datastore:/data alpine touch /data/hello
```


## DCOS/MESOSPHERE Instructions

### Build the driver
```
make
```

### Copy the driver to all relevant Mesos Agent nodes
```
scp -i ~/your_ssh_key dddbin user@agent-node:/some/location/dddbin
```

### Create config file
```
$ sudo touch datera-config-file.txt
```
This is a JSON file with the following structure:
```
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
```
scp -i ~/your_ssh_key datera-config-file.txt user@agent-node:/some/location/dddbin
```

### Start the driver with the config file
#### For Mesos Container nodes
```
sudo ./dddbin -config datera-config-template.txt
```

#### For Docker Container nodes
```
sudo env DATERA_FRAMEWORK=dcos DATERA_VOL_SIZE=33 ./dddbin -config datera-config-template.txt
```
The following environment variables are available to use for Docker container nodes
```
DATERA_FRAMEWORK=dcos
DATERA_VOL_SIZE=XX
DATERA_REPLICAS=X
DATERA_PLACEMENT=hybrid
DATERA_MAX_IOPS=100
DATERA_MAX_BW=100
DATERA_FSTYPE=ext4
```
PLEASE NOTE: These environment variables are necessary only for Docker
containers and will be global for all Docker containers on each Mesos Agent
node. Eg: if `DATERA_VOL_SIZE=100` is set on an Agent node EVERY SINGLE DOCKER
CONTAINER USING DATERA STORAGE WILL USE THIS VALUE.  Mesos containers are
unaffected

### Create a service with Datera storage
#### For Mesos containers
All 'dvdi/xxxxx' options must be double-quoted strings
```
{
  "id": "test-datera-2",
  "instances": 1,
  "cpus": 0.1,
  "mem": 32,
  "cmd": "/usr/bin/tail -f /dev/null",
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
            "dvdi/fsType": "ext4"
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
options must be specified during driver instantiation time via the environment
variables shown in an earlier section.
```
{
  "id": "test-datera-docker",
  "instances": 1,
  "cpus": 0.1,
  "mem": 32,
  "cmd": "/usr/bin/tail -f /dev/null",
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
