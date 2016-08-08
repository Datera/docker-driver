# Docker volume plugin for Datera Storage backend

This plugin uses Datera storage backend as distributed data storage for containers.

## Installation

Using go (until we get proper binaries):

```
$ go get github.com/datera/datera-volume-driver
```

## Usage

This plugin doesn't create volumes in your Datera cluster yet, so you'll have to create them yourself first.

1 - Start the plugin using this command:

```
$ sudo docker-volume-datera -servers dss-1:dss-2:dss-3
```

We use the flag `-servers` to specify where to find the Datera servers. The server names are separated by colon.

2 - Start your docker containers with the option `--volume-driver=datera` and use the first part of `--volume` to specify the remote volume that you want to connect to:

```
$ sudo docker run --volume-driver datera --volume datastore:/data alpine touch /data/helo
```

### Volume creation on demand

This extension can create volumes on the remote cluster if you install https://github.com/datera/datera-rest in one of the nodes of the cluster.

You need to set two extra flags when you start the extension if you want to let containers to create their volumes on demand:

- rest: is the URL address to the remote api.
- datera-base: is the base path where the volumes will be created.

This is an example of the command line to start the plugin:

```
$ docker-volume-datera -servers dss-1:dss \
    -rest http://dss-1:9000 -dss-base /var/lib/datera/volumes
```

These volumes are replicated among all the peers in the cluster that you specify in the `-servers` flag.

## LICENSE

MIT
