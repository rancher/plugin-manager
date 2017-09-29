FROM rancher/agent-base:v0.3.0
RUN ln -s / /mnt/sda1
COPY plugin-manager umount.sh oldvethsync /usr/bin/
ENTRYPOINT ["/rancher-entrypoint.sh"]
CMD ["plugin-manager"]
