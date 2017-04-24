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
