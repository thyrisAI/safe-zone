FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go.mod, go.sum and the local module first
COPY go.mod go.sum ./
COPY pkg/tszclient-go ./pkg/tszclient-go

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o api main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/api .

EXPOSE 8080

CMD ["./api"]
