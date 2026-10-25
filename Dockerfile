FROM golang:1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/travelplan-server ./cmd/server

FROM alpine:3
RUN apk add --no-cache ca-certificates su-exec \
  && adduser -D -u 65532 -H appuser
COPY --from=build /out/travelplan-server /travelplan-server
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh
ENV DATA_DIR=/data
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/travelplan-server", "healthcheck"]
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["serve"]
