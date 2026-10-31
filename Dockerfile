FROM golang:1.21-alpine
LABEL org.opencontainers.image.licenses="AGPL-3.0-or-later"
WORKDIR /app
COPY . .
# Include third-party license texts required for attribution in binaries
COPY third_party_licenses /usr/share/licenses/measure/
RUN go build -o /usr/local/bin/syncd ./cmd/syncd
CMD ["/usr/local/bin/syncd"]
