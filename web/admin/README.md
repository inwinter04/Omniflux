# Omniflux Admin

Vue 3 + TypeScript + Naive UI management console for Omniflux.

This app is based on [SoybeanAdmin](https://github.com/soybeanjs/soybean-admin)
2.3.0 and keeps its MIT license in `LICENSE`.

## Commands

```powershell
corepack pnpm install
corepack pnpm dev
corepack pnpm typecheck
corepack pnpm build
```

During frontend development, Vite proxies `/api` to the Go backend at
`http://127.0.0.1:8080`.

For production, `corepack pnpm build` creates `dist/`; the root script
`scripts/sync-admin.ps1` copies it into `internal/console/dist` for embedding.