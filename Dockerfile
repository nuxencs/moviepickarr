# build web
FROM oven/bun:1.4.3@sha256:ec06c3b6cea04192ae6770c434f668ca41d343ad19fa6472216c7b48be39c598 AS web-builder

WORKDIR /web

COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

COPY web ./
RUN bun run build

# build app
FROM golang:1.27-alpine3.23@sha256:754d5200fc3edae76d3b6598a9a3fac7e74981ee6a5413ab955025f185753ef2 AS app-builder

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
FROM alpine:latest@sha256:71e9b7bae160ba167fa17414401b94cbcdaf3c32feeab9298bd794a2a0614c35

LABEL org.opencontainers.image.source="https://github.com/nuxencs/moviepickarr"

RUN apk --no-cache add ca-certificates curl tzdata jq

WORKDIR /app

COPY --from=app-builder /src/bin/moviepickarr /usr/local/bin/

EXPOSE 3030

ENTRYPOINT ["/usr/local/bin/moviepickarr"]
