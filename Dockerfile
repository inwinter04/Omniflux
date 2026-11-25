# syntax=docker/dockerfile:1

# 阶段 1:构建管理台静态产物
FROM node:22-alpine AS admin-builder
WORKDIR /app/web/admin
RUN corepack enable
COPY web/admin/package.json web/admin/pnpm-lock.yaml web/admin/pnpm-workspace.yaml web/admin/.npmrc ./
RUN pnpm install --frozen-lockfile
COPY web/admin ./
RUN pnpm build

# 阶段 2:构建服务端(前端产物经 go:embed 打进二进制)
FROM golang:1.27-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=admin-builder /app/web/admin/dist ./internal/console/dist
RUN CGO_ENABLED=0 go build -trimpath -o /omniflux ./cmd/omniflux

# 阶段 3:运行镜像
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=go-builder /omniflux /usr/local/bin/omniflux
ENV OMNIFLUX_HTTP_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["omniflux"]
