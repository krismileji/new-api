# 下游功能文档

本目录只记录当前下游分支新增或改变的功能。每个一级目录对应一个大功能，目录内每个 Markdown 文件对应一个具体功能。功能行为以当前代码为准。

## 功能目录

- [渠道监控](channel-monitor/README.md)：渠道运行管理及其子功能。
- [逻辑归组](channel-logical-group/README.md)：多物理渠道共享调度、探测和模型检测身份。
- [使用日志](usage-logs/README.md)：用户侧日志范围、权限和脱敏。
- [中继可靠性](relay-reliability/README.md)：中继失败切换和错误可见性。
- [管理员分组访问](admin-group-access/README.md)：管理员分组访问范围。
- [图像生成定价保护](image-pricing-guard/README.md)：未配置图像倍率时的请求保护。

上游功能不在此重复说明；只有下游改变了入口、权限、数据或行为时才记录。
