FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates tzdata && \
    addgroup -g 1000 app && adduser -u 1000 -G app -D app
WORKDIR /app
COPY --from=build /out/server /app/server
RUN mkdir -p /var/videos /var/uploads && chown -R app:app /var/videos /var/uploads /app
USER app
EXPOSE 8080
CMD ["/app/server"]
