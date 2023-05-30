FROM golang:1.10-alpine as builder
COPY . /go/src/github.com/nxtedition/docker-volume-webdavfs
WORKDIR /go/src/github.com/nxtedition/docker-volume-webdavfs
RUN set -ex \
    && apk add --no-cache --virtual .build-deps \
    gcc libc-dev \
    && go install --ldflags '-extldflags "-static"' \
    && apk del .build-deps
CMD ["/go/bin/docker-volume-webdavfs"]

FROM alpine:3.7
RUN apk add --no-cache webdavfs2
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes
RUN echo -e $'\
dav_user        root\n\
dav_group       root\n\
kernel_fs       fuse\n\
buf_size        16\n\
connect_timeout 10\n\
read_timeout    30\n\
retry           10\n\
max_retry       300\n\
dir_refresh     30\n\
# file_refresh    10\n\
' >> /etc/webdavfs2/webdavfs2.conf
COPY --from=builder /go/bin/docker-volume-webdavfs .
CMD ["docker-volume-webdavfs"]
