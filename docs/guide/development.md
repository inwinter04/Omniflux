# 开发

## 前端开发

先启动后端:

```bash
make run
```

再启动 Vite 开发服务器:

```bash
make admin-dev
```

Vite 会将 `/api` 代理到 Go 服务。

## 构建管理台

```bash
corepack pnpm --dir web/admin install
make build-admin
```

## 文档站

文档站基于 VitePress,源文件位于 `docs/`:

```bash
make docs-dev    # 本地开发
make docs-build  # 构建到 docs/.vitepress/dist
```

## 项目结构

```text
cmd/omniflux/           服务入口
internal/               领域模块、迁移、存储与管理接口
web/admin/              Vue 3 + Naive UI 管理台
docs/                   VitePress 文档站
scripts/                构建辅助脚本
docker-compose.yml      本地 PostgreSQL 与 Redis
```

## 开发验证

```bash
go test -race ./...
go vet ./...
corepack pnpm --dir web/admin typecheck
corepack pnpm --dir web/admin build
```

CI 会在推送与 PR 时运行后端测试与管理台构建。
