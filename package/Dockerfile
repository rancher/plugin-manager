FROM rancher/agent-base:v0.2.0
RUN ln -s / /mnt/sda1
COPY plugin-manager start.sh /usr/bin/
ENTRYPOINT ["/usr/bin/start.sh"]
CMD ["plugin-manager"]
