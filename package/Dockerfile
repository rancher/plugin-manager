FROM rancher/agent-base:v0.3.0
RUN ln -s / /mnt/sda1

RUN apt-get update && \
    apt-get install --no-install-recommends -y \
    ipset && \
    rm -rf /var/lib/apt/lists/*

COPY plugin-manager start.sh oldvethsync /usr/bin/
ENTRYPOINT ["/rancher-entrypoint.sh", "/usr/bin/start.sh"]
CMD ["plugin-manager"]
