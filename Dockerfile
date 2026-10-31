FROM golang:1.21-alpine
LABEL org.opencontainers.image.licenses="AGPL-3.0-or-later"
WORKDIR /app
COPY . .
RUN go build -o /usr/local/bin/syncd ./cmd/syncd
CMD ["/usr/local/bin/syncd"]
