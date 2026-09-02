## 1. 排序先验

- [x] 1.1 扩展分数诊断以保留 raw relevance、source prior 和 final score
- [x] 1.2 将有界来源先验移到 MMR 前并移除固定后置乘分

## 2. 验证

- [x] 2.1 增加低相关 Wiki 不越级、分数边界和插件顺序测试
- [x] 2.2 用离线基线评估相关性、无答案和 source displacement 后确定默认值（见 `establish-rag-evaluation-baseline/evidence`；保留 Wiki 0.05、来源先验上限 0.08）
