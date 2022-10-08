# syntax=docker/dockerfile:1
FROM golang:1.18-alpine

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
RUN go build -o /nprobe
 
# tells Docker that the container listens on specified network ports at runtime
EXPOSE 8000

# command to be used to execute when the image is used to start a container
ENTRYPOINT [ "/nprobe" ]
