FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/parliamentsim .

FROM alpine:3.21
RUN adduser -D -u 10001 appuser
WORKDIR /app

COPY --from=builder /out/parliamentsim /app/parliamentsim
COPY scenarios/ /app/scenarios/

USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/parliamentsim"]
CMD ["-addr", ":8080", "-trust-proxy"]
