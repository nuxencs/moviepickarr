# build web
FROM oven/bun:1.4.5@sha256:3aa040e5445efa09d75bb428a64ba50db13e2b3ac9b6b0c7f145615f62b2ee18 AS web-builder

WORKDIR /web

COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

COPY web ./
RUN bun run build

# build app
FROM golang:1.27-alpine3.23@sha256:7a93747afee80359e81dbe39c8a65037b147cada0262bf86f5b96dd06842b368 AS app-builder

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
