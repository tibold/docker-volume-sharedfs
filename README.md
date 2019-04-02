# docker-volume-sharedfs

Docker Volume plugin to create persistent volumes on a shared filesystem such as beegfs or gfs2.

## Preconditions

- Your choice of shared filesystem has to be operational.
- The plugin does not do anything to verify the operation of the filesystem.

## Installation

### Docker Hub

docker plugin install tibold/sharedfs --alias sharedfs
docker plugin set sharedfs volumes.source=<wherever you want to store the volumes>
docker plugin enable sharedfs

### Systemd

A pre-built binary as well as `rpm` and `deb` packages are available from the [releases](https://github.com/tibold/docker-volume-sharedfs/releases) page.

### From source code

Get the code:

    go get github.com/tibold/docker-volume-sharedfs

The plugin uses [govendor](https://github.com/kardianos/govendor) to manage dependencies.

    go get -u github.com/kardianos/govendor

Restore dependencies:
    
    govendor sync

Build the plugin:

    go build

#### Docker plugin

There is a `Makefile.docker` that describes the steps required:

    make -f Makefile.docker

This is equivalent to the `docker plugin install` command.
Set the volumes location:
    
    docker plugin set sharedfs volumes.source=<wherever you want to store the volumes>

Enable the plugin:

    docker plugin enable tibold/sharedfs:next

#### RedHat/CentOS 7

An rpm can be built with:

    make -f Makefile.systemd rpm

Then install and start the service:

    yum localinstall docker-volume-sharedfs-$VERSION.rpm
    systemctl start docker-volume-sharedfs

#### Debian 8

Debian packages are currently built on a RedHat system, but the `Makefile.systemd`
describes which packages to install on Debian when building from scratch.
Building the actual package can be done on a Debian system without Makefile modifications:

    make -f Makefile.systemd deb

Now you can install and start the service:

    dpkg -i docker-volume-sharedfs_$VERSION.deb
    systemctl start docker-volume-sharedfs


## Usage

First create a volume:

    docker volume create -d sharedfs --name postgres-portroach

Then use the volume by passing the name (`postgres-1`):

    docker run -ti -v postgres-portroach:/var/lib/postgresql/data --volume-driver=sharedfs -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres

Inspect the volume:

    docker volume inspect postgres-portroach

Remove the volume (note that this will _not_ remove the actual data):

    docker volume rm postgres-portroach

### Options

* `exclusive`: Restrict to one concurrent mount. Default: `true`
* `protected`: Forbid deleting the data from disk. Default: `false`

When protected mode is activated, the volume will be removed from docker's bookeeping, but the data will be left intact. Recreating the volume with the same name will reuse the already existing data files.

Changing the properties after a volume was created is not supported. When creating a volume in docker, if the volume already exists on disk the options provided through docker are ignored. This also means that a protected volume can not be deleted by docker ever.

### Volume

When the volume is created in docker the driver creates the following folder structure:

```
. Volumes root
+-- _data                  : stores data files
+-- _locks                 : stores lock files
|  +-- <hostname>.lock     : a lock file is created by every driver instance
|  +-- <mount id>.mount    : a mount file is created for every mount when not exclusive
|  +-- exclusive.mount     : a mount file is created when mounting an exclusive volume
+-- meta.json              : stores the metadata about the volume
```

Every mount file will have the hostname of the mountee written in it.

`docker inspect volume <volume-name>` will list all locks and mounts and display the used options in the `Status` field.

### Deleting protected volumes

Navigate to the volume you want to delete in the filesystem. If the the `_locks` folder is empty you can manually delete the volume. Do __not__ delete the volume if there are any files in the `_locks` folder.

## Roadmap

- No outstanding features/requests.

## License

MIT, please see the LICENSE file.

## Credit

This project was inspired by:
* https://github.com/vieux/docker-volume-sshfs
* https://github.com/RedCoolBeans/docker-volume-beegfs

## Contributing

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request
