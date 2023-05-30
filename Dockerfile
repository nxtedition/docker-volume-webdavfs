FROM golang:1.10-alpine as builder1
COPY . /go/src/github.com/nxtedition/docker-volume-webdavfs
WORKDIR /go/src/github.com/nxtedition/docker-volume-webdavfs
RUN set -ex \
    && apk add --no-cache --virtual .build-deps \
    gcc libc-dev \
    && go install --ldflags '-extldflags "-static"' \
    && apk del .build-deps
CMD ["/go/bin/docker-volume-webdavfs"]

FROM golang:1.20.4-alpine as builder2
WORKDIR /go/src/github.com/miquels/webdavfs
RUN set -ex \
    && apk add --no-cache --virtual .build-deps \
    gcc libc-dev git fuse \
    && git clone https://github.com/miquels/webdavfs . \
    && CGO_ENABLED=0 go install \
    && apk del .build-deps

FROM alpine:3.7
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes
COPY --from=builder1 /go/bin/docker-volume-webdavfs .
COPY --from=builder2 /go/bin/webdavfs /sbin/webdavfs
CMD ["docker-volume-webdavfs"]
