# Multi-stage Dockerfile for Birrcare Go Backend (Zero CGO)
FROM golang:1.27-alpine3.24 AS builder

WORKDIR /app

RUN apk --no-cache add ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build statically linked binary with modernc.org/sqlite (CGO_ENABLED=0)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/birrcare-backend ./main.go

# Minimal runtime stage
FROM alpine:3.24

WORKDIR /app

RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/birrcare-backend /app/birrcare-backend

EXPOSE 8080

VOLUME ["/data"]

ENV PORT=8080
ENV DB_PATH=/data/birrcare.db
ENV APP_ENV=production

# Last, so a new commit only rebuilds this layer (issue #43).
ARG GIT_SHA=dev
ENV GIT_SHA=${GIT_SHA}

ENTRYPOINT ["/app/birrcare-backend"]
