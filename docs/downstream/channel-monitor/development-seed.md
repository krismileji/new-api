# 智能调度开发验收数据

`cmd/channel-smart-schedule-seed` 是一次性开发工具，用真实数据库表结构写入可重复查看的智能调度验收数据。工具不会执行调度、不会清空表，也不会修改现有智能调度全局设置。

工具会创建或刷新以下数据：

- 10 个名称以 `[智能调度验收]` 开头、标签为“智能调度验收数据”的渠道。
- “调度验收-低成本”和“调度验收-高可靠”两个隔离分组。
- `smart-seed-chat-fast` 和 `smart-seed-chat-balanced` 两个隔离模型。
- 每个分组模型池 10 条已参与路由，共 40 条 Ability 和路由参与状态。
- 每个 `(渠道, 模型)` 只保存一份共享样本，共 20 份；同一模型关联的两个分组读取同一份样本。
- 成本、首字、TPS、成功、快速失败、慢失败、首字抖动和样本不足等可区分数据。

## 执行保护

执行前应停止开发后端，确认 `SQL_DSN` 或 `SQLITE_PATH` 指向需要验收的开发数据库。命令同时要求开发环境标记和固定确认令牌；缺少任一项都会拒绝写入。

本机开发环境已在仓库根目录 `.env` 中配置开发数据库，因此从项目根目录执行：

```powershell
$env:APP_ENV = 'development'
$env:CHANNEL_SMART_SCHEDULE_SEED_CONFIRM = 'write-channel-smart-schedule-v1'
& 'D:\Go\sdk\go1.26.5\bin\go.exe' run ./cmd/channel-smart-schedule-seed
Remove-Item Env:CHANNEL_SMART_SCHEDULE_SEED_CONFIRM -ErrorAction SilentlyContinue
Remove-Item Env:APP_ENV -ErrorAction SilentlyContinue
```

工具会通过 `godotenv` 读取仓库根目录 `.env`，无需把其中的数据库密码复制到命令行。若在其他机器执行，则必须先显式设置正确的 `SQL_DSN` 或 `SQLITE_PATH`。

使用 SQLite 时还需要显式指定文件，不能依赖默认路径：

```powershell
$env:SQLITE_PATH = 'D:\absolute\path\to\development.db'
```

使用 MySQL 或 PostgreSQL 时改为只设置实际的 `SQL_DSN`。不要同时设置
`SQL_DSN` 和 `SQLITE_PATH`，数据库类型仍以 `SQL_DSN` 为准。

重复执行会按完整渠道名称更新同一批验收渠道及其关联数据，不会重复创建。若数据库中已经存在两个完全同名的验收渠道，工具会回滚整次事务并要求人工确认。工具使用的密钥和上游地址均不可用，验收分组和模型不应承载真实业务请求。

写入完成后，在管理端为两个验收分组配置需要审核的智能调度策略，再手动执行一次智能调度。这样由实际任务生成的评分、优先级、稳定性变化和执行记录可以直接在页面检查；数据工具本身不会伪造调度任务结果。

## 生成真实调度记录

seed 不会修改全局设置。执行调度前，必须先在“渠道监控 -> 智能调度设置”中为“调度验收-低成本”和“调度验收-高可靠”分别保存完整策略，并启用智能调度；没有显式策略的分组会被忽略。

需要只监听 `3001` 时，先构建前端，再由 Go 服务同时托管页面和 API：

```powershell
Push-Location web
bun run build
Pop-Location
$env:PORT = '3001'
& 'D:\Go\sdk\go1.26.5\bin\go.exe' run .
```

保持服务运行，在另一个 PowerShell 窗口登录并触发一次真实调度任务：

```powershell
$baseUrl = 'http://127.0.0.1:3001'
$securePassword = Read-Host '管理员密码' -AsSecureString
$adminPassword = [System.Net.NetworkCredential]::new('', $securePassword).Password
$loginBody = @{ username = 'admin'; password = $adminPassword } | ConvertTo-Json
$login = Invoke-RestMethod -Method Post -Uri "$baseUrl/api/user/login" -ContentType 'application/json' -Body $loginBody
if (-not $login.success -or -not $login.data.access_token) { throw "登录失败：$($login.message)" }
$headers = @{ Authorization = "Bearer $($login.data.access_token)" }
$run = Invoke-RestMethod -Method Post -Uri "$baseUrl/api/channel_monitor/schedule/run" -Headers $headers
$taskId = $run.data.task.task_id
$taskId
```

任务是异步执行的。用返回的 `$taskId` 轮询并输出完整结果：

```powershell
do {
  Start-Sleep -Seconds 1
  $history = Invoke-RestMethod -Method Get -Uri "$baseUrl/api/channel_monitor/tasks?kind=schedule&p=1&page_size=20" -Headers $headers
  $task = $history.data.items | Where-Object { $_.task_id -eq $taskId } | Select-Object -First 1
} while ($task -and $task.status -in @('pending', 'running'))
$task | ConvertTo-Json -Depth 20
$adminPassword = $null
```

也可以直接在管理端点击“立即执行”，然后从独立的智能调度执行记录入口查看同一任务的评分输入、计算结果、调整原因和失败阶段。
