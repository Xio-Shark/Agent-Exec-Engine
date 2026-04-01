FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /agent-exec-engine ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates docker-cli
COPY --from=builder /agent-exec-engine /agent-exec-engine

EXPOSE 8080
ENTRYPOINT ["/agent-exec-engine"]
