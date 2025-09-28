########## Build stage ##########
FROM golang:1.25-alpine AS build
WORKDIR /src

# Build metadata (can be overridden at build time)
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE

ENV CGO_ENABLED=0 \
	GO111MODULE=on

COPY go.mod go.sum ./
# Module download with cache
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# If DATE not provided, compute it (UTC RFC3339)
RUN : "${DATE:=$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
	--mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" -o /out/can-server ./cmd/can-server

########## Runtime (distroless) ##########
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /out/can-server /usr/local/bin/can-server
USER nonroot

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
LABEL org.opencontainers.image.title="can-server" \
	  org.opencontainers.image.source="https://github.com/kstaniek/go-ampio-server" \
	  org.opencontainers.image.description="CAN <-> TCP cannelloni gateway" \
	  org.opencontainers.image.version="${VERSION}" \
	  org.opencontainers.image.revision="${COMMIT}" \
	  org.opencontainers.image.created="${DATE}" \
	  org.opencontainers.image.licenses="MIT"

ENTRYPOINT ["/usr/local/bin/can-server"]
# Example (serial): docker run --rm --network host --device /dev/ttyUSB0 can-server -backend serial -serial /dev/ttyUSB0
# Example (socketcan, host net): docker run --rm --network host can-server -backend socketcan -can-if can0