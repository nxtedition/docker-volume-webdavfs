FROM alpine:3.6

RUN apk add --no-cache davfs2
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
' >> /etc/davfs2/davfs2.conf
COPY docker-volume-davfs docker-volume-davfs

CMD ["docker-volume-davfs"]
