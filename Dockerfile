FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go.mod, go.sum and the local module first
COPY go.mod go.sum ./
COPY pkg/tszclient-go ./pkg/tszclient-go

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o api main.go

FROM alpine:3.24.1

WORKDIR /app

RUN addgroup -S tsz && adduser -S tsz -G tsz

COPY --from=builder /app/api .

RUN chown tsz:tsz /app/api

USER tsz:tsz

EXPOSE 8080

CMD ["./api"]
