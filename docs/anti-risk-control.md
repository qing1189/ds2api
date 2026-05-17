# 防风控策略说明

> 适用对象：DeepSeek 上游接口的请求形态。本文记录当前 ds2api 层面已经做的反指纹、反频控措施，以及对应的开关。

## 设计目标

让 ds2api 发出的请求在 DeepSeek 服务端看起来更像「一群普通的 Android 客户端」，而不是「一台机器在并发刷接口」。

具体到三件事：

1. **TLS 指纹**：避免暴露 Go 默认 ClientHello。
2. **HTTP 指纹**：每个账号有稳定的设备身份，每次请求带合理的 trace id。
3. **行为指纹**：请求间隔有自然抖动，不是均匀机器人。

## 当前已落地的能力

### 1. uTLS 指纹（已存在）

`internal/deepseek/transport/transport.go` 强制使用 `utls.HelloSafari_Auto` 握手并把 ALPN 锁到 `http/1.1`。Go 默认 `crypto/tls` 的 ClientHello 是相对独特的，主流 CDN（含 Cloudflare、阿里云）一旦把它当作签名的一部分就很容易过滤；切到 Safari Hello 后基本融入移动端流量。

### 2. 账号粘性指纹（新增）

`internal/deepseek/protocol/fingerprint.go` 为每个账号派生一份固定的设备身份：

| 字段 | 来源 | 行为 |
| --- | --- | --- |
| `User-Agent` | UA 池随机选取（按账号哈希） | **同账号永远一致** |
| `x-client-platform` / `x-client-version` / `x-client-locale` | 与 UA 同源 | **同账号永远一致** |
| `Accept-Language` | 跟随 locale，权重 zh_CN=70 / en_US=15 / 其他=15 | **同账号永远一致** |
| `x-app-build` | UA profile 内置的真实 build 号 | **同账号永远一致** |
| `x-os-version` | Android API level（34/35/36） | **同账号永远一致** |
| `x-device-id` | sha256(salt + accountID) 取前 12 字节 hex | **同账号永远一致** |
| `x-trace-id` | UUIDv4-like，每请求重新生成 | **每请求换** |

之前的 `RandomBaseHeaders()` 是**每次调用都换 UA**，相当于「同一个用户每发一条消息都换一台手机」，这本身就是异常信号。改成账号粘性后：

- 同一账号 Login → CreateSession → GetPow → Completion 四步都用同一个 UA / device id；
- 不同账号是不同的「设备」；
- 操作员可以通过 `POST /admin/queue/rotate-fingerprints` 一键让所有账号「换一台手机」（适合刚被风控完想全员重生的场景）。

派生的盐默认每次进程启动都随机生成。如果你希望重启后所有账号继续保持同一个身份（避免每次重启都"全员换手机"），设置 `DS2API_FINGERPRINT_SALT=任意字符串`。

### 3. 移除可疑请求头（新增）

之前的 baseline 里有 `accept-charset: UTF-8`。现代 Android OkHttp **基本不发这个头**，反倒是 Python `requests` / curl 默认会带。已删除，并用 `Accept-Encoding: gzip, deflate, br` 替代。

### 4. log-normal 抖动（新增）

`internal/deepseek/client/client_core.go` 的 `Jitter()` 之前是均匀分布 200–800ms，且四次串行调用会累积到 0.8–3.2s。两个问题：

- 均匀分布的请求间隔本身就是机器特征；真实人类操作呈右偏分布（大量短间隔 + 少量长间隔）。
- 累积太大伤交互体验。

新版本：
- 默认范围下调到 80–350ms。
- 采样改成对数均匀分布（log-uniform）：~50% 落在 80–180ms、~30% 落在 180–250ms、~20% 落在 250–350ms。
- 仍然支持 `DS2API_REQUEST_JITTER_MIN_MS` / `DS2API_REQUEST_JITTER_MAX_MS` 环境变量覆盖；设为 0 即关闭。

如果你的运行环境本来就慢、不想再多加 jitter，可以直接 `DS2API_REQUEST_JITTER_MIN_MS=0 DS2API_REQUEST_JITTER_MAX_MS=0`。

### 5. 智能权重轮询（已存在）

`internal/account/pool_weight.go` 的逻辑：

- 每个账号初始权重 100；
- 每次失败 -20，连续 5 次成功满血回 100；
- 30 分钟无失败自动恢复；
- 权重为 0 时自动禁用。

这套机制让"被打到 429 / 风控的账号自动冷下来"，避免你不停往同一个倒霉账号上灌请求。

### 6. 多渠道出口（已存在）

`internal/deepseek/client/proxy.go` 支持每账号绑定不同 SOCKS5 出口。配合账号粘性指纹，等价于「每个账号一台手机一个 IP」。**强烈建议为不同账号配不同代理**，否则同 IP 下数十个不同 device id 是另一种异常信号。

### 7. 会话复用（已存在）

`DS2API_SESSION_CACHE_TTL_MINUTES`（默认 10）：同一账号 10 分钟内复用同一个 chat_session_id，不每条消息都重开会话。重开会话本身是低代价行为，但频繁开关也会被风控关注。

## 操作建议

| 场景 | 建议 |
| --- | --- |
| 突然出现大量风控 | 先 `POST /admin/queue/rotate-fingerprints`，再观察一波 |
| 想要重启后保持身份稳定 | 设置 `DS2API_FINGERPRINT_SALT` 为随便一串文本 |
| 部署在中国大陆代理后 | 把 zh_CN 比例继续提高（参见 `localeMix`） |
| 部署在海外服务器 + 中国账号 | 必须给账号配代理，否则地理与语言锁不上 |
| 被怀疑高频 | 把 jitter max 调到 600–1200ms，配合更慢的轮询 |
| 多账号并发想再低调 | 把 `DS2API_REQUEST_JITTER_MIN_MS` 至 200，让首包 RTT 自然分散 |

## 待做（不在本次范围）

下面这些是更激进的方向，目前没做，因为收益不一定值得复杂度：

- **HTTP/2 帧序指纹**：DeepSeek Android 客户端走 HTTP/1.1，所以我们也强制 `http/1.1`，避免 Go HTTP/2 PRIORITY 帧序列暴露身份。这块已经做了，列在这里只是说明。
- **TCP/IP 层指纹**（pof v3、tcp window size 等）：需要 root + raw socket，部署成本太高。
- **请求体顺序随机化**：JSON map 在 Go 里序列化是随机 key 顺序，但 DeepSeek 的 payload 是固定 schema，没什么可乱的。
- **Pow 计算时间随机化**：Pow 太慢会让人觉得 Pow 难度变高，反而引起注意。当前实现是"算多快算多快"。

如果以上有需要，可以再讨论。
