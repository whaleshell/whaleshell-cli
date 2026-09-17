# SPDX-FileCopyrightText: Copyright (c) 2026 the osg authors
# SPDX-License-Identifier: MIT
#
# OpenAI Codex CLI preinstalled.
# Base (osg-runtime sandboxes/base) already ships NodeSource Node.js + npm —
# Debian's `npm` package conflicts with that nodejs (Conflicts: npm).

ARG BASE_IMAGE=osg-sandbox:local
FROM ${BASE_IMAGE}

ENTRYPOINT []
USER root
ENV DEBIAN_FRONTEND=noninteractive \
    HOME=/root

# Refresh/ensure @openai/codex on the NodeSource toolchain already in base.
RUN npm install -g @openai/codex \
    && CODE_BIN="$(command -v codex || true)" \
    && if [ -z "$CODE_BIN" ]; then CODE_BIN="$(find "$(npm root -g)" -name 'codex' -type f 2>/dev/null | head -1)"; fi \
    && if [ -z "$CODE_BIN" ]; then \
         echo "codex binary not found after npm install -g @openai/codex" >&2; \
         npm root -g; ls -la "$(npm root -g)/@openai" 2>/dev/null || true; \
         exit 1; \
       fi \
    && ln -sfn "$CODE_BIN" /usr/local/bin/codex \
    && /usr/local/bin/codex --version || true \
    && npm cache clean --force

RUN mkdir -p /osg/data/home/.local/bin /osg/data/bin /etc/profile.d \
    && ln -sfn /usr/local/bin/codex /osg/data/home/.local/bin/codex \
    && printf '%s\n' 'export PATH="/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:$PATH"' > /etc/profile.d/osg-agent.sh

ENV HOME=/osg/data/home \
    PATH=/usr/local/bin:/osg/data/home/.local/bin:/osg/data/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/osg-init"]
CMD ["--", "sleep", "infinity"]
