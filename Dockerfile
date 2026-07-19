# build
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=1.0.0
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags="-s -w -X github.com/aleksey-mu/travel-planner-server/internal/version.Version=${VERSION}" \
  -o /out/travelplan-server ./cmd/server

# run — alpine for HEALTHCHECK + create-user exec
FROM alpine:3.22
RUN apk add --no-cache ca-certificates su-exec \
  && adduser -D -u 65532 -H appuser
COPY --from=build /out/travelplan-server /travelplan-server
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/travelplan-server", "healthcheck"]
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["serve"]
