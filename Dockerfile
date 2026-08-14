FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go.mod, go.sum and the local module first
COPY go.mod go.sum ./
COPY pkg/tszclient-go ./pkg/tszclient-go

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o api main.go \
    && CGO_ENABLED=0 GOOS=linux go build -o tsz-ext-proc ./cmd/tsz-ext-proc \
    && CGO_ENABLED=0 GOOS=linux go build -o tsz-controller ./cmd/tsz-controller \
    && CGO_ENABLED=0 GOOS=linux go build -o tsz-policy ./cmd/tsz-policy \
    && CGO_ENABLED=0 GOOS=linux go build -o byg-mock-openai ./cmd/byg-mock-openai

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/api .
COPY --from=builder /app/tsz-ext-proc .
COPY --from=builder /app/tsz-controller .
COPY --from=builder /app/tsz-policy .
COPY --from=builder /app/byg-mock-openai .

EXPOSE 8080
EXPOSE 9002

CMD ["./api"]
