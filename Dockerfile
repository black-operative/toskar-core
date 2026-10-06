# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The commit the image was built from, for /api/v1/version.
ARG COMMIT=""
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/yeixio/toskar-core/internal/version.Commit=${COMMIT}" -o /out/toskar ./cmd/daemon \
 && CGO_ENABLED=0 go build -o /out/clustercheck ./cmd/clustercheck

# The image for an AMD or Intel GPU (docker build --target gpu): Vulkan and
# Mesa's drivers on Debian 13, whose Mesa supports current cards, and
# pciutils so the daemon can name the card. Run it with the card passed in:
# --device /dev/dri --group-add "$(getent group render | cut -d: -f3)"
# (docs/gpu.md). The default image, below, runs on the CPU.
FROM debian:trixie-slim AS gpu
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates libgomp1 libcurl4t64 libvulkan1 mesa-vulkan-drivers vulkan-tools pciutils \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/toskar /usr/local/bin/toskar
RUN ln -s toskar /usr/local/bin/yggdrasil-daemon
COPY --from=build /out/clustercheck /usr/local/bin/clustercheck
ENV YGGDRASIL_API_HOST=0.0.0.0 \
    YGGDRASIL_INTERNAL_HOST=0.0.0.0 \
    YGGDRASIL_DISCOVERY_ENABLED=true
EXPOSE 7331 7332
ENTRYPOINT ["/usr/local/bin/toskar"]
CMD ["--data-dir", "/data"]

# The default image: last, so a plain docker build makes it.
FROM debian:bookworm-slim AS runtime
# libgomp1 and libcurl4 are for the llama.cpp release the daemon installs
# when a model is first started.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libgomp1 libcurl4 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/toskar /usr/local/bin/toskar
# The name from before the rename, for setups that run it by path (#237).
RUN ln -s toskar /usr/local/bin/yggdrasil-daemon
COPY --from=build /out/clustercheck /usr/local/bin/clustercheck
# The image listens beyond loopback, so the daemon refuses to start until
# TOSKAR_API_KEY is set or an API key already exists in the data directory.
# These defaults keep the names from before the rename, so `docker run -e`
# with either YGGDRASIL_* or TOSKAR_* overrides them (TOSKAR_* wins).
ENV YGGDRASIL_API_HOST=0.0.0.0 \
    YGGDRASIL_INTERNAL_HOST=0.0.0.0 \
    YGGDRASIL_DISCOVERY_ENABLED=true
EXPOSE 7331 7332
ENTRYPOINT ["/usr/local/bin/toskar"]
CMD ["--data-dir", "/data"]
