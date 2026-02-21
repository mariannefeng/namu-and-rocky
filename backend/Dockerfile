FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o backend main.go

FROM alpine:3.19

WORKDIR /app

# Needed for outbound HTTPS (e.g. R2 API)
RUN apk --no-cache add ca-certificates

COPY --from=builder /app/backend .

EXPOSE 8080

CMD ["./backend"]
