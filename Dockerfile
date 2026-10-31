# syntax=docker/dockerfile:1

FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/syncd ./cmd/syncd \
 && mkdir -p /out/data

FROM gcr.io/distroless/static:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
LABEL org.opencontainers.image.licenses="AGPL-3.0-or-later"
COPY --from=build /out/syncd /usr/local/bin/syncd
COPY --from=build /src/third_party_licenses /usr/share/licenses/measure/
COPY --from=build --chown=65532:65532 /out/data /data
ENV SYNCD_ADDR=0.0.0.0:8080 \
    SYNCD_DB=/data/syncd.db
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/syncd"]
