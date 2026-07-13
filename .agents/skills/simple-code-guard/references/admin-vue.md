# admin-vue 项目边界

## 真实结构

- 可见功能沿 `views`、`router`、`api`、`store`、`components` 和 `locales` 闭环。
- 优先复用现有 Vben 组件、schema、hooks 和请求客户端，不另建平行 UI 或状态框架。
- 前端只承载交互状态和展示规则；后端权限、安全和数据规则不复制成第二套业务实现。
- 新增或重构的变量、常量、函数、类型、接口、字段、路由、状态结构和关键交互必须按项目规范补简洁中文注释。

## 简化重点

- 单页面使用的状态优先留在页面；没有稳定复用时不提取 composable、store 或通用组件。
- API wrapper 直接映射后端契约，不叠加只转发参数或改名的多层 wrapper。
- 表格、表单、弹窗和 action 优先使用现有 schema 与组件能力；不要为少量字段制造配置生成器。
- 避免无边界的 deep watch、重复请求、全量前端过滤和大数组复制；分页、取消和 loading 状态直接可见。
- 删除无路由、无菜单、无调用方的旧页面、类型、i18n key 和兼容字段。
- 性能优化优先减少网络请求、重复渲染和大对象响应式开销，不用难读的缓存技巧优化普通页面。

## 验证

按改动风险选择并在最终候选版本运行：

- `pnpm -F @vben/web-antd run typecheck`
- 提交就绪时运行 `pnpm exec lefthook run pre-commit`
- 必要时补受影响 package 的 lint 或 build
- `git diff --check`、`git diff --cached --check`、`git status --short`
