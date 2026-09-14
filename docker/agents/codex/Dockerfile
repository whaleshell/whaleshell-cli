# SPDX-FileCopyrightText: Copyright (c) 2026 lkmavi
# SPDX-License-Identifier: MIT
#
# OpenAI Codex CLI preinstalled.

ARG BASE_IMAGE=osg-sandbox:local
FROM ${BASE_IMAGE}

ENTRYPOINT []
USER root
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends nodejs npm \
    && npm install -g @openai/codex \
    && CODE_BIN="$(command -v codex || true)" \
    && if [ -z "$CODE_BIN" ]; then CODE_BIN="$(find "$(npm root -g)" -name 'codex' -type f 2>/dev/null | head -1)"; fi \
    && if [ -n "$CODE_BIN" ]; then ln -sf "$CODE_BIN" /usr/local/bin/codex; fi \
    && command -v codex \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /osg/data/home/.local/bin /osg/data/bin /etc/profile.d \
    && ln -sf /usr/local/bin/codex /osg/data/home/.local/bin/codex \
    && printf '%s\n' 'export PATH="/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:$PATH"' > /etc/profile.d/osg-agent.sh

ENV PATH=/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/osg-init"]
CMD ["--", "sleep", "infinity"]
