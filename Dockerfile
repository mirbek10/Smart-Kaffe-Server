FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && CGO_ENABLED=0 go build -trimpath -o /out/seed ./cmd/seed && CGO_ENABLED=0 go build -trimpath -o /out/admin ./cmd/admin

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app -u 10001 app
WORKDIR /app
COPY --from=build /out/server /out/seed /out/admin /app/
USER app
EXPOSE 8080
ENV APP_ENV=production
ENTRYPOINT ["/app/server"]
