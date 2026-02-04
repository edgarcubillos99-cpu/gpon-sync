# --- Stage 1: Builder ---
    FROM golang:1.23-alpine AS builder

    # Instalar git y certificados SSL (necesarios para llamadas HTTPS a Notion/Zabbix)
    RUN apk add --no-cache git ca-certificates
    
    WORKDIR /app
    
    # Copiamos primero los go.mod y go.sum para aprovechar la caché de capas de Docker
    COPY go.mod go.sum ./
    RUN go mod download
    
    # Copiamos el código fuente
    COPY . .
    
    # Compilamos el binario. 
    # CGO_ENABLED=0 asegura que sea un binario estático sin dependencias de librerías de C del sistema.
    RUN CGO_ENABLED=0 GOOS=linux go build -o gpon-sync ./cmd/worker/main.go
    
    # --- Stage 2: Runner ---
    FROM alpine:latest
    
    # Instalar certificados CA actualizados y zona horaria (importante para logs)
    RUN apk --no-cache add ca-certificates tzdata
    
    WORKDIR /root/
    
    # Copiamos solo el binario desde la etapa anterior
    COPY --from=builder /app/gpon-sync .
    
    # Comando de ejecución
    CMD ["./gpon-sync"]