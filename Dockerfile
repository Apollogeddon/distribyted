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

RUN apk add --no-cache gcc libc-dev fuse-dev

COPY --from=builder /bin/distribyted /bin/distribyted
RUN chmod -v +x /bin/distribyted

RUN mkdir -v /distribyted-data

RUN echo "user_allow_other" | tee /etc/fuse.conf
ENV DISTRIBYTED_FUSE_ALLOW_OTHER=true

ENTRYPOINT [ "/bin/distribyted" ]
