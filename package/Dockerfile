FROM ubuntu:16.04
RUN apt-get update && \
    apt-get install -y iptables kmod curl iproute2 tcpdump iputils-ping
RUN curl -sLf https://get.docker.com/builds/Linux/x86_64/docker-1.12.3.tgz | tar xvzf - -C /usr/bin --strip-components=1 docker/docker-runc docker/docker \
    && mv /usr/bin/docker-runc /usr/bin/runc \
    && curl -sLf https://get.docker.com/builds/Linux/x86_64/docker-1.11.2.tgz | tar xvzf - -C /usr/bin --strip-components=1 docker/docker-runc \
    && mv /usr/bin/docker-runc /usr/bin/runc-1.11 \
    && mkdir -p /mnt/sda1 \
    && ln -s /var /mnt/sda1/var
RUN rm /var/run \
    && mkdir /var/run
COPY plugin-manager start.sh runc-1.10 /usr/bin/
ENTRYPOINT ["/usr/bin/start.sh"]
CMD ["plugin-manager"]
