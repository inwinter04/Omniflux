# 源码构建与本地运行

对于需要针对企业自身业务场景进行二次开发、定制专属路由插件或在本地源码调试的开发者，本篇提供完整的从源码构建指引。

## 本地前置编译工具

- **Go 1.27+**
- **Node.js 20+**
- **pnpm**（可通过 `corepack enable` 开启）
- **Make**（可选，用于快捷执行命令）

## 构建与运行全流程

### 步骤 1：拉取仓库并启动依赖
```bash
git clone https://github.com/inwinter04/Omniflux.git
cd Omniflux

# 启动本地基础依赖服务 (Postgres & Redis)
make infra
```

### 步骤 2：构建管理台前端并同步静态资源
Omniflux 将管理台编译后的静态网页自动嵌入到 Go 的 `internal/console/dist` 目录中：

```bash
# 这一步将执行 pnpm install、打包构建并将产物同步至后端嵌入目录
make build-admin
```

### 步骤 3：直接启动后端服务
导出必须的环境变量后，直接启动 Go 程序：

```bash
export OMNIFLUX_DATABASE_URL="postgres://omniflux:omniflux@localhost:5432/omniflux?sslmode=disable"
export OMNIFLUX_REDIS_ADDR="localhost:6379"
export OMNIFLUX_MASTER_KEY="MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
export OMNIFLUX_API_KEY_PEPPER="local-dev-pepper-must-be-at-least-32-chars-long"
export OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com"
export OMNIFLUX_BOOTSTRAP_PASSWORD="admin-password-16chars"

go run ./cmd/omniflux
```

系统在启动时会自动利用嵌入的 `goose` 迁移工具检测并执行数据库 Schema 迁移，无需手动导入 SQL 脚本。

### 步骤 4：编译独立二进制执行文件
若需要脱离开发环境打包单一可执行文件：

```bash
go build -ldflags="-s -w" -o bin/omniflux ./cmd/omniflux
```
编译产出的 `bin/omniflux` 是一个零外部动态依赖、自带管理台前端的单文件可执行程序！
