#===============
# Stage 1: Build
#===============

FROM golang:1.26-alpine as builder

RUN apk add --no-cache fuse-dev gcc libc-dev g++ make git

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
RUN BIN_OUTPUT=/bin/distribyted make build \
    LDFLAGS="-X=main.Version=${VERSION} -X=main.Build=${BUILD}"

#===============
# Stage 2: Run
#===============

FROM alpine:3

# Install only runtime dependencies
RUN apk add --no-cache fuse libstdc++ libgcc

# Create a non-root user
RUN addgroup -S distribyted && adduser -S distribyted -G distribyted

COPY --from=builder /bin/distribyted /bin/distribyted
RUN chmod +x /bin/distribyted

# FUSE configuration
RUN echo "user_allow_other" >> /etc/fuse.conf && \
    chmod 644 /etc/fuse.conf

# Set up data directory with correct permissions
RUN mkdir -p /distribyted-data && chown distribyted:distribyted /distribyted-data

USER distribyted
ENV DISTRIBYTED_FUSE_ALLOW_OTHER=true

ENTRYPOINT [ "/bin/distribyted" ]
