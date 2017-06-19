FROM alpine:3.6

RUN apk add --no-cache davfs2

RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes

COPY docker-volume-davfs docker-volume-davfs

CMD ["docker-volume-davfs"]
