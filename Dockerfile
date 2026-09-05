# ---- 构建阶段 ----
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /offerpilot ./src/

# ---- 运行阶段 ----
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai
ENV PORT=3001

WORKDIR /app
COPY --from=builder /offerpilot .

# 知识库需要预构建，构建时挂载到 /app/data/knowledge.db
RUN mkdir -p /app/data

EXPOSE 3001

HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O- http://localhost:${PORT}/health || exit 1

CMD ["./offerpilot"]