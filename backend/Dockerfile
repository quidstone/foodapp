# backend/Dockerfile

FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server .

# --- runtime image ---
FROM alpine:3.20

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/server /app/server

# default listen port
ENV PORT=8080

EXPOSE 8080

CMD ["/app/server"]
