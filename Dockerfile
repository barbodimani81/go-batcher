FROM golang:1.24.2-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go build -o app ./cmd/demo/main.go

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /app/app .

ENTRYPOINT ["./app"]
CMD ["-count=1000000", "-batch-size=10000", "-timeout=5s", "-workers=6"]
