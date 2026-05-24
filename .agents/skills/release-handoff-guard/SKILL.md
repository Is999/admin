---
name: release-handoff-guard
description: "准备最终交付和交接。用于完成前检查验证、git status、staged 文件，以及迁移、回填、缓存失效、重启、回滚、补偿说明。"
---

# 交付收口护栏

## 工作流程

1. 回放最新用户需求，确认最终 diff 与需求一致。
2. 每个触达 Git 仓库都检查 `git status --short`，区分 staged、unstaged、untracked 和已有无关改动。
3. 仓库规则要求时，只暂存本轮交付文件；不要暂存无关用户改动、本地数据目录、构建产物或历史未跟踪文件。
4. 运行 `git diff --check`；存在 staged 文件时，再运行 `git diff --cached --check`。
5. 确认必需验证命令已运行；跳过时要说明准确原因。
6. 识别运维后续：data migration、backfill、cache invalidation、config reload、process restart、feature flag、rollback、compensation 或人工发布步骤。
7. 最终回复保持诚实，已完成、已验证、已跳过、被阻塞和需要后续处理的事项要分开说明。

## 交付证据

交付时说明改动范围、验证命令、跳过检查、仓库状态、已暂存文件、保留未动的无关改动，以及是否需要 migration、backfill、cache 或 compensation。
