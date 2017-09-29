#!/bin/bash
set -e

for i in $(curl -s --unix /var/run/docker.sock http://localhost/info | jq -r .DockerRootDir) /var/lib/docker /run /var/run; do
    for m in $(tac /proc/mounts | awk '{print $2}' | grep ^${i}/); do
        umount $m || true
    done
done
