# build
FROM golang:1.26-alpine AS build
# Coolify injects runtime env as build ARGs — declare in build stage too (unused at compile time).
ARG COOLIFY_FQDN
ARG JWT_SECRET
ARG CORS_ORIGINS
ARG ALLOW_REGISTER
ARG JWT_TTL_SECONDS
ARG SERVER_NAME
ARG PUBLIC_BASE_URL
ARG MAX_USERS
ARG INVITE_TTL_DAYS
ARG PASSWORD_RESET_TTL_SECONDS
ARG AUTH_RATE_LIMIT_MAX
ARG AUTH_RATE_LIMIT_WINDOW_SECONDS
ARG TRUST_PROXY
ARG VAULT_QUOTA_MB
ARG MAX_UPLOAD_MB
ARG LOG_FORMAT
ARG LOG_LEVEL
ARG SEED_ADMIN_EMAIL
ARG SEED_ADMIN_PASSWORD
ARG SEED_ADMIN_DISPLAY_NAME
ARG HTTP_PUBLISH_PORT
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
# Coolify injects runtime env as build ARGs — declare so injection does not break the file.
ARG COOLIFY_FQDN
ARG JWT_SECRET
ARG CORS_ORIGINS
ARG ALLOW_REGISTER
ARG JWT_TTL_SECONDS
ARG SERVER_NAME
ARG PUBLIC_BASE_URL
ARG MAX_USERS
ARG INVITE_TTL_DAYS
ARG PASSWORD_RESET_TTL_SECONDS
ARG AUTH_RATE_LIMIT_MAX
ARG AUTH_RATE_LIMIT_WINDOW_SECONDS
ARG TRUST_PROXY
ARG VAULT_QUOTA_MB
ARG MAX_UPLOAD_MB
ARG LOG_FORMAT
ARG LOG_LEVEL
ARG SEED_ADMIN_EMAIL
ARG SEED_ADMIN_PASSWORD
ARG SEED_ADMIN_DISPLAY_NAME
ARG HTTP_PUBLISH_PORT
RUN apk add --no-cache ca-certificates shadow \
  && adduser -D -u 65532 -H appuser
COPY --from=build /out/travelplan-server /travelplan-server
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/travelplan-server", "healthcheck"]
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["serve"]
