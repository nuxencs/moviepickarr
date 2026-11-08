# build web
FROM oven/bun:1.4.4@sha256:1d22548f177f4ff38e0c15baa3c2c46c6c2060ba2d1bc573bb4720b407548f1d AS web-builder

WORKDIR /web

COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

COPY web ./
RUN bun run build

# build app
FROM golang:1.27-alpine3.23@sha256:5d5113301ffcac84e2bfb20c42648803fecaea6789c89bb9b66884e18cbb30a7 AS app-builder

ARG VERSION=dev
ARG REVISION=dev
ARG BUILDTIME

RUN apk add --no-cache git build-base tzdata

ENV SERVICE=moviepickarr

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
COPY --from=web-builder /web/dist ./web/dist

#ENV GOOS=linux
#ENV CGO_ENABLED=0

RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILDTIME}" -o bin/moviepickarr main.go

# build runner
FROM alpine:latest@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

LABEL org.opencontainers.image.source="https://github.com/nuxencs/moviepickarr"

RUN apk --no-cache add ca-certificates curl tzdata jq

WORKDIR /app

COPY --from=app-builder /src/bin/moviepickarr /usr/local/bin/

EXPOSE 3030

ENTRYPOINT ["/usr/local/bin/moviepickarr"]
