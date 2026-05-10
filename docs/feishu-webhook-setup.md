# 飞书 Webhook 配置指南

## 前置条件

- ToTra admin 服务已运行（默认 `:8081`）
- 已登录管理员账号
- 飞书应用已有 **机器人能力** 并获得 `task:read`、`docs:read` 权限

---

## 1. 在 ToTra 中创建飞书集成

调用管理员 API 注册集成配置（`webhook_secret` 将用于签名验证）：

```bash
curl -X POST http://localhost:8081/api/integrations \
  -H "Authorization: Bearer <ADMIN_JWT>" \
  -H "Content-Type: application/json" \
  -d '{
    "platform": "feishu",
    "webhook_secret": "your-32-char-secret-here",
    "event_weights": {
      "feishu.task_completed": 2,
      "feishu.doc_created": 2
    }
  }'
```

记录返回的 `id`；配置成功后集成级别自动升到 Level 2（再加 GitHub/Jira 则为 Level 3）。

---

## 2. 获取 Tenant ID

从 JWT 解析或调用任意需要 `tenant_id` 的接口均可得到。
开发环境固定为 `00000000-0000-0000-0000-000000000001`。

---

## 3. 在飞书开放平台配置回调 URL

进入 **飞书开放平台 → 你的应用 → 事件订阅**，填写：

```
请求地址 (Request URL):
  https://your-totra-host/webhooks/feishu?tenant_id=<TENANT_ID>
```

> **本地开发** 可用 [ngrok](https://ngrok.com) 或 [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/) 暴露本地端口：
> ```bash
> ngrok http 8081
> # 得到 https://xxxx.ngrok.io，填入飞书：
> # https://xxxx.ngrok.io/webhooks/feishu?tenant_id=00000000-0000-0000-0000-000000000001
> ```

在 **事件加密** 一栏填入与上面 `webhook_secret` 相同的值（飞书称为「验证令牌」）。

---

## 4. 订阅事件

在 **事件订阅 → 添加事件** 中勾选：

| 事件名称 | 事件 Key | 对应 ToTra 事件类型 |
|---------|---------|-------------------|
| 任务完成 | `task.completed` | `task_completed`（权重 2） |
| 云文档创建 | `docs.created` | `doc_created`（权重 2） |

---

## 5. 员工绑定飞书账号

员工在 ToTra Dashboard 中点击 **Link Account → Platform: feishu**，输入自己的飞书 `open_id`：

- 飞书 `open_id` 格式如 `ou_xxxxxxxxxxxxxxxx`，可在飞书开放平台 **用户信息接口** 或应用 **成员管理** 中获取
- 绑定后飞书事件中的 `assignee.open_id` 会自动匹配到对应用户，计入 OSS 评分

---

## 6. 签名算法（供开发者参考）

ToTra 验证方式：

```
signature = HMAC-SHA256(key=webhook_secret, data=timestamp+body)
// body 为原始 JSON 字节，timestamp 为 X-Lark-Request-Timestamp 头的值（Unix 秒）
```

对应飞书请求头：
- `X-Lark-Request-Timestamp`: Unix timestamp（秒）
- `X-Lark-Signature`: 上述 HMAC 的十六进制字符串

---

## 7. 验收测试

使用项目提供的冒烟测试脚本快速验证端到端：

```bash
# 设置环境变量
export TOTRA_WEBHOOK_SECRET="your-32-char-secret-here"
export TOTRA_TENANT_ID="00000000-0000-0000-0000-000000000001"
export TOTRA_BASE_URL="http://localhost:8081"

bash scripts/smoke-test-feishu.sh
```

脚本会依次发送 `task.completed` 和 `docs.created` 事件，并验证返回 `{"status":"ok"}`。

---

## 常见问题

| 现象 | 原因 | 解决 |
|-----|------|------|
| `401 invalid signature` | Secret 不一致或 timestamp 超时 | 确认两侧 `webhook_secret` 相同；脚本使用当前 Unix 时间 |
| `404 feishu integration not configured` | 未调用 `POST /api/integrations` | 先执行步骤 1 |
| `200 skipped` + `unsupported feishu event` | 飞书事件 Key 拼写错误 | 确认事件 Key 为 `task.completed` / `docs.created` |
| 员工无 OSS 加分 | `open_id` 未绑定 | 员工执行步骤 5 |
