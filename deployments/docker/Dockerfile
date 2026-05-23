FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/hotel-harmony-api ./cmd/server

FROM alpine:3.20
RUN adduser -D -H appuser
USER appuser
WORKDIR /app
COPY --from=build /out/hotel-harmony-api /app/hotel-harmony-api
EXPOSE 8787
ENTRYPOINT ["/app/hotel-harmony-api"]
