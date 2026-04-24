FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o leash ./cmd/leash

FROM alpine:3.21
# bubblewrap est disponible pour le backend sandbox bwrap.
# Pour l'utiliser dans un conteneur, activer les user namespaces sur l'hôte :
#   docker run --security-opt seccomp=unconfined ...
# ou utiliser le backend none (par défaut) si les namespaces ne sont pas disponibles.
RUN apk add --no-cache bubblewrap
RUN adduser -D -u 1000 leash
WORKDIR /app
COPY --from=builder /build/leash /app/leash
COPY --from=builder /build/policies/ /app/policies/
RUN mkdir -p /tmp/leash && chown leash:leash /tmp/leash
USER leash
ENTRYPOINT ["/app/leash"]
CMD ["--mode", "mcp-stdio"]
