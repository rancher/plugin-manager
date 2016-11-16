#!/bin/bash

cat > /etc/resolv.conf << EOF
search rancher.internal
nameserver 169.254.169.250
EOF

export DOCKER_API_VERSION=1.22
exec "$@"
