# syntax=docker/dockerfile:1

# The daemon image (BASE, built from the repository's Dockerfile) with
# Vulkan and Mesa's GPU drivers, so a self-hosted quality run can use an AMD
# or Intel card passed in with --device /dev/dri (QUALITY_GPU=vulkan).
# Debian 13 for a Mesa new enough for current cards; the daemon is a static
# binary, so it runs on either.
ARG BASE
FROM ${BASE} AS toskar

FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates libgomp1 libcurl4t64 libvulkan1 mesa-vulkan-drivers vulkan-tools \
 && rm -rf /var/lib/apt/lists/*
COPY --from=toskar /usr/local/bin/toskar /usr/local/bin/toskar
ENV YGGDRASIL_API_HOST=0.0.0.0 \
    YGGDRASIL_INTERNAL_HOST=0.0.0.0 \
    YGGDRASIL_DISCOVERY_ENABLED=true
EXPOSE 7331 7332
ENTRYPOINT ["/usr/local/bin/toskar"]
CMD ["--data-dir", "/data"]
