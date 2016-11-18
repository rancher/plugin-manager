#!/bin/bash

cat > /etc/resolv.conf << EOF
search rancher.internal
nameserver 169.254.169.250
EOF

export DOCKER_API_VERSION=1.22

NETWORK_AGENT=$(docker ps -f label=io.rancher.container.system=NetworkAgent -q)

if [ -n "${NETWORK_AGENT}" ]; then
    docker rm -fv ${NETWORK_AGENT}
fi

exec "$@"
