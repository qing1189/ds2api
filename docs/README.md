# DS2API 文档导航

为减少重复维护，本仓库文档按“入口文档 + 专题文档”拆分。建议从下列顺序阅读：

1. [项目总览（README）](../README.MD)
2. [架构与目录说明](./ARCHITECTURE.md)
3. [接口文档（API）](../API.md)
4. [部署指南](./DEPLOY.md)
5. [测试指南](./TESTING.md)
6. [开发者速查](./DEVELOPMENT.md)
7. [贡献指南](./CONTRIBUTING.md)

## 专题文档

- [DS2API 项目价值说明](./project-value.md)
- [API -> 网页对话纯文本兼容主链路说明](./prompt-compatibility.md)
- [DeepSeek SSE 行为结构说明（逆向观察）](./DeepSeekSSE行为结构说明-2026-04-05.md)

## 文档维护约定

- 文档更新必须以实际代码实现为依据：总路由装配看 `internal/server/router.go`，协议/resource 路由看 `internal/httpapi/**/handler*.go` 与 `internal/httpapi/admin/handler.go`，配置默认值看 `internal/config/*`，模型/alias 看 `internal/config/models.go`，prompt 兼容链路看 `docs/prompt-compatibility.md` 列出的代码入口。
- `README.MD`：面向首次接触用户，保留“是什么 + 怎么快速跑起来”。
- `docs/ARCHITECTURE.md`：面向开发者，集中维护项目结构、模块职责与调用链。
- `API.md`：面向客户端接入者，聚焦接口行为、鉴权和示例。
- `docs/prompt-compatibility.md`：面向维护者，集中维护“API -> 网页对话纯文本上下文”的统一兼容语义；相关行为修改时必须同步更新。
- 其他 `docs/*.md`：主题化说明，避免在多个文档重复粘贴同一段内容。
