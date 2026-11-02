ARG PYTHON_IMAGE=python:3.13.9-slim-bookworm@sha256:b685a4fa58bb19d1814d78a1ec0f0208f351452724f78b20212c984d6e124a34
FROM ${PYTHON_IMAGE}

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates openssh-client \
    && rm -rf /var/lib/apt/lists/*

COPY runtime/requirements.txt /tmp/requirements.txt
RUN python -m pip install --no-cache-dir --requirement /tmp/requirements.txt \
    && rm /tmp/requirements.txt

RUN groupadd --gid 65532 ansible \
    && useradd --create-home --gid 65532 --no-log-init --shell /usr/sbin/nologin --uid 65532 ansible

ENV HOME=/home/ansible
USER 65532:65532
WORKDIR /workspace
