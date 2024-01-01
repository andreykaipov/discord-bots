FROM golang:alpine AS builder
ARG module
ARG dir
RUN test -n "$module"
RUN test -n "$dir"
RUN apk add --no-cache git
WORKDIR /work
COPY "$dir" .
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/app "$module"

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /work/bin/app /app
ENTRYPOINT ["/app"]
