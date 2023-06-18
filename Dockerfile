# syntax=docker/dockerfile:1
FROM golang:1.18-alpine
ARG GIT_COMMIT=""
ARG BUILD_TIME=""

MAINTAINER maintainers@nprobe.net

# create a working directory inside the image
WORKDIR /app

# copy Go modules and dependencies to image
COPY go.mod go.sum ./

# download Go modules and dependencies
RUN go mod download

# copy directory files i.e all files ending with .go
COPY *.go ./

# compile application
RUN go build -ldflags "-X main.commitS=${GIT_COMMIT} -X main.buildtime=${BUILD_TIME}" -o /nprobe

# second stage for running
FROM prom/busybox:glibc
COPY --from=0 /nprobe /usr/local/bin/nprobe
EXPOSE 8000
ENTRYPOINT [ "/usr/local/bin/nprobe" ]
