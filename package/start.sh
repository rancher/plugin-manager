#!/bin/bash
set -e

if [ -n "$METADATA_IP" ]; then
    cat > /etc/resolv.conf << EOF
search rancher.internal
nameserver ${METADATA_IP}
EOF
fi

for i in $(curl -s --unix /var/run/docker.sock http://localhost/info | jq -r .DockerRootDir) /var/lib/docker /run /var/run; do
    for m in $(tac /proc/mounts | awk '{print $2}' | grep ^${i}/); do
        umount $m || true
    done
done

export DOCKER_API_VERSION=1.22

NETWORK_AGENT=$(docker ps -f label=io.rancher.container.system=NetworkAgent -q)

if [ -n "${NETWORK_AGENT}" ]; then
    docker rm -fv ${NETWORK_AGENT}
fi

if [ -n "$DOCKER_BRIDGE" ] && [ -n "$METADATA_IP" ]; then
    ip route add ${METADATA_IP}/32 dev ${DOCKER_BRIDGE} 2>/dev/null || true
fi

exec "$@"
