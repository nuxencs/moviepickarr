# build web
FROM oven/bun:latest AS web-builder

WORKDIR /web

COPY web/package.json web/bun.lockb ./
RUN bun install --frozen-lockfile

COPY web ./
RUN bun run build

# build app
FROM golang:1.26-alpine3.23 AS app-builder

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
FROM alpine:latest

LABEL org.opencontainers.image.source="https://github.com/nuxencs/moviepickarr"

RUN apk --no-cache add ca-certificates curl tzdata jq

WORKDIR /app

COPY --from=app-builder /src/bin/moviepickarr /usr/local/bin/

EXPOSE 3030

ENTRYPOINT ["/usr/local/bin/moviepickarr"]