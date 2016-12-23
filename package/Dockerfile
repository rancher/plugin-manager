FROM rancher/agent-base:v0.3.0
RUN ln -s / /mnt/sda1
COPY plugin-manager start.sh /usr/bin/
ENTRYPOINT ["/rancher-entrypoint.sh", "/usr/bin/start.sh"]
CMD ["plugin-manager"]
