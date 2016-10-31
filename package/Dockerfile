FROM ubuntu:16.04
RUN apt-get update && \
    apt-get install -y iptables kmod curl
RUN curl -sLf https://get.docker.com/builds/Linux/x86_64/docker-1.12.3.tgz | tar xvzf - -C /usr/bin --strip-components=1 && \
    ln -s /usr/bin/docker-runc /usr/bin/runc
COPY plugin-manager /usr/bin/
CMD ["plugin-manager"]
