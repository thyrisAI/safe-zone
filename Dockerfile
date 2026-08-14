FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go.mod, go.sum and the local module first
COPY go.mod go.sum ./
COPY pkg/tszclient-go ./pkg/tszclient-go

RUN go mod download

COPY . .

RUN mkdir -p /out \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/api main.go \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/tsz-ext-proc ./cmd/tsz-ext-proc \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/tsz-controller ./cmd/tsz-controller \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/tsz-policy ./cmd/tsz-policy \
    && CGO_ENABLED=0 GOOS=linux go build -o /out/byg-mock-openai ./cmd/byg-mock-openai

FROM alpine:latest

WORKDIR /app

COPY --from=builder /out/api ./api
COPY --from=builder /out/tsz-ext-proc ./tsz-ext-proc
COPY --from=builder /out/tsz-controller ./tsz-controller
COPY --from=builder /out/tsz-policy ./tsz-policy
COPY --from=builder /out/byg-mock-openai ./byg-mock-openai

EXPOSE 8080
EXPOSE 9002

CMD ["./api"]
