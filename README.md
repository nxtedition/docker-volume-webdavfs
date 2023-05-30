# Docker volume plugin for webdavfs

This plugin allows you to mount remote folder using webdavfs2 in your container easily.

[![TravisCI](https://travis-ci.org/nxtedition/docker-volume-webdavfs.svg)](https://travis-ci.org/nxtedition/docker-volume-webdavfs)
[![Go Report Card](https://goreportcard.com/badge/github.com/nxtedition/docker-volume-webdavfs)](https://goreportcard.com/report/github.com/nxtedition/docker-volume-webdavfs)

## Usage

1 - Install the plugin

```sh
docker plugin install nxtedition/webdavfs # or docker plugin install nxtedition/webdavfs DEBUG=1
```

2 - Create a volume

```sh
$ docker volume create \
  -d nxtedition/webdavfs \
  -o url=<https://user:passwd@host/path> \
  -o uid=1000 -o gid=1000 davvolume
davvolume
$ docker volume ls
DRIVER              VOLUME NAME
local               2d75de358a70ba469ac968ee852efd4234b9118b7722ee26a1c5a90dcaea6751
local               842a765a9bb11e234642c933b3dfc702dee32b73e0cf7305239436a145b89017
local               9d72c664cbd20512d4e3d5bb9b39ed11e4a632c386447461d48ed84731e44034
local               be9632386a2d396d438c9707e261f86fd9f5e72a7319417901d84041c8f14a4d
local               e1496dfe4fa27b39121e4383d1b16a0a7510f0de89f05b336aab3c0deb4dda0e
nxtedition/webdavfs        davvolume
```

**NOTE:** If you have special characters within your username or/and password you can use `-o username=<user>` and `-o password=<password>`.
You can check if your url is correctly parsed here: https://play.golang.org/p/JBtsIJjURsK

For more options refer to `mount.webdavfs --help`.

3 - Use the volume

```
$ docker run -it -v davvolume:<path> busybox ls <path>
```

## Global `/etc/webdav/webdav.conf` atm.
```ini
dav_user        root
dav_group       root
kernel_fs       fuse
buf_size        16
connect_timeout 10
read_timeout    30
retry           10
max_retry       300
dir_refresh     30
# file_refresh    10
```

## TODO
- [ ] set custom `webdav.conf`

## THANKS

- https://github.com/docker/go-plugins-helpers
- https://github.com/vieux/docker-volume-sshfs as template for this repo

## LICENSE

MIT
