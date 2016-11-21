FROM ubuntu:16.04
RUN apt-get update && \
    apt-get install -y iptables kmod curl iproute2 tcpdump iputils-ping
RUN curl -sLf https://get.docker.com/builds/Linux/x86_64/docker-1.12.3.tgz | tar xvzf - -C /usr/bin --strip-components=1 docker/docker
RUN rm /var/run \
    && mkdir /var/run
COPY plugin-manager start.sh /usr/bin/
ENTRYPOINT ["/usr/bin/start.sh"]
CMD ["plugin-manager"]
