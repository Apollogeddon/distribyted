#===============
# Stage 1: Build
#===============

# the same Alpine release as the image below, whose libraries the binary links against
FROM golang:1.27-alpine3.24 AS builder

RUN apk add --no-cache fuse-dev gcc libc-dev g++ git

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build arguments for versioning
ARG VERSION=unknown
ARG BUILD=unknown

# Build with verbosity
RUN go tool -modfile=.forgego/task/go.mod task build BIN_OUTPUT=/bin/distribyted VERSION="${VERSION}" BUILD="${BUILD}"

#===============
# Stage 2: Run
#===============

# pinned to a release and its digest, so a rebuild can't change the base unseen; Dependabot proposes updates
FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

# Install only runtime dependencies
RUN apk add --no-cache fuse libstdc++ libgcc

# Create a non-root user
RUN addgroup -S distribyted && adduser -S distribyted -G distribyted

COPY --from=builder /bin/distribyted /bin/distribyted
# GPL-3.0 requires the license to accompany the program
COPY --from=builder /app/LICENSE /usr/share/licenses/distribyted/LICENSE
RUN chmod +x /bin/distribyted

# FUSE configuration
RUN echo "user_allow_other" >> /etc/fuse.conf && \
    chmod 644 /etc/fuse.conf

# Set up data directory with correct permissions
RUN mkdir -p /distribyted-data && chown distribyted:distribyted /distribyted-data

USER distribyted
ENV DISTRIBYTED_FUSE_ALLOW_OTHER=true
# a container is reached through its published ports, so it listens on every address
ENV DISTRIBYTED_HTTP_IP=0.0.0.0 DISTRIBYTED_WEBDAV_IP=0.0.0.0

# the binary checks its own HTTP server, so the image needs no curl; loading many torrents can
# take minutes before the server starts
HEALTHCHECK --interval=30s --timeout=10s --start-period=5m --retries=3 \
    CMD [ "/bin/distribyted", "healthcheck" ]

ENTRYPOINT [ "/bin/distribyted" ]
