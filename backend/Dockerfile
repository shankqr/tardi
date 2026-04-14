FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /tardi-dashboard-shim ./cmd/dashboard-shim
RUN CGO_ENABLED=0 go build -o /server ./cmd/server
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /synthetic-monitor ./cmd/synthetic-monitor

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /server /server
COPY --from=builder /tardi-dashboard-shim /app/bin/tardi-dashboard-shim
COPY --from=builder /synthetic-monitor /synthetic-monitor
ENV DASHBOARD_SHIM_PATH=/app/bin/tardi-dashboard-shim
EXPOSE 8080
CMD ["/server"]
