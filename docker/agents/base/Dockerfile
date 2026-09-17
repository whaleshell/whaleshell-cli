# SPDX-FileCopyrightText: Copyright (c) 2026 lkmavi
# SPDX-License-Identifier: MIT
#
# Thin Debian base for agent images.
# Prefer `task runtime:image:cli` (osg-sandbox:local) — OpenShell-like toolset
# (python/node/gh/agents) lives in osg-runtime/images/sandbox Dockerfile target cli.
# This file remains a minimal fallback for agent-layer Dockerfiles.

ARG DEBIAN_BASE=debian:bookworm
FROM ${DEBIAN_BASE}

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    bash \
    && rm -rf /var/lib/apt/lists/*

# osg-init is copied by the build task when present; agent Dockerfiles may skip it
# and rely on the host bind-mount of osg-init at create time.
COPY osg-init /usr/local/bin/osg-init
RUN chmod 0755 /usr/local/bin/osg-init || true

ENV OSG_POLICY=/osg/policy.yaml \
    HOME=/osg/data/home \
    PATH=/osg/data/home/.local/bin:/osg/data/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/osg-init"]
CMD ["--", "sleep", "infinity"]
