FROM alpine:3.6

RUN apk add --no-cache davfs2
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes
RUN echo -e "\ndav_user root\ndav_group root\nkernel_fs fuse\nbuf_size 16" >> /etc/davfs2/davfs2.conf

COPY docker-volume-davfs docker-volume-davfs

CMD ["docker-volume-davfs"]
