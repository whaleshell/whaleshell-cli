# SPDX-FileCopyrightText: Copyright (c) 2026 lkmavi
# SPDX-License-Identifier: MIT
#
# Claude Code CLI preinstalled.

ARG BASE_IMAGE=osg-sandbox:local
FROM ${BASE_IMAGE}

ENTRYPOINT []
USER root
ENV DEBIAN_FRONTEND=noninteractive \
    HOME=/root

RUN curl -fsSL https://claude.ai/install.sh | bash \
    && CLAUDE_BIN="$(find /root /osg/data/home -type f -name claude 2>/dev/null | head -1)" \
    && if [ -z "$CLAUDE_BIN" ]; then CLAUDE_BIN="$(find /root /osg/data/home -type l -name claude 2>/dev/null | head -1)"; fi \
    && if [ -z "$CLAUDE_BIN" ]; then \
         echo "claude binary not found after install" >&2; \
         ls -laR /root/.local 2>/dev/null || true; \
         exit 1; \
       fi \
    && cp -L "$CLAUDE_BIN" /usr/local/bin/claude \
    && chmod 755 /usr/local/bin/claude \
    && /usr/local/bin/claude --version || true

RUN mkdir -p /osg/data/home/.local/bin /osg/data/bin /etc/profile.d \
    && ln -sf /usr/local/bin/claude /osg/data/home/.local/bin/claude \
    && printf '%s\n' 'export PATH="/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:$PATH"' > /etc/profile.d/osg-agent.sh

ENV HOME=/osg/data/home \
    PATH=/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/osg-init"]
CMD ["--", "sleep", "infinity"]
