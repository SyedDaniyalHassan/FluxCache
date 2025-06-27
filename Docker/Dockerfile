# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN ls -al
COPY /mnt/host/c/Users/daniyal.hassan/Documents/personal/FluxCache .
RUN go build -o fluxcache ./cmd/cache-node
RUN ls

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/fluxcache .
EXPOSE 8080 9100
ENTRYPOINT ["./fluxcache"]