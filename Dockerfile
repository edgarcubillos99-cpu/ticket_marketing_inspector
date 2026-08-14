FROM golang:1.25-alpine AS builder

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/agente_marketing .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
	&& adduser -D -H -u 10001 appuser

WORKDIR /app
COPY --from=builder /out/agente_marketing /app/agente_marketing

ENV PORT=8080
EXPOSE 8080

USER appuser
CMD ["/app/agente_marketing"]
