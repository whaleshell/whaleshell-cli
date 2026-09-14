# SPDX-FileCopyrightText: Copyright (c) 2026 lkmavi
# SPDX-License-Identifier: MIT
#
# Cursor Agent CLI preinstalled (like OpenShell community base baking agent CLIs).
# Installer puts a bash wrapper + bundled node under ~/.local/share/cursor-agent;
# we relocate that tree to /opt so HOME can be /osg/data/home at runtime.

ARG BASE_IMAGE=osg-sandbox:local

FROM ${BASE_IMAGE} AS install
ENTRYPOINT []
USER root
ENV DEBIAN_FRONTEND=noninteractive \
    HOME=/root

RUN curl -fsSL https://cursor.com/install | bash \
    && test -L /root/.local/bin/agent \
    && mkdir -p /opt \
    && mv /root/.local/share/cursor-agent /opt/cursor-agent \
    && VER="$(find /opt/cursor-agent/versions -mindepth 1 -maxdepth 1 -type d | sort | tail -1)" \
    && test -n "$VER" \
    && test -x "$VER/cursor-agent" \
    && test -x "$VER/node" \
    && ln -sfn "$VER/cursor-agent" /usr/local/bin/agent \
    && ln -sfn "$VER/cursor-agent" /usr/local/bin/cursor-agent \
    && /usr/local/bin/agent --version

FROM ${BASE_IMAGE}
# Clear base entrypoint for setup RUN; restore osg-init below.
ENTRYPOINT []
USER root
COPY --from=install /opt/cursor-agent /opt/cursor-agent
RUN VER="$(find /opt/cursor-agent/versions -mindepth 1 -maxdepth 1 -type d | sort | tail -1)" \
    && ln -sfn "$VER/cursor-agent" /usr/local/bin/agent \
    && ln -sfn "$VER/cursor-agent" /usr/local/bin/cursor-agent \
    && mkdir -p /osg/data/home/.local/bin /osg/data/bin /etc/profile.d \
    && ln -sfn /usr/local/bin/agent /osg/data/home/.local/bin/agent \
    && printf '%s\n' 'export PATH="/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:$PATH"' > /etc/profile.d/osg-agent.sh \
    && /usr/local/bin/agent --version

ENV HOME=/osg/data/home \
    PATH=/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/osg-init"]
CMD ["--", "sleep", "infinity"]
