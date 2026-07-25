# build app
FROM --platform=$BUILDPLATFORM golang:1.26-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS app-builder
RUN apk add --no-cache git tzdata

ENV SERVICE=moviepickarr

WORKDIR /src

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# The build below bind-mounts the build context over /src, so a COPY of the
# source here would be shadowed and never read. BuildKit still checksums the
# mounted context into the RUN's cache key, so invalidation works without it.

ARG VERSION=dev
ARG REVISION=dev
ARG BUILDTIME
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

RUN --network=none --mount=target=. \
    export GOOS=$TARGETOS; \
    export GOARCH=$TARGETARCH; \
    [[ "$GOARCH" == "amd64" ]] && export GOAMD64=$TARGETVARIANT; \
    [[ "$GOARCH" == "arm" ]] && [[ "$TARGETVARIANT" == "v6" ]] && export GOARM=6; \
    [[ "$GOARCH" == "arm" ]] && [[ "$TARGETVARIANT" == "v7" ]] && export GOARM=7; \
    echo $GOARCH $GOOS $GOARM$GOAMD64; \
    go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILDTIME}" -o /out/bin/moviepickarr main.go

# build runner
FROM alpine:latest@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS runner

LABEL org.opencontainers.image.source="https://github.com/nuxencs/moviepickarr"
LABEL org.opencontainers.image.licenses="GPL-2.0-or-later"
LABEL org.opencontainers.image.base.name="alpine:latest"

RUN apk --no-cache add ca-certificates curl tzdata jq

WORKDIR /app
EXPOSE 3030

COPY --link --from=app-builder /out/bin/moviepickarr /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/moviepickarr"]