---
name: go-test-failure-debugger
description: "复现并修复 Go 验证失败。用于 go test、go test ./...、go vet、go test -race、编译、CI、flaky、mock 或环境依赖测试问题。"
---

# Go 测试失败排查

## 工作流程

1. 复现准确命令，抓第一处真实失败，不只看最终 summary。
2. 判断失败类型：编译、vet、单测逻辑、集成依赖、race、timeout、flaky 顺序、缓存或环境/配置。
3. 区分本轮改动引入的失败和仓库已有健康问题；不要用无关噪声掩盖新失败。
4. 用 package path、`-run`、`-count=1`、compile-only `-run '^$'` 和定向重跑缩小范围，再逐步扩大。
5. 从源码、测试、fixture、生成数据或配置修根因；除非用户明确要求且理由写清，不跳过、不削弱、不删除测试。
6. 测试改动要匹配生产行为和仓库风格；避免只断言实现细节的脆弱 mock。
7. 先重跑最小失败命令，再在可行时跑必要的更大范围命令。

## 常用检查

- `go test ./path/to/pkg -run TestName -count=1`
- `go test ./path/to/pkg/...`
- `go test ./...`
- `go test -race ./path/to/pkg/...`
- `go vet ./path/to/pkg/...`
- `git diff --check`

## 交付证据

说明失败命令、根因、修改文件、重跑命令、仍存在的无关失败，以及为什么跳过全量测试。
