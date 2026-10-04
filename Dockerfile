# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/toskar ./cmd/daemon \
 && CGO_ENABLED=0 go build -o /out/clustercheck ./cmd/clustercheck

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
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
