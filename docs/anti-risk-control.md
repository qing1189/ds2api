# 防风控策略说明

> 适用对象：DeepSeek 上游接口的请求形态。本文记录当前 ds2api 层面已经做的反指纹、反频控措施，以及对应的开关。

## 设计目标

让 ds2api 发出的请求在 DeepSeek 服务端看起来更像「一群正常的 Chrome 浏览器网页用户」，而不是「一台机器在并发刷接口」。

> **重要变更（对话方案对齐）**：早期版本把请求伪装成 **Android 客户端**（`DeepSeek/x.y.z Android/35`、`x-client-platform: android`）。实测该形态会触发 DeepSeek 风控并导致账号禁言。现已对齐到经过验证、不触发风控的 **Web 客户端方案**（Chrome 浏览器 + 每账号 Cookie 罐 + HIF 反爬令牌），与参考实现保持一致。

具体到几件事：

1. **TLS 指纹**：避免暴露 Go 默认 ClientHello。
2. **HTTP 身份指纹**：每个账号有稳定的浏览器身份（UA / sec-ch-ua / locale / Cookie 罐）。
3. **HIF 反爬令牌**：复刻 Web 端 `x-hif-leim` / `x-hif-dliq` 头。
4. **行为指纹**：请求间隔有自然抖动，不是均匀机器人。

## 当前已落地的能力

### 1. uTLS 指纹（已存在）

`internal/deepseek/transport/transport.go` 强制使用 `utls.HelloSafari_Auto` 握手并把 ALPN 锁到 `http/1.1`。Go 默认 `crypto/tls` 的 ClientHello 是相对独特的，主流 CDN（含 Cloudflare、阿里云）一旦把它当作签名的一部分就很容易过滤；切到浏览器 Hello 后基本融入正常网页流量。Web 端走 HTTP/1.1，因此锁定 http/1.1 与真实浏览器一致。

### 2. Web 浏览器身份（对话方案核心，已重写）

`internal/deepseek/protocol/fingerprint.go` 为每个账号派生一份固定的浏览器身份：

| 字段 | 来源 | 行为 |
| --- | --- | --- |
| `User-Agent` | Chrome 桌面 UA 池（按账号哈希选取，119–123） | **同账号永远一致** |
| `x-client-platform` | 固定 `web` | 所有账号一致 |
| `x-client-version` / `x-app-version` | 固定 `2.0.0` | 所有账号一致 |
| `x-client-locale` / `Accept-Language` | locale 池，权重 zh_CN=75 | **同账号永远一致** |
| `sec-ch-ua` / `sec-ch-ua-platform` | 与 UA 主版本、操作系统同源 | **同账号永远一致** |
| `Cookie` | 每账号稳定 Cookie 罐（见下） | **同账号永远一致** |
| 静态浏览器头 | `Origin` / `Referer` / `sec-fetch-*` / `x-client-timezone-offset:28800` | 固定 |

**Cookie 罐**（`buildWebIdentity`）按账号确定性派生，复刻真实 Web 端会话：

```
smidV2=20260520<alnum10><hex24>;
HWWAFSESTIME=<13 位 epoch_ms>;
HWWAFSESID=<alnum4><hex12>;
ds_session_id=<hex32>;
.thumbcache_<hex32>=<url-encoded base64 设备 id>
```

派生的盐默认每次进程启动随机生成。若希望重启后所有账号保持同一身份，设置 `DS2API_FINGERPRINT_SALT=任意字符串`。操作员可通过 `POST /admin/queue/rotate-fingerprints` 让所有账号「换一台设备」（重新生成 UA + Cookie 罐）。

> 之前的 Android 形态每条请求带 `x-app-build` / `x-os-version` / `x-device-id` / `x-trace-id`，这些头真实 Web 端并不发送，已全部移除。

### 3. HIF 反爬令牌（新增）

`internal/deepseek/client/client_hif.go` 复刻 Web 端「隐藏校验」流程：首次请求时向 `hif-leim.deepseek.com/query` 与 `hif-dliq.deepseek.com/query` 拉取令牌，并在后续的 **创建会话 / 取 PoW / completion / continue / 上传** 请求上回放为 `x-hif-leim` / `x-hif-dliq` 头。

- 令牌按 token 缓存，TTL 取响应头 `x-hif-ttl`（默认 600s）。
- **尽力而为**：HIF 端点不可达或返回空时，请求照常进行（不阻塞）。
- 可用 `DS2API_DISABLE_HIF=1` 关闭。

### 4. 登录 / 会话形态对齐 Web（新增）

- 登录：`os=web`，`device_id` 用账号稳定的 base64 设备 id（前缀 `B`），并补齐 `mobile`/`area_code` 空字段。
- 创建会话：请求体改为 `{}`（与 Web 端一致），不再发 `{"agent":"chat"}`。
- completion 请求体补齐 `action: null` 与 `preempt: false`，`parent_message_id` 始终为 `null`（纯对话转发，单条 prompt）。
- completion / continue 请求 `Accept: text/event-stream`。

### 5. log-uniform 抖动（已存在）

`internal/deepseek/client/client_core.go` 的 `Jitter()` 采用对数均匀分布，默认 80–350ms，模拟人类右偏的请求间隔。可用 `DS2API_REQUEST_JITTER_MIN_MS` / `DS2API_REQUEST_JITTER_MAX_MS` 覆盖；设为 0 关闭。

### 6. 智能权重轮询（已存在）

`internal/account/pool_weight.go`：每账号初始权重 100，失败 -20，连续成功满血，30 分钟无失败自动恢复，权重为 0 自动禁用。让被风控的账号自动冷却。

### 7. 多渠道出口（已存在）

`internal/deepseek/client/proxy.go` 支持每账号绑定不同 SOCKS5 出口。配合每账号稳定的浏览器身份，等价于「每个账号一个浏览器一个 IP」。**强烈建议为不同账号配不同代理**，否则同 IP 下数十个不同 Cookie 身份是另一种异常信号。

### 8. 会话复用（已存在）

`DS2API_SESSION_CACHE_TTL_MINUTES`（默认 10）：同一账号短时间内复用同一个 `chat_session_id`，不每条消息都重开会话。

## 操作建议

| 场景 | 建议 |
| --- | --- |
| 突然出现大量风控 | 先 `POST /admin/queue/rotate-fingerprints`，再观察一波 |
| 想要重启后保持身份稳定 | 设置 `DS2API_FINGERPRINT_SALT` 为随便一串文本 |
| HIF 端点异常 / 调试 | 临时 `DS2API_DISABLE_HIF=1` 关闭 HIF |
| 海外服务器 + 中国账号 | 必须给账号配代理，否则地理与语言锁不上 |
| 被怀疑高频 | 把 jitter max 调到 600–1200ms，配合更慢的轮询 |

## 待做（不在本次范围）

- **HTTP 头大小写 / 顺序**：Go `net/http` 会把头名规范化为 `Sec-Ch-Ua` 这种 Title-Case，真实浏览器发的是小写。HTTP/1.1 下头名大小写不敏感，但精细 WAF 可能会比对；如需完全对齐需绕过 Go 的规范化逻辑，成本较高，暂未做。
- **TCP/IP 层指纹**：需要 root + raw socket，部署成本太高。
